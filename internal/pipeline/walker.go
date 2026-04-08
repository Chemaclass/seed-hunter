package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/bip39"
	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

// WalkConfig drives Walk. Most fields parallel pipeline.Config; the
// keyspace walker doesn't need a Template (it generates a fresh mnemonic
// for every cursor) but it does need the loaded wordlist so it can index
// into it.
type WalkConfig struct {
	Words        []string // loaded BIP-39 wordlist (must be 2048 entries)
	NAddresses   int
	ScriptType   derivation.ScriptType
	API          string
	Rate         float64
	WordlistPath string
	BatchSize    int
	Fresh        bool // ignore any paused walk session and start at cursor zero
}

// WalkDependencies bundles the collaborators Walk needs. Iterator is NOT
// part of this — Walk does its own enumeration via Cursor.
type WalkDependencies struct {
	Repository *storage.Repository
	Deriver    Deriver
	Checker    Checker
}

// WalkResult is what Walk returns once the loop exits (or is cancelled).
// FinalStatus is StatusPaused on Ctrl+C / ctx-cancel and StatusCompleted
// on the (impossible) full-keyspace overflow.
type WalkResult struct {
	SessionID    int64
	StartCursor  Cursor
	EndCursor    Cursor
	FinalStatus  string
	WasResumed   bool
	WasCancelled bool
	Processed    int64 // candidates processed in THIS run
}

// walkSignature builds a SessionSignature for walk-mode resume. The walk
// has no template, so we use a fixed sentinel hash for the template_hash
// column and Position=-1. The (api, address_type, n_addresses) fields
// still contribute, so users can run sweeps and walks against different
// providers without colliding.
func walkSignature(cfg WalkConfig) storage.SessionSignature {
	return storage.SessionSignature{
		TemplateHash: walkTemplateHashSentinel,
		Position:     -1,
		API:          cfg.API,
		AddressType:  string(cfg.ScriptType),
		NAddresses:   cfg.NAddresses,
	}
}

// walkTemplateHashSentinel is a fixed marker that distinguishes walk-mode
// session rows from sweep-mode rows. It is intentionally not a SHA-256 of
// anything so it never collides with a real template hash.
const walkTemplateHashSentinel = "__walk__"

// Walk runs the full-keyspace walk. It iterates the 2048^12 keyspace
// cursor-by-cursor, validating each candidate's BIP-39 checksum, deriving
// receiving addresses for the valid ones, and querying the balance API
// (rate-limited). It blocks until ctx is cancelled or the keyspace is
// exhausted (the latter will not happen in your lifetime).
//
// On entry, Walk looks up the latest paused walk-mode session and resumes
// from its persisted cursor. If cfg.Fresh is true, any such session is
// retired before the walker starts at cursor zero.
func Walk(ctx context.Context, cfg WalkConfig, deps WalkDependencies, stats *Stats) (WalkResult, error) {
	if err := validateWalkConfig(cfg); err != nil {
		return WalkResult{}, err
	}
	if deps.Repository == nil || deps.Deriver == nil || deps.Checker == nil {
		return WalkResult{}, errors.New("walk: dependencies must be non-nil")
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	sig := walkSignature(cfg)

	if cfg.Fresh {
		if _, err := deps.Repository.MarkPausedAsCompleted(ctx, sig); err != nil {
			return WalkResult{}, fmt.Errorf("walk: clear paused session: %w", err)
		}
	}

	// Look up resumable cursor (independent from BeginSession so we can
	// read it BEFORE the row gets refreshed by BeginSession).
	resumeCursorStr, wasResumed, err := loadResumableWalkCursor(ctx, deps.Repository, sig)
	if err != nil {
		return WalkResult{}, err
	}

	cursor, err := ParseCursor(resumeCursorStr)
	if err != nil {
		return WalkResult{}, fmt.Errorf("walk: parse resume cursor: %w", err)
	}
	if wasResumed {
		// The persisted cursor is the LAST one processed; advance one
		// before starting so we don't re-process it.
		if cursor.Inc() {
			// Already at the end of the keyspace (impossible).
			return WalkResult{
				FinalStatus: storage.StatusCompleted,
				WasResumed:  true,
			}, nil
		}
	}

	startCursor := cursor

	init := storage.SessionInit{
		SessionSignature: sig,
		Mode:             storage.ModeWalk,
		Cursor:           cursor.String(),
		Rate:             cfg.Rate,
		WordlistPath:     cfg.WordlistPath,
	}
	sessionID, err := deps.Repository.BeginSession(ctx, init)
	if err != nil {
		return WalkResult{}, fmt.Errorf("walk: begin session: %w", err)
	}

	if stats != nil {
		stats.SessionID.Store(sessionID)
		// ResumedAt is unused in walk mode; set to -1 so the dashboard
		// doesn't render a stale "[resumed at N]" badge.
		stats.ResumedAt.Store(-1)
		s := cursor.String()
		stats.Cursor.Store(&s)
	}

	batch := make([]storage.Attempt, 0, batchSize)
	flush := func(currentCursor Cursor) error {
		if len(batch) == 0 {
			return nil
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := deps.Repository.InsertAttempts(writeCtx, batch); err != nil {
			return err
		}
		s := currentCursor.String()
		if err := deps.Repository.CheckpointCursor(writeCtx, sessionID, s); err != nil {
			return err
		}
		if stats != nil {
			stats.Cursor.Store(&s)
		}
		batch = batch[:0]
		return nil
	}

	cancelled := false
	overflow := false
walkloop:
	for {
		if err := ctx.Err(); err != nil {
			cancelled = true
			break walkloop
		}

		mnemonic, err := cursor.Mnemonic(cfg.Words)
		if err != nil {
			return WalkResult{}, fmt.Errorf("walk: build mnemonic: %w", err)
		}

		started := time.Now()
		var (
			addresses     []string
			validChecksum bool
			balance       int64
			cerr          error
		)
		got, derr := deps.Deriver.Derive(mnemonic, cfg.NAddresses, cfg.ScriptType)
		if derr == nil {
			addresses = got
			validChecksum = true
			balance, cerr = deps.Checker.CheckAddresses(ctx, addresses)
		}
		dur := time.Since(started).Milliseconds()

		addrJSON, _ := json.Marshal(addresses)
		if addresses == nil {
			addrJSON = []byte("[]")
		}
		errStr := ""
		if cerr != nil {
			errStr = cerr.Error()
		}

		batch = append(batch, storage.Attempt{
			SessionID:     sessionID,
			WordIndex:     0, // unused in walk mode; cursor is in session row
			MnemonicHash:  bip39.Fingerprint(mnemonic),
			AddressesJSON: string(addrJSON),
			BalanceSats:   balance,
			ValidChecksum: validChecksum,
			Error:         errStr,
			DurationMS:    dur,
			CheckedAtUnix: time.Now().Unix(),
		})

		if stats != nil {
			stats.Processed.Add(1)
			if validChecksum {
				stats.ValidMnemonics.Add(1)
			}
			if balance > 0 {
				stats.Hits.Add(1)
			}
			if cerr != nil {
				stats.Errors.Add(1)
			}
		}

		if len(batch) >= batchSize {
			if err := flush(cursor); err != nil {
				return WalkResult{}, fmt.Errorf("walk: flush: %w", err)
			}
		}

		if cursor.Inc() {
			// Full keyspace exhausted (impossible). Flush and exit.
			overflow = true
			break walkloop
		}
	}

	// Final flush before EndSession so the persisted cursor reflects the
	// last processed candidate even if we got cancelled mid-batch.
	//
	// On cancel: cursor points to the NEXT candidate to process (because
	// the last successful iteration already incremented). The last
	// successfully processed cursor is decCursor(cursor). decCursor
	// underflows to zero safely so the "cancel before first process"
	// case (where the batch is empty and flush is a no-op) is harmless.
	//
	// On overflow: cursor IS the wrap-to-zero result of Inc, but the
	// last processed is the all-2047s cursor — we leave finalCursor as
	// the wrapped zero since the session is going to be marked completed
	// anyway and resume is meaningless after a full sweep.
	finalCursor := cursor
	if !overflow {
		finalCursor = decCursor(cursor)
	}
	if err := flush(finalCursor); err != nil {
		return WalkResult{}, fmt.Errorf("walk: final flush: %w", err)
	}

	finalStatus := storage.StatusPaused
	if overflow {
		finalStatus = storage.StatusCompleted
	}

	endCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := deps.Repository.EndSession(endCtx, sessionID, finalStatus); err != nil {
		return WalkResult{}, fmt.Errorf("walk: end session: %w", err)
	}

	res := WalkResult{
		SessionID:    sessionID,
		StartCursor:  startCursor,
		EndCursor:    finalCursor,
		FinalStatus:  finalStatus,
		WasResumed:   wasResumed,
		WasCancelled: cancelled,
	}
	if stats != nil {
		res.Processed = stats.Processed.Load()
	}
	return res, nil
}

func validateWalkConfig(cfg WalkConfig) error {
	if len(cfg.Words) != CursorBase {
		return fmt.Errorf("walk: wordlist must have %d entries, got %d", CursorBase, len(cfg.Words))
	}
	if cfg.NAddresses < 1 {
		return fmt.Errorf("walk: n_addresses must be >= 1, got %d", cfg.NAddresses)
	}
	if cfg.ScriptType != derivation.ScriptLegacy && cfg.ScriptType != derivation.ScriptSegwit {
		return fmt.Errorf("walk: invalid script type %q", cfg.ScriptType)
	}
	if strings.TrimSpace(cfg.API) == "" {
		return errors.New("walk: api must be set")
	}
	return nil
}

// loadResumableWalkCursor reads the latest paused/running session matching
// the walk signature and returns its persisted cursor. The boolean is true
// when there is something to resume; otherwise the empty cursor is returned.
func loadResumableWalkCursor(ctx context.Context, repo *storage.Repository, sig storage.SessionSignature) (string, bool, error) {
	last, err := repo.LatestResumable(ctx)
	if err != nil {
		return "", false, fmt.Errorf("walk: lookup last session: %w", err)
	}
	if last == nil {
		return "", false, nil
	}
	if last.Mode != storage.ModeWalk {
		return "", false, nil
	}
	if last.TemplateHash != sig.TemplateHash || last.API != sig.API ||
		last.AddressType != sig.AddressType || last.NAddresses != sig.NAddresses {
		return "", false, nil
	}
	return last.Cursor, true, nil
}

// decCursor returns the cursor immediately before c (the previous step in
// the odometer). Used by Walk to identify the last *processed* cursor when
// the loop exits between processing one entry and incrementing for the
// next. If c is the zero cursor, the result is also the zero cursor (no
// underflow).
func decCursor(c Cursor) Cursor {
	for i := CursorLength - 1; i >= 0; i-- {
		if c[i] > 0 {
			c[i]--
			return c
		}
		c[i] = CursorBase - 1
	}
	// Underflow: return zero (this would only happen if c was already
	// zero, which means we never processed anything).
	return Cursor{}
}
