package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Chemaclass/seed-hunter/config"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

var resetFlags = struct {
	DBPath string
	Yes    bool
}{
	DBPath: config.DefaultDBPath,
}

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear all attempt history (sessions and attempts)",
	RunE:  resetE,
}

func init() {
	rootCmd.AddCommand(resetCmd)
	f := resetCmd.Flags()
	f.StringVar(&resetFlags.DBPath, "db", resetFlags.DBPath, "SQLite database path")
	f.BoolVar(&resetFlags.Yes, "yes", false, "skip the confirmation prompt")
}

func resetE(cmd *cobra.Command, _ []string) error {
	if !resetFlags.Yes {
		fmt.Fprintf(cmd.OutOrStdout(), "About to wipe %s — type 'yes' to confirm: ", resetFlags.DBPath)
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read confirmation: %w", err)
		}
		if strings.TrimSpace(line) != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "aborted")
			return nil
		}
	}

	repo, err := storage.Open(resetFlags.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = repo.Close() }()

	if err := repo.Reset(context.Background()); err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	// Also remove the auto-generated demo-template sidecar so the next run
	// without --template starts from a clean slate. Missing file is fine.
	if path := sidecarPath(resetFlags.DBPath); path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(cmd.OutOrStdout(), "warning: could not remove %s: %v\n", path, err)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), "ok")
	return nil
}
