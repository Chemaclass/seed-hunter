package pipeline_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Chemaclass/seed-hunter/internal/bip39"
	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/pipeline"
	"github.com/Chemaclass/seed-hunter/internal/storage"
	"github.com/Chemaclass/seed-hunter/internal/wordlist"
)

// BenchmarkRunWorkers measures the wall-clock time of one full
// 2048-candidate pipeline pass at several --workers values, using the REAL
// BIP-32/44/84 deriver and a no-op checker. The point is to find the sweet
// spot for parallel deriver goroutines on the host machine: how many
// workers it takes before throughput stops scaling.
//
// Run with:
//
//	go test -bench=BenchmarkRunWorkers -benchtime=3x ./internal/pipeline
//
// Higher -benchtime means more samples per worker count.
func BenchmarkRunWorkers(b *testing.B) {
	iter, err := bip39.NewIterator(wordlist.Default())
	if err != nil {
		b.Fatal(err)
	}
	realDeriver := derivation.New()

	for _, w := range []int{1, 2, 4, 6, 8, 10, 12, 14, 16, 24} {
		b.Run(fmt.Sprintf("workers=%d", w), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				dbPath := filepath.Join(b.TempDir(), "bench.db")
				repo, err := storage.Open(dbPath)
				if err != nil {
					b.Fatal(err)
				}
				cfg := pipeline.Config{
					Template:   validTemplate,
					Position:   3,
					ScriptType: derivation.ScriptSegwit,
					NAddresses: 1,
					API:        "bench",
					BatchSize:  256,
					Workers:    w,
					Fresh:      true,
				}
				_, runErr := pipeline.Run(context.Background(), cfg, pipeline.Dependencies{
					Repository: repo,
					Iterator:   iter,
					Deriver:    realDeriver,
					Checker:    &countingChecker{},
				}, nil)
				_ = repo.Close()
				if runErr != nil {
					b.Fatalf("Run: %v", runErr)
				}
			}
		})
	}
}
