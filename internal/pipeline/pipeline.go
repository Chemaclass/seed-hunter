package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/bip39"
	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

const (
	wordlistSize     = 2048
	defaultBatchSize = 50
)

// Result is what Run returns once the pipeline finishes (or is cancelled).
type Result struct {
	SessionID    int64
	StartIndex   int    // first word index this run processed
	EndIndex     int    // last word index this run processed; -1 if none
	FinalStatus  string // storage.StatusCompleted or storage.StatusPaused
	WasResumed   bool
	WasCancelled bool
}

// Run executes the pipeline for cfg using deps. It blocks until either every
// remaining word index has been processed or ctx is cancelled. On
// cancellation the in-flight item finishes, the SQLite batch is flushed, and
// the session is marked paused. On natural completion the session is marked
// completed.
//
// stats may be nil; when supplied, the function updates its atomic counters
// live so a dashboard goroutine can read them.
func Run(ctx context.Context, cfg Config, deps Dependencies, stats *Stats) (Result, error) {
	if err := validateConfig(cfg); err != nil {
		return Result{}, err
	}
	if deps.Repository == nil || deps.Deriver == nil || deps.Checker == nil || deps.Iterator == nil {
		return Result{}, errors.New("pipeline: dependencies must be non-nil")
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	templateHash := hashTemplate(cfg.Template)
	sig := storage.SessionSignature{
		TemplateHash: templateHash,
		Position:     cfg.Position,
		API:          cfg.API,
		AddressType:  string(cfg.ScriptType),
		NAddresses:   cfg.NAddresses,
	}
	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	init := storage.SessionInit{
		SessionSignature: sig,
		Template:         strings.Join(cfg.Template, " "),
		Rate:             cfg.Rate,
		WordlistPath:     cfg.WordlistPath,
		Workers:          workers,
	}

	if cfg.Fresh {
		// Best-effort: retire any paused/running session for this signature so
		// the caller starts at index 0. The MarkPausedAsCompleted helper does
		// this in a single statement.
		if _, err := deps.Repository.MarkPausedAsCompleted(ctx, sig); err != nil {
			return Result{}, fmt.Errorf("pipeline: clear paused session: %w", err)
		}
	}

	resumeIdx, err := deps.Repository.Resume(ctx, sig)
	if err != nil {
		return Result{}, fmt.Errorf("pipeline: resume lookup: %w", err)
	}
	startIdx := resumeIdx + 1
	wasResumed := resumeIdx >= 0

	if startIdx >= wordlistSize {
		// Nothing to do — already complete.
		sessionID, err := deps.Repository.BeginSession(ctx, init)
		if err != nil {
			return Result{}, fmt.Errorf("pipeline: begin session: %w", err)
		}
		if err := deps.Repository.EndSession(ctx, sessionID, storage.StatusCompleted); err != nil {
			return Result{}, fmt.Errorf("pipeline: end session: %w", err)
		}
		return Result{
			SessionID:   sessionID,
			StartIndex:  startIdx,
			EndIndex:    -1,
			FinalStatus: storage.StatusCompleted,
			WasResumed:  true,
		}, nil
	}

	sessionID, err := deps.Repository.BeginSession(ctx, init)
	if err != nil {
		return Result{}, fmt.Errorf("pipeline: begin session: %w", err)
	}

	if stats != nil {
		stats.SessionID.Store(sessionID)
		if wasResumed {
			stats.ResumedAt.Store(int64(resumeIdx))
		} else {
			stats.ResumedAt.Store(-1)
		}
	}

	// Wire the five stages:
	//   generator → candidates → [N derivers] → derivedRaw → reorder → derived → checker → checked → logger
	//
	// The reorder stage is what makes parallel derivers safe for resume:
	// even though the N workers may finish out of order, the reorder goroutine
	// emits items in strict word_index order, so the checker → logger
	// committed prefix advances monotonically and last_word_index is always
	// the highest contiguous index processed.
	candidates := make(chan Candidate, max(batchSize, workers*2))
	derivedRaw := make(chan Derived, max(batchSize, workers*2))
	derived := make(chan Derived, batchSize)
	checked := make(chan Checked, batchSize)

	genErrCh := make(chan error, 1)
	go func() {
		defer close(candidates)
		genErrCh <- generate(ctx, cfg, deps.Iterator, startIdx, candidates)
	}()

	// Spawn N derivers. They share the candidates channel and write to
	// derivedRaw. derivedRaw is closed once every worker has returned.
	var derivWG sync.WaitGroup
	for w := 0; w < workers; w++ {
		derivWG.Add(1)
		go func() {
			defer derivWG.Done()
			derive(ctx, cfg, deps.Deriver, candidates, derivedRaw)
		}()
	}
	go func() {
		derivWG.Wait()
		close(derivedRaw)
	}()

	// Reorder: takes Derived in any order, emits in word_index order
	// starting from startIdx. Uses a small bounded pending map (≤ workers).
	go func() {
		defer close(derived)
		reorder(ctx, startIdx, derivedRaw, derived)
	}()

	go func() {
		defer close(checked)
		check(ctx, deps.Checker, derived, checked, stats)
	}()

	// Logger runs in this goroutine so we can capture the result and any
	// flush errors directly. It returns the highest word index successfully
	// committed and a flag for whether ctx was cancelled mid-run.
	endIdx, cancelled, logErr := logResults(ctx, deps.Repository, sessionID, batchSize, checked, stats)

	// Drain generator error (the goroutine closed candidates already so it has
	// either returned ctx.Err() or nil). We treat ctx.Canceled as expected.
	genErr := <-genErrCh
	if genErr != nil && !errors.Is(genErr, context.Canceled) && !errors.Is(genErr, context.DeadlineExceeded) {
		return Result{}, fmt.Errorf("pipeline: generator: %w", genErr)
	}
	if logErr != nil {
		return Result{}, fmt.Errorf("pipeline: logger: %w", logErr)
	}

	// Decide final status. We compute it independently of cancelled so that a
	// run that exhausted the keyspace exactly when ctx was cancelled is still
	// marked completed (the work IS done).
	finalStatus := storage.StatusPaused
	if endIdx == wordlistSize-1 {
		finalStatus = storage.StatusCompleted
	} else if !cancelled {
		// Generator finished without producing any error AND we hit the end
		// of the iterator naturally — that also counts as completed.
		finalStatus = storage.StatusCompleted
	}

	// Use a fresh context for the final EndSession update so a cancelled
	// caller still records the paused state.
	endCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := deps.Repository.EndSession(endCtx, sessionID, finalStatus); err != nil {
		return Result{}, fmt.Errorf("pipeline: end session: %w", err)
	}

	return Result{
		SessionID:    sessionID,
		StartIndex:   startIdx,
		EndIndex:     endIdx,
		FinalStatus:  finalStatus,
		WasResumed:   wasResumed,
		WasCancelled: cancelled,
	}, nil
}

func validateConfig(cfg Config) error {
	if len(cfg.Template) != 12 {
		return fmt.Errorf("pipeline: template must be 12 words, got %d", len(cfg.Template))
	}
	if cfg.Position < 0 || cfg.Position >= 12 {
		return fmt.Errorf("pipeline: position out of range: %d", cfg.Position)
	}
	if cfg.NAddresses <= 0 {
		return fmt.Errorf("pipeline: n_addresses must be > 0, got %d", cfg.NAddresses)
	}
	if cfg.ScriptType != derivation.ScriptLegacy && cfg.ScriptType != derivation.ScriptSegwit {
		return fmt.Errorf("pipeline: invalid script type %q", cfg.ScriptType)
	}
	if strings.TrimSpace(cfg.API) == "" {
		return errors.New("pipeline: api must be set")
	}
	return nil
}

// generate emits Candidate values for word indices in [startIdx, 2048).
func generate(ctx context.Context, cfg Config, it Iterator, startIdx int, out chan<- Candidate) error {
	for i := startIdx; i < wordlistSize; i++ {
		mnemonic, err := it.CandidateAt(cfg.Template, cfg.Position, i)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- Candidate{WordIndex: i, Mnemonic: mnemonic}:
		}
	}
	return nil
}

// derive consumes Candidates and emits Derived. Mnemonics that fail the
// BIP-39 checksum pass through with ValidChecksum=false (so they still get
// logged) but no addresses and no balance lookup.
func derive(ctx context.Context, cfg Config, d Deriver, in <-chan Candidate, out chan<- Derived) {
	for c := range in {
		var (
			addrs []string
			valid = true
		)
		got, err := d.Derive(c.Mnemonic, cfg.NAddresses, cfg.ScriptType)
		if err != nil {
			if errors.Is(err, derivation.ErrInvalidMnemonic) {
				valid = false
			} else {
				// Non-checksum errors propagate as a "valid but addressless"
				// row with ValidChecksum=true and the error captured in the
				// downstream stage. For now we treat them like invalid
				// mnemonics and let the user inspect the DB.
				valid = false
			}
		} else {
			addrs = got
		}

		select {
		case <-ctx.Done():
			return
		case out <- Derived{Candidate: c, Addresses: addrs, ValidChecksum: valid}:
		}
	}
}

// reorder consumes Derived items in arbitrary order and emits them in
// strictly ascending WordIndex order, starting from startIdx.
//
// It exists because the deriver pool may finish work out of order: worker A
// might complete index 5 before worker B finishes index 3. The downstream
// checker → logger needs in-order delivery so the SQLite checkpoint
// (last_word_index) always reflects the highest CONTIGUOUS index processed,
// which is what makes resume correct after a Ctrl+C.
//
// pending holds at most `workers` items at any time (one per in-flight
// worker), so memory is bounded and small.
func reorder(ctx context.Context, startIdx int, in <-chan Derived, out chan<- Derived) {
	next := startIdx
	pending := make(map[int]Derived)
	emit := func(d Derived) bool {
		select {
		case <-ctx.Done():
			return false
		case out <- d:
			return true
		}
	}
	drainContiguous := func() bool {
		for {
			p, ok := pending[next]
			if !ok {
				return true
			}
			delete(pending, next)
			if !emit(p) {
				return false
			}
			next++
		}
	}
	for d := range in {
		if d.WordIndex == next {
			if !emit(d) {
				return
			}
			next++
			if !drainContiguous() {
				return
			}
		} else {
			pending[d.WordIndex] = d
		}
	}
	// Input closed: only flush items that are contiguous from `next`.
	// Anything past a gap is dropped — those indices were not derived
	// (probably because ctx was cancelled mid-run) and will be re-processed
	// on the next "seed-hunter run" because the resume checkpoint will be
	// the highest contiguous index actually emitted.
	drainContiguous()
}

// check consumes Derived and emits Checked. Invalid mnemonics short-circuit
// to BalanceSats=0 without contacting the upstream API.
func check(ctx context.Context, c Checker, in <-chan Derived, out chan<- Checked, stats *Stats) {
	for d := range in {
		started := time.Now()
		var (
			balance int64
			cerr    error
		)
		if d.ValidChecksum && len(d.Addresses) > 0 {
			balance, cerr = c.CheckAddresses(ctx, d.Addresses)
		}
		dur := time.Since(started).Milliseconds()
		out <- Checked{
			Derived:     d,
			BalanceSats: balance,
			CheckErr:    cerr,
			DurationMS:  dur,
			CheckedAt:   time.Now(),
		}
		_ = stats // stats are bumped in the logger after a successful commit
	}
}

// logResults batches Checked into SQLite and updates the session checkpoint.
// It returns the highest word index successfully committed (or -1 if nothing
// was committed), a flag indicating ctx was cancelled mid-run, and any
// non-recoverable error.
func logResults(
	ctx context.Context,
	repo *storage.Repository,
	sessionID int64,
	batchSize int,
	in <-chan Checked,
	stats *Stats,
) (int, bool, error) {
	highest := -1
	cancelled := false

	batch := make([]storage.Attempt, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		// Use a background context for the actual write so a cancelled
		// caller still gets its data persisted.
		writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := repo.InsertAttempts(writeCtx, batch); err != nil {
			return err
		}
		// Checkpoint the highest word index in the batch.
		topIdx := batch[len(batch)-1].WordIndex
		if err := repo.Checkpoint(writeCtx, sessionID, topIdx); err != nil {
			return err
		}
		highest = topIdx
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case ch, ok := <-in:
			if !ok {
				if err := flush(); err != nil {
					return highest, cancelled, err
				}
				return highest, cancelled, nil
			}
			batch = append(batch, toAttempt(sessionID, ch))
			if stats != nil {
				stats.Processed.Add(1)
				if ch.ValidChecksum {
					stats.ValidMnemonics.Add(1)
				}
				if ch.BalanceSats > 0 {
					stats.Hits.Add(1)
				}
				if ch.CheckErr != nil {
					stats.Errors.Add(1)
				}
			}
			if len(batch) >= batchSize {
				if err := flush(); err != nil {
					return highest, cancelled, err
				}
			}
		case <-ctx.Done():
			cancelled = true
			// Drain anything still in the channel so we don't drop work that
			// the upstream stages already produced before noticing the
			// cancel.
			for {
				select {
				case ch, ok := <-in:
					if !ok {
						if err := flush(); err != nil {
							return highest, cancelled, err
						}
						return highest, cancelled, nil
					}
					batch = append(batch, toAttempt(sessionID, ch))
					if stats != nil {
						stats.Processed.Add(1)
						if ch.ValidChecksum {
							stats.ValidMnemonics.Add(1)
						}
						if ch.BalanceSats > 0 {
							stats.Hits.Add(1)
						}
						if ch.CheckErr != nil {
							stats.Errors.Add(1)
						}
					}
					if len(batch) >= batchSize {
						if err := flush(); err != nil {
							return highest, cancelled, err
						}
					}
				default:
					if err := flush(); err != nil {
						return highest, cancelled, err
					}
					return highest, cancelled, nil
				}
			}
		}
	}
}

func toAttempt(sessionID int64, c Checked) storage.Attempt {
	addrJSON, _ := json.Marshal(c.Addresses)
	if c.Addresses == nil {
		addrJSON = []byte("[]")
	}
	errStr := ""
	if c.CheckErr != nil {
		errStr = c.CheckErr.Error()
	}
	return storage.Attempt{
		SessionID:     sessionID,
		WordIndex:     c.WordIndex,
		MnemonicHash:  bip39.Fingerprint(c.Mnemonic),
		AddressesJSON: string(addrJSON),
		BalanceSats:   c.BalanceSats,
		ValidChecksum: c.ValidChecksum,
		Error:         errStr,
		DurationMS:    c.DurationMS,
		CheckedAtUnix: c.CheckedAt.Unix(),
	}
}

func hashTemplate(template []string) string {
	h := sha256.Sum256([]byte(strings.Join(template, " ")))
	return hex.EncodeToString(h[:])
}
