package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
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
	Short: "Start (or resume) the brute-force loop",
	Long: `Run the brute-force loop.

With no flags, "seed-hunter run" picks up the most recent paused session
from the database and resumes it with all of its parameters intact —
template, position, addresses, api, script type, rate, wordlist. The DB is
the source of truth.

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
	f.IntVar(&runFlags.Position, "position", runFlags.Position, "word position to mutate (0-11)")
	f.IntVar(&runFlags.NAddresses, "addresses", runFlags.NAddresses, "number of receiving addresses to derive per candidate")
	f.StringVar(&runFlags.API, "api", runFlags.API, "balance API: mempool|blockstream")
	f.StringVar(&runFlags.ScriptType, "script-type", runFlags.ScriptType, "address type: segwit|legacy")
	f.Float64Var(&runFlags.Rate, "rate", runFlags.Rate, "API requests per second")
	f.IntVar(&runFlags.DeriveWorkers, "derive-workers", runFlags.DeriveWorkers, "number of derivation workers (0 = NumCPU)")
	f.IntVar(&runFlags.APIWorkers, "api-workers", runFlags.APIWorkers, "number of API workers")
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

	if !cfg.Reset {
		last, err := repo.LatestResumable(ctx)
		if err != nil {
			return fmt.Errorf("lookup last session: %w", err)
		}
		if last != nil {
			inheritFromSession(&cfg, last, cmd.Flags())
			fmt.Fprintf(cmd.OutOrStdout(),
				"Resuming session #%d at word index %d (use --reset to start over).\n",
				last.ID, last.LastWordIndex+1,
			)
		}
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Load the wordlist AFTER inherit so an inherited WordlistPath wins. The
	// underlying tyler-smith/go-bip39 library uses a process-global wordlist
	// for checksum validation and PBKDF2 seed derivation; binding it to the
	// loaded list keeps the iterator and the deriver in sync.
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

	deriveWorkers := cfg.DeriveWorkers
	if deriveWorkers <= 0 {
		deriveWorkers = runtime.NumCPU()
	}

	baseChecker, err := checker.New(checker.Provider(cfg.API), &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		return fmt.Errorf("build checker: %w", err)
	}
	rateLimited := checker.WithRateLimit(baseChecker, cfg.Rate)

	pipelineCfg := pipeline.Config{
		Template:     template,
		Position:     cfg.Position,
		ScriptType:   derivation.ScriptType(cfg.ScriptType),
		NAddresses:   cfg.NAddresses,
		API:          cfg.API,
		Rate:         cfg.Rate,
		WordlistPath: cfg.WordlistPath,
		BatchSize:    cfg.BatchSize,
		Fresh:        cfg.Reset,
	}
	deps := pipeline.Dependencies{
		Repository: repo,
		Iterator:   iterator,
		Deriver:    derivation.New(),
		Checker:    rateLimited,
	}

	stats := pipeline.NewStats()

	slog.Info("starting run",
		"position", cfg.Position,
		"api", cfg.API,
		"script_type", cfg.ScriptType,
		"addresses", cfg.NAddresses,
		"rate", cfg.Rate,
		"derive_workers", deriveWorkers,
		"api_workers", cfg.APIWorkers,
		"batch_size", cfg.BatchSize,
		"reset", cfg.Reset,
	)

	if !cfg.NoDashboard {
		go dashboard.Run(ctx, cmd.OutOrStdout(), dashboard.Meta{
			TemplateHash:  hashTemplate(template),
			Position:      cfg.Position,
			API:           cfg.API,
			ScriptType:    cfg.ScriptType,
			DeriveWorkers: deriveWorkers,
			APIWorkers:    cfg.APIWorkers,
			RateLimit:     cfg.Rate,
			NAddresses:    cfg.NAddresses,
		}, stats, 200*time.Millisecond)
	}

	res, err := pipeline.Run(ctx, pipelineCfg, deps, stats)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(),
		"run finished: status=%s session=%d end_index=%d resumed=%t cancelled=%t\n",
		res.FinalStatus, res.SessionID, res.EndIndex, res.WasResumed, res.WasCancelled,
	)
	if res.FinalStatus == storage.StatusPaused {
		fmt.Fprintln(cmd.OutOrStdout(),
			"Run 'seed-hunter run' (no flags) to resume from where this stopped.")
	}
	return nil
}

// inheritFromSession overlays fields from last onto cfg, but only for flags
// the user did NOT explicitly set on the command line. The user's --template,
// --position, etc. always win; everything else inherits from the DB so that
// "seed-hunter run" with zero flags is enough to continue the previous run.
func inheritFromSession(cfg *config.Config, last *storage.Session, flags *pflag.FlagSet) {
	if !flags.Changed("template") && last.Template != "" {
		cfg.Template = last.Template
	}
	if !flags.Changed("position") {
		cfg.Position = last.Position
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
