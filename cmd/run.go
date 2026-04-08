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
	Short: "Start the brute-force loop (single --position, 2048 candidates)",
	RunE:  runE,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runFlags.ApplyEnv()

	f := runCmd.Flags()
	f.StringVar(&runFlags.DBPath, "db", runFlags.DBPath, "SQLite database path")
	f.StringVar(&runFlags.WordlistPath, "wordlist", runFlags.WordlistPath, "path to a 2048-word BIP-39 wordlist file (empty = embedded English)")
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

	// Load the wordlist FIRST: it influences both the iterator (which words
	// it yields) and the underlying tyler-smith/go-bip39 library (which uses
	// a process-global wordlist for checksum validation and for generating
	// the random demo mnemonic). Binding both to the same source guarantees
	// the iterator and the deriver always agree on the words.
	words, err := wordlist.Load(cfg.WordlistPath)
	if err != nil {
		return fmt.Errorf("load wordlist: %w", err)
	}
	extbip39.SetWordList(words)
	iterator, err := bip39.NewIterator(words)
	if err != nil {
		return fmt.Errorf("build iterator: %w", err)
	}

	template, generated, err := resolveTemplate(cfg.Template, sidecarPath(cfg.DBPath), cmd.OutOrStdout(), generateRandomMnemonic)
	if err != nil {
		return err
	}
	_ = generated // captured for the resume hint below

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
		Iterator:   iterator,
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
	if res.FinalStatus == storage.StatusPaused {
		printResumeHint(cmd.OutOrStdout(), template, cfg)
	}
	return nil
}

// printResumeHint emits a copy-paste-ready command that re-runs the same
// session signature. It is shown only when the run paused, so the user
// always knows exactly how to continue. The template is included in full
// so resume works even if the sidecar file is deleted.
func printResumeHint(out io.Writer, template []string, cfg config.Config) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "To resume this session manually, run:")
	fmt.Fprintf(out,
		"  seed-hunter run --template %q --position %d --addresses %d --api %s --script-type %s --rate %g\n",
		strings.Join(template, " "),
		cfg.Position,
		cfg.NAddresses,
		cfg.API,
		cfg.ScriptType,
		cfg.Rate,
	)
}

// resolveTemplate returns the 12-word template the run will iterate over.
//
// Resolution order, in priority:
//  1. raw (the --template flag) — if non-empty, use it verbatim.
//  2. sidecarPath — if a sidecar file exists at that path with a 12-word
//     mnemonic, use it. This is what makes "Ctrl+C, then run again" resume
//     the SAME random demo seed instead of generating a new one.
//  3. otherwise call generate() to produce a fresh random mnemonic, write
//     it to sidecarPath for next time, and print the "do not fund" notice.
//
// The bool return is `generated` — true only when this call produced a
// brand-new random mnemonic. It's currently informational; callers can use
// it to decide whether to print extra context.
func resolveTemplate(raw, sidecarPath string, out io.Writer, generate func() (string, error)) (template []string, generated bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		words := strings.Fields(raw)
		if len(words) != 12 {
			return nil, false, fmt.Errorf("template must be 12 words, got %d", len(words))
		}
		return words, false, nil
	}

	if sidecarPath != "" {
		if data, readErr := os.ReadFile(sidecarPath); readErr == nil {
			words := strings.Fields(strings.TrimSpace(string(data)))
			if len(words) == 12 {
				fmt.Fprintf(out, "Reusing saved demo seed from %s (delete the file or pass --template to override).\n",
					sidecarPath)
				return words, false, nil
			}
			// File exists but is malformed — fall through and regenerate.
			fmt.Fprintf(out, "Sidecar %s exists but is not a 12-word mnemonic; regenerating.\n",
				sidecarPath)
		}
	}

	mnemonic, err := generate()
	if err != nil {
		return nil, false, err
	}

	fmt.Fprintln(out, "──── demo seed (DO NOT FUND) ───────────────────────────────")
	fmt.Fprintln(out, mnemonic)
	fmt.Fprintln(out, "This mnemonic was generated locally for educational use only.")
	fmt.Fprintln(out, "────────────────────────────────────────────────────────────")

	if sidecarPath != "" {
		if writeErr := os.WriteFile(sidecarPath, []byte(mnemonic+"\n"), 0o600); writeErr != nil {
			fmt.Fprintf(out, "Warning: could not save demo seed to %s: %v\n", sidecarPath, writeErr)
		} else {
			fmt.Fprintf(out, "Saved to %s — re-run without --template to resume this session.\n",
				sidecarPath)
		}
	}

	return strings.Fields(mnemonic), true, nil
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

// sidecarPath returns the path of the auto-generated-template sidecar file
// for a given SQLite db path. The naming is `<db>.template`, e.g.
// `seed-hunter.db.template`. Empty input returns empty.
func sidecarPath(dbPath string) string {
	if dbPath == "" {
		return ""
	}
	return dbPath + ".template"
}

func hashTemplate(template []string) string {
	// Local re-hash for the dashboard meta. The pipeline computes the same
	// hash internally for the session signature.
	return pipelineTemplateHash(strings.Join(template, " "))
}
