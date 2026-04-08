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
	extbip39 "github.com/tyler-smith/go-bip39"

	"github.com/Chemaclass/seed-hunter/config"
	"github.com/Chemaclass/seed-hunter/internal/checker"
	"github.com/Chemaclass/seed-hunter/internal/dashboard"
	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/pipeline"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

var runFlags = config.Default()

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the brute-force loop (single --position, 2048 candidates)",
	RunE:  runE,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runFlags.ApplyEnv()

	f := runCmd.Flags()
	f.StringVar(&runFlags.DBPath, "db", runFlags.DBPath, "SQLite database path")
	f.StringVar(&runFlags.Template, "template", runFlags.Template, "12-word BIP-39 template (empty = generate a random demo seed)")
	f.IntVar(&runFlags.Position, "position", runFlags.Position, "word position to mutate (0-11)")
	f.IntVar(&runFlags.NAddresses, "addresses", runFlags.NAddresses, "number of receiving addresses to derive per candidate")
	f.StringVar(&runFlags.API, "api", runFlags.API, "balance API: mempool|blockstream")
	f.StringVar(&runFlags.ScriptType, "script-type", runFlags.ScriptType, "address type: segwit|legacy")
	f.Float64Var(&runFlags.Rate, "rate", runFlags.Rate, "API requests per second")
	f.IntVar(&runFlags.DeriveWorkers, "derive-workers", runFlags.DeriveWorkers, "number of derivation workers (0 = NumCPU)")
	f.IntVar(&runFlags.APIWorkers, "api-workers", runFlags.APIWorkers, "number of API workers")
	f.IntVar(&runFlags.BatchSize, "batch-size", runFlags.BatchSize, "SQLite insert batch size")
	f.BoolVar(&runFlags.Fresh, "fresh", false, "ignore any paused session for this signature")
	f.BoolVar(&runFlags.NoDashboard, "no-dashboard", false, "disable the live dashboard (useful for non-TTY use)")
}

func runE(cmd *cobra.Command, _ []string) error {
	cfg := runFlags
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	template, err := resolveTemplate(cfg.Template, cmd.OutOrStdout())
	if err != nil {
		return err
	}

	deriveWorkers := cfg.DeriveWorkers
	if deriveWorkers <= 0 {
		deriveWorkers = runtime.NumCPU()
	}

	repo, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = repo.Close() }()

	baseChecker, err := checker.New(checker.Provider(cfg.API), &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		return fmt.Errorf("build checker: %w", err)
	}
	rateLimited := checker.WithRateLimit(baseChecker, cfg.Rate)

	pipelineCfg := pipeline.Config{
		Template:   template,
		Position:   cfg.Position,
		ScriptType: derivation.ScriptType(cfg.ScriptType),
		NAddresses: cfg.NAddresses,
		API:        cfg.API,
		BatchSize:  cfg.BatchSize,
		Fresh:      cfg.Fresh,
	}
	deps := pipeline.Dependencies{
		Repository: repo,
		Deriver:    derivation.New(),
		Checker:    rateLimited,
	}

	stats := pipeline.NewStats()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("starting run",
		"position", cfg.Position,
		"api", cfg.API,
		"script_type", cfg.ScriptType,
		"addresses", cfg.NAddresses,
		"rate", cfg.Rate,
		"derive_workers", deriveWorkers,
		"api_workers", cfg.APIWorkers,
		"batch_size", cfg.BatchSize,
		"fresh", cfg.Fresh,
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
	return nil
}

// resolveTemplate returns the 12-word template the run will iterate over. If
// the user supplied an empty template, a random demo mnemonic is generated
// and printed once with a clear "do not fund this" notice.
func resolveTemplate(raw string, out io.Writer) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		words := strings.Fields(raw)
		if len(words) != 12 {
			return nil, fmt.Errorf("template must be 12 words, got %d", len(words))
		}
		return words, nil
	}

	entropy, err := extbip39.NewEntropy(128)
	if err != nil {
		return nil, fmt.Errorf("generate entropy: %w", err)
	}
	mnemonic, err := extbip39.NewMnemonic(entropy)
	if err != nil {
		return nil, fmt.Errorf("generate mnemonic: %w", err)
	}

	fmt.Fprintln(out, "──── demo seed (DO NOT FUND) ───────────────────────────────")
	fmt.Fprintln(out, mnemonic)
	fmt.Fprintln(out, "This mnemonic was generated locally for educational use only.")
	fmt.Fprintln(out, "────────────────────────────────────────────────────────────")
	return strings.Fields(mnemonic), nil
}

func hashTemplate(template []string) string {
	// Local re-hash for the dashboard meta. The pipeline computes the same
	// hash internally for the session signature.
	return pipelineTemplateHash(strings.Join(template, " "))
}
