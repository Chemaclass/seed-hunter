package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Chemaclass/seed-hunter/config"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

var statsFlags = struct {
	DBPath    string
	SessionID int64
}{
	DBPath: config.DefaultDBPath,
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show progress summary from the database",
	RunE:  statsE,
}

func init() {
	rootCmd.AddCommand(statsCmd)
	f := statsCmd.Flags()
	f.StringVar(&statsFlags.DBPath, "db", statsFlags.DBPath, "SQLite database path")
	f.Int64Var(&statsFlags.SessionID, "session", 0, "show stats for a single session id (0 = aggregate)")
}

func statsE(cmd *cobra.Command, _ []string) error {
	repo, err := storage.Open(statsFlags.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = repo.Close() }()

	ctx := context.Background()
	var s storage.Stats
	if statsFlags.SessionID > 0 {
		s, err = repo.Stats(ctx, statsFlags.SessionID)
	} else {
		s, err = repo.AggregateStats(ctx)
	}
	if err != nil {
		return fmt.Errorf("read stats: %w", err)
	}

	out := cmd.OutOrStdout()
	if statsFlags.SessionID > 0 {
		fmt.Fprintf(out, "Session #%d\n", statsFlags.SessionID)
	} else {
		fmt.Fprintln(out, "Aggregate (all sessions)")
	}
	fmt.Fprintf(out, "  Total attempts:  %d\n", s.Total)
	fmt.Fprintf(out, "  Valid mnemonics: %d\n", s.ValidMnemonics)
	fmt.Fprintf(out, "  Hits (funded):   %d\n", s.Hits)
	fmt.Fprintf(out, "  Errors:          %d\n", s.Errors)
	return nil
}

// pipelineTemplateHash mirrors internal/pipeline.hashTemplate so cmd/ doesn't
// need to import an unexported helper. Both must stay in sync.
func pipelineTemplateHash(joined string) string {
	h := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(h[:])
}
