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
	if !cfg.Reset {
		last, err := repo.LatestResumable(ctx)
		if err != nil {
			return fmt.Errorf("lookup last session: %w", err)
		}
		if last != nil {
			inheritFromSession(&cfg, last, cmd.Flags())
			inheritedPosition = last.Position
			fmt.Fprintf(cmd.OutOrStdout(),
				"Resuming session #%d at position %d, word index %d (use --reset to start over).\n",
				last.ID, last.Position, last.LastWordIndex+1,
			)
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

	template, err := resolveTemplate(cfg.Template, cmd.OutOrStdout(), generateRandomMnemonic)
	if err != nil {
		return err
	}

	workers := cfg.Workers // validated >= 1 above

	baseChecker, err := checker.New(checker.Provider(cfg.API), &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		return fmt.Errorf("build checker: %w", err)
	}
	rateLimited := checker.WithRateLimit(baseChecker, cfg.Rate)

	deps := pipeline.Dependencies{
		Repository: repo,
		Iterator:   iterator,
		Deriver:    derivation.New(),
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

	return sweepPositions(ctx, cmd.OutOrStdout(), positions, startIdx, runOne)
}

// runOneFn is the per-position pipeline runner. sweepPositions calls it
// once per position in the swept list. It is parameterised so the test for
// sweepPositions can pass a deterministic stub instead of a real pipeline.
type runOneFn func(ctx context.Context, position int) (pipeline.Result, error)

// sweepPositions runs runOne for each position in positions[startIdx:], in
// order. It honors:
//
//   - context cancellation: aborts the sweep without error
//   - status=paused (Ctrl+C inside a position): aborts the sweep without error
//   - status=completed: prints a per-position summary and continues
//
// When all positions in the slice complete, it prints a final "all
// positions exhausted" summary. Any error from runOne is returned wrapped.
func sweepPositions(ctx context.Context, out io.Writer, positions []int, startIdx int, runOne runOneFn) error {
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(positions) {
		fmt.Fprintln(out, "nothing to sweep: starting index is past the end of the positions list")
		return nil
	}

	for i := startIdx; i < len(positions); i++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		pos := positions[i]
		fmt.Fprintf(out, "\n──── position %d (%d/%d) ────\n", pos, i+1, len(positions))

		res, err := runOne(ctx, pos)
		if err != nil {
			return fmt.Errorf("position %d: %w", pos, err)
		}

		fmt.Fprintf(out,
			"position %d: status=%s session=%d end_index=%d resumed=%t cancelled=%t\n",
			pos, res.FinalStatus, res.SessionID, res.EndIndex, res.WasResumed, res.WasCancelled,
		)

		if res.FinalStatus == storage.StatusPaused {
			fmt.Fprintln(out,
				"\nPosition paused. Run 'seed-hunter run' (no flags) to resume from where this stopped.")
			return nil
		}
	}

	fmt.Fprintf(out,
		"\nSweep complete: %d positions × 2048 = %d candidates exhausted.\n",
		len(positions)-startIdx, (len(positions)-startIdx)*2048,
	)
	fmt.Fprintln(out,
		"Reminder: this is 12 × 2048 = 24,576 attempts at most. The actual BIP-39")
	fmt.Fprintln(out,
		"keyspace is 2048^12 ≈ 5.4e+39. We just covered an infinitesimal slice.")
	return nil
}

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
