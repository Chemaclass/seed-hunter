// Package cmd implements the seed-hunter command-line interface.
//
// Each subcommand lives in its own file (run.go, stats.go, reset.go) and is
// registered with the root command from this file's init.
package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd is the top-level cobra command. Subcommands attach to it via init.
var rootCmd = &cobra.Command{
	Use:   "seed-hunter",
	Short: "Educational BIP-39 brute-force demo",
	Long: `seed-hunter — educational BIP-39 brute-force demo.

Iterates word combinations at a single position of a 12-word BIP-39 template,
derives mainnet receive addresses, and queries a public block-explorer API
for confirmed balances. Every attempt is logged to SQLite so a long run can
be stopped with Ctrl+C and resumed later from the exact same word index.

This is strictly an educational tool. The math (2048^12 combinations) makes
its impossibility viscerally obvious — see the README for the full numbers.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits with 0 on success or 1 on error.
// It is the only symbol main.go imports.
func Execute() {
	setupLogger()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func setupLogger() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
}
