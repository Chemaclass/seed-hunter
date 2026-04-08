package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	extbip39 "github.com/tyler-smith/go-bip39"

	"github.com/Chemaclass/seed-hunter/config"
	"github.com/Chemaclass/seed-hunter/internal/bip39"
	"github.com/Chemaclass/seed-hunter/internal/checker"
	"github.com/Chemaclass/seed-hunter/internal/dashboard"
	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/pipeline"
	"github.com/Chemaclass/seed-hunter/internal/storage"
	"github.com/Chemaclass/seed-hunter/internal/wordlist"
)

var runFlags = config.Default()

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start (or resume) the brute-force loop across one or more positions",
	Long: `Run the brute-force loop.

With no flags, "seed-hunter run" picks up the most recent paused session
from the database and resumes it with all of its parameters intact —
template, position, addresses, api, script type, rate, wordlist. The DB is
the source of truth.

By default --positions is "0-11", which means the run sweeps every word
position sequentially: 12 × 2048 = 24,576 candidates total. After one
position is exhausted (2048 attempts), the next position starts
automatically. Pass --positions 5 to mutate just one position, or
--positions 0,3-5,9 to pick a custom subset.

Pass any flag to override the corresponding parameter for this run; the
flags you don't pass are inherited from the last paused session. Pass
--reset to ignore the last paused session and start a brand-new one (a
fresh template will be auto-generated unless you also pass --template).`,
	RunE: runE,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runFlags.ApplyEnv()

	f := runCmd.Flags()
	f.StringVar(&runFlags.DBPath, "db", runFlags.DBPath, "SQLite database path")
	f.StringVar(&runFlags.WordlistPath, "wordlist", runFlags.WordlistPath, "path to a 2048-word BIP-39 wordlist file (empty = embedded English)")
	f.StringVar(&runFlags.Template, "template", runFlags.Template, "12-word BIP-39 template (empty = inherit from last session, or generate a random demo seed)")
	f.StringVar(&runFlags.Positions, "positions", runFlags.Positions, "word positions to sweep: '5', '0-11', '0,3,7' or '0,3-5,9'")
	f.IntVar(&runFlags.NAddresses, "addresses", runFlags.NAddresses, "number of receiving addresses to derive per candidate")
	f.StringVar(&runFlags.API, "api", runFlags.API, "balance API: mempool|blockstream")
	f.StringVar(&runFlags.ScriptType, "script-type", runFlags.ScriptType, "address type: segwit|legacy")
	f.Float64Var(&runFlags.Rate, "rate", runFlags.Rate, "API requests per second")
	f.IntVar(&runFlags.Workers, "workers", runFlags.Workers, "number of parallel deriver goroutines (>= 1)")
	f.IntVar(&runFlags.APIWorkers, "api-workers", runFlags.APIWorkers, "number of API workers (currently always serialised by the rate limiter)")
	f.IntVar(&runFlags.BatchSize, "batch-size", runFlags.BatchSize, "SQLite insert batch size")
	f.BoolVar(&runFlags.Reset, "reset", false, "ignore the most recent paused session and start a brand-new one")
	f.BoolVar(&runFlags.NoDashboard, "no-dashboard", false, "disable the live dashboard (useful for non-TTY use)")
	f.BoolVar(&runFlags.NoWalk, "no-walk", false, "stop after the 12-position sweep; do NOT auto-transition to the full keyspace walk")
	f.BoolVar(&runFlags.SkipSweep, "skip-sweep", false, "skip the 12-position sweep and go straight to the full keyspace walk")
}

func runE(cmd *cobra.Command, _ []string) error {
	cfg := runFlags

	// Open the repo first so we can look up the last paused session BEFORE
	// validating cfg — we need to potentially inherit fields from it.
	repo, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = repo.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	inheritedPosition := -1
	resumeWalk := false
	if !cfg.Reset {
		last, err := repo.LatestResumable(ctx)
		if err != nil {
			return fmt.Errorf("lookup last session: %w", err)
		}
		if last != nil {
			inheritFromSession(&cfg, last, cmd.Flags())
			if last.Mode == storage.ModeWalk {
				// The latest paused session is a walk; jump straight to
				// walk mode and skip the sweep entirely. The walker will
				// itself find and resume the cursor.
				resumeWalk = true
				fmt.Fprintf(cmd.OutOrStdout(),
					"Resuming walk session #%d at cursor %s (use --reset to start over).\n",
					last.ID, last.Cursor,
				)
			} else {
				inheritedPosition = last.Position
				fmt.Fprintf(cmd.OutOrStdout(),
					"Resuming sweep session #%d at position %d, word index %d (use --reset to start over).\n",
					last.ID, last.Position, last.LastWordIndex+1,
				)
			}
		}
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	positions, err := config.ParsePositions(cfg.Positions)
	if err != nil {
		return fmt.Errorf("invalid --positions: %w", err)
	}

	// Load the wordlist AFTER inherit so an inherited WordlistPath wins.
	words, err := wordlist.Load(cfg.WordlistPath)
	if err != nil {
		return fmt.Errorf("load wordlist: %w", err)
	}
	extbip39.SetWordList(words)
	iterator, err := bip39.NewIterator(words)
	if err != nil {
		return fmt.Errorf("build iterator: %w", err)
	}

	workers := cfg.Workers // validated >= 1 above

	baseChecker, err := checker.New(checker.Provider(cfg.API), &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		return fmt.Errorf("build checker: %w", err)
	}
	rateLimited := checker.WithRateLimit(baseChecker, cfg.Rate)
	deriver := derivation.New()

	// If we're going straight into walk mode (resuming a walk session, or
	// the user passed --skip-sweep), skip template generation and the
	// sweep wiring entirely. The walker doesn't use a template.
	walkOnly := resumeWalk || cfg.SkipSweep
	if walkOnly {
		slog.Info("starting walk",
			"api", cfg.API,
			"script_type", cfg.ScriptType,
			"addresses", cfg.NAddresses,
			"rate", cfg.Rate,
			"reset", cfg.Reset,
		)
		return runWalk(ctx, cmd.OutOrStdout(), cfg, words, repo, deriver, rateLimited, !cfg.NoDashboard)
	}

	template, err := resolveTemplate(cfg.Template, cmd.OutOrStdout(), generateRandomMnemonic)
	if err != nil {
		return err
	}

	deps := pipeline.Dependencies{
		Repository: repo,
		Iterator:   iterator,
		Deriver:    deriver,
		Checker:    rateLimited,
	}

	slog.Info("starting sweep",
		"positions", cfg.Positions,
		"api", cfg.API,
		"script_type", cfg.ScriptType,
		"addresses", cfg.NAddresses,
		"rate", cfg.Rate,
		"workers", workers,
		"api_workers", cfg.APIWorkers,
		"batch_size", cfg.BatchSize,
		"reset", cfg.Reset,
	)

	// Determine the starting index in the positions list. If we're resuming
	// a paused session, jump straight to the position it was on; otherwise
	// start at the head of the list.
	startIdx := startIndexInPositions(positions, inheritedPosition)

	// Build the per-position runner. The closure captures everything except
	// the position itself, so the outer sweepPositions loop only needs to
	// pass the position number.
	runOne := func(ctx context.Context, position int) (pipeline.Result, error) {
		stats := pipeline.NewStats()
		var dashCancel context.CancelFunc = func() {}
		if !cfg.NoDashboard {
			dashCtx, c := context.WithCancel(ctx)
			dashCancel = c
			go dashboard.Run(dashCtx, cmd.OutOrStdout(), dashboard.Meta{
				TemplateHash: hashTemplate(template),
				Position:     position,
				API:          cfg.API,
				ScriptType:   cfg.ScriptType,
				Workers:      workers,
				APIWorkers:   cfg.APIWorkers,
				RateLimit:    cfg.Rate,
				NAddresses:   cfg.NAddresses,
			}, stats, 200*time.Millisecond)
		}

		pCfg := pipeline.Config{
			Template:      template,
			Position:      position,
			ScriptType:    derivation.ScriptType(cfg.ScriptType),
			NAddresses:    cfg.NAddresses,
			API:           cfg.API,
			Rate:          cfg.Rate,
			WordlistPath:  cfg.WordlistPath,
			Workers:       workers,
			PositionsSpec: cfg.Positions,
			BatchSize:     cfg.BatchSize,
			Fresh:         cfg.Reset,
		}
		res, err := pipeline.Run(ctx, pCfg, deps, stats)
		dashCancel()
		// Give the dashboard goroutine a moment to stop repainting before
		// we print the per-position summary, so the line isn't clobbered.
		time.Sleep(50 * time.Millisecond)
		return res, err
	}

	out := cmd.OutOrStdout()

	// Run the sweep first.
	swept, err := sweepPositionsAndReport(ctx, out, positions, startIdx, runOne)
	if err != nil {
		return err
	}

	// After a successful sweep that ran to completion (not paused, not
	// cancelled, not error), automatically transition into the keyspace
	// walk unless --no-walk was set.
	if swept == sweepCompleted && !cfg.NoWalk && ctx.Err() == nil {
		fmt.Fprintln(out, "\n──── full keyspace walk ────")
		fmt.Fprintln(out, "Sweep done. Continuing into the full 2048^12 keyspace walk.")
		fmt.Fprintln(out, "This will not finish in the lifetime of the universe. Press Ctrl+C any time.")
		return runWalk(ctx, out, cfg, words, repo, deriver, rateLimited, !cfg.NoDashboard)
	}
	return nil
}

// sweepOutcome reports how sweepPositionsAndReport finished, so the caller
// can decide whether to transition into the walk.
type sweepOutcome int

const (
	sweepCompleted sweepOutcome = iota // every position completed
	sweepPaused                        // a position paused (Ctrl+C)
	sweepNoWork                        // startIdx was past the end (nothing to do)
)

// sweepPositionsAndReport is a thin wrapper around sweepPositions that
// also tells the caller whether the sweep ran to natural completion (so
// that runE knows whether to chain into the keyspace walk).
func sweepPositionsAndReport(ctx context.Context, out io.Writer, positions []int, startIdx int, runOne runOneFn) (sweepOutcome, error) {
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(positions) {
		fmt.Fprintln(out, "nothing to sweep: starting index is past the end of the positions list")
		return sweepNoWork, nil
	}
	for i := startIdx; i < len(positions); i++ {
		if err := ctx.Err(); err != nil {
			return sweepPaused, nil
		}
		pos := positions[i]
		fmt.Fprintf(out, "\n──── position %d (%d/%d) ────\n", pos, i+1, len(positions))
		res, err := runOne(ctx, pos)
		if err != nil {
			return sweepPaused, fmt.Errorf("position %d: %w", pos, err)
		}
		fmt.Fprintf(out,
			"position %d: status=%s session=%d end_index=%d resumed=%t cancelled=%t\n",
			pos, res.FinalStatus, res.SessionID, res.EndIndex, res.WasResumed, res.WasCancelled,
		)
		if res.FinalStatus == storage.StatusPaused {
			fmt.Fprintln(out,
				"\nPosition paused. Run 'seed-hunter run' (no flags) to resume from where this stopped.")
			return sweepPaused, nil
		}
	}
	fmt.Fprintf(out,
		"\nSweep complete: %d positions × 2048 = %d candidates exhausted.\n",
		len(positions)-startIdx, (len(positions)-startIdx)*2048,
	)
	return sweepCompleted, nil
}

// runWalk runs the full-keyspace walker with its own dashboard goroutine.
// It is called either as the next phase after a successful sweep or
// directly when --skip-sweep is set or the resumed session is a walk.
func runWalk(
	ctx context.Context,
	out io.Writer,
	cfg config.Config,
	words []string,
	repo *storage.Repository,
	deriver pipeline.Deriver,
	chk pipeline.Checker,
	withDashboard bool,
) error {
	stats := pipeline.NewStats()
	var dashCancel context.CancelFunc = func() {}
	if withDashboard {
		dashCtx, c := context.WithCancel(ctx)
		dashCancel = c
		go dashboard.Run(dashCtx, out, dashboard.Meta{
			Mode:       dashboard.ModeWalk,
			API:        cfg.API,
			ScriptType: cfg.ScriptType,
			Workers:    cfg.Workers,
			APIWorkers: cfg.APIWorkers,
			RateLimit:  cfg.Rate,
			NAddresses: cfg.NAddresses,
		}, stats, 200*time.Millisecond)
	}

	walkCfg := pipeline.WalkConfig{
		Words:        words,
		NAddresses:   cfg.NAddresses,
		ScriptType:   derivation.ScriptType(cfg.ScriptType),
		API:          cfg.API,
		Rate:         cfg.Rate,
		WordlistPath: cfg.WordlistPath,
		BatchSize:    cfg.BatchSize,
		Fresh:        cfg.Reset,
	}
	res, err := pipeline.Walk(ctx, walkCfg, pipeline.WalkDependencies{
		Repository: repo,
		Deriver:    deriver,
		Checker:    chk,
	}, stats)
	dashCancel()
	time.Sleep(50 * time.Millisecond)
	if err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out,
		"walk %s: session=%d processed=%d cursor=%s\n",
		res.FinalStatus, res.SessionID, res.Processed, res.EndCursor,
	)
	if res.FinalStatus == storage.StatusPaused {
		fmt.Fprintln(out,
			"Walk paused. Run 'seed-hunter run' (no flags) to resume from where this stopped.")
	}
	return nil
}

// runOneFn is the per-position pipeline runner. sweepPositionsAndReport
// calls it once per position. It is parameterised so the test can pass a
// deterministic stub instead of a real pipeline.
type runOneFn func(ctx context.Context, position int) (pipeline.Result, error)

// startIndexInPositions returns the index of `current` in `positions`, or
// 0 if `current` is not in the list (start at the head). A negative
// `current` (no inherited position) also yields 0.
func startIndexInPositions(positions []int, current int) int {
	if current < 0 {
		return 0
	}
	for i, p := range positions {
		if p == current {
			return i
		}
	}
	return 0
}

// inheritFromSession overlays fields from last onto cfg, but only for flags
// the user did NOT explicitly set on the command line. The user's --template,
// --positions, etc. always win; everything else inherits from the DB so that
// "seed-hunter run" with zero flags is enough to continue the previous run.
func inheritFromSession(cfg *config.Config, last *storage.Session, flags *pflag.FlagSet) {
	if !flags.Changed("template") && last.Template != "" {
		cfg.Template = last.Template
	}
	if !flags.Changed("positions") && last.PositionsSpec != "" {
		cfg.Positions = last.PositionsSpec
	}
	if !flags.Changed("addresses") {
		cfg.NAddresses = last.NAddresses
	}
	if !flags.Changed("api") {
		cfg.API = last.API
	}
	if !flags.Changed("script-type") {
		cfg.ScriptType = last.AddressType
	}
	if !flags.Changed("rate") && last.Rate > 0 {
		cfg.Rate = last.Rate
	}
	if !flags.Changed("wordlist") {
		cfg.WordlistPath = last.WordlistPath
	}
	if !flags.Changed("workers") && last.Workers > 0 {
		cfg.Workers = last.Workers
	}
}

// resolveTemplate returns the 12-word template the run will iterate over.
//
// If raw is non-empty it is parsed and returned (the user supplied
// --template, possibly via inheritance from the last session). Otherwise
// generate() produces a fresh random mnemonic and the "DO NOT FUND" notice
// is printed to out.
func resolveTemplate(raw string, out io.Writer, generate func() (string, error)) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		words := strings.Fields(raw)
		if len(words) != 12 {
			return nil, fmt.Errorf("template must be 12 words, got %d", len(words))
		}
		return words, nil
	}

	mnemonic, err := generate()
	if err != nil {
		return nil, err
	}

	fmt.Fprintln(out, "──── demo seed (DO NOT FUND) ───────────────────────────────")
	fmt.Fprintln(out, mnemonic)
	fmt.Fprintln(out, "This mnemonic was generated locally for educational use only.")
	fmt.Fprintln(out, "────────────────────────────────────────────────────────────")
	return strings.Fields(mnemonic), nil
}

// generateRandomMnemonic produces a fresh 12-word mnemonic using the
// process-global wordlist (which cmd/run.go binds to whatever wordlist file
// the user loaded). It is the default `generate` callback for resolveTemplate
// in production; tests inject a deterministic stub instead.
func generateRandomMnemonic() (string, error) {
	entropy, err := extbip39.NewEntropy(128)
	if err != nil {
		return "", fmt.Errorf("generate entropy: %w", err)
	}
	mnemonic, err := extbip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("generate mnemonic: %w", err)
	}
	return mnemonic, nil
}

func hashTemplate(template []string) string {
	// Local re-hash for the dashboard meta. The pipeline computes the same
	// hash internally for the session signature.
	return pipelineTemplateHash(strings.Join(template, " "))
}
