package pipeline_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/pipeline"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// validTemplate is the official BIP-39 test-vector mnemonic. Every
// substitution at any position produces a syntactically valid mnemonic
// candidate, though most fail the checksum — which is exactly what we want
// the pipeline to handle gracefully.
var validTemplate = strings.Fields(
	"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
)

// fakeDeriver returns a fixed pretend address. It honours
// derivation.ErrInvalidMnemonic exactly the way the real Deriver would, by
// validating the BIP-39 checksum via... actually, for our test purposes we
// pretend ALL mnemonics are valid so the pipeline always exercises the
// checker path. The unit-level invalid-checksum behaviour is already covered
// by internal/derivation tests.
type fakeDeriver struct{}

func (fakeDeriver) Derive(_ string, n int, _ derivation.ScriptType) ([]string, error) {
	out := make([]string, n)
	for i := range out {
		out[i] = "bc1qfakeaddress"
	}
	return out, nil
}

// countingChecker records how many times CheckAddresses was called and
// returns 0 satoshis. It optionally cancels a context once a target call
// count is reached, which is how we simulate a Ctrl+C mid-run.
type countingChecker struct {
	calls       atomic.Int64
	cancel      context.CancelFunc
	cancelAfter int64
}

func (c *countingChecker) CheckAddresses(_ context.Context, _ []string) (int64, error) {
	n := c.calls.Add(1)
	if c.cancel != nil && c.cancelAfter > 0 && n == c.cancelAfter {
		c.cancel()
	}
	return 0, nil
}

func newRepo(t *testing.T) *storage.Repository {
	t.Helper()
	repo, err := storage.Open(filepath.Join(t.TempDir(), "pipeline.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func newCfg() pipeline.Config {
	return pipeline.Config{
		Template:   validTemplate,
		Position:   3,
		ScriptType: derivation.ScriptSegwit,
		NAddresses: 1,
		API:        "fake",
		BatchSize:  10,
	}
}

func TestRunFromScratchProcessesEntireKeyspaceAndCompletesSession(t *testing.T) {
	repo := newRepo(t)
	cfg := newCfg()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	checker := &countingChecker{}
	stats := pipeline.NewStats()

	res, err := pipeline.Run(ctx, cfg, pipeline.Dependencies{
		Repository: repo,
		Deriver:    fakeDeriver{},
		Checker:    checker,
	}, stats)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.FinalStatus != storage.StatusCompleted {
		t.Errorf("FinalStatus: want completed, got %s", res.FinalStatus)
	}
	if res.EndIndex != 2047 {
		t.Errorf("EndIndex: want 2047, got %d", res.EndIndex)
	}
	if res.WasResumed {
		t.Error("WasResumed: want false on a fresh run")
	}

	dbStats, err := repo.Stats(ctx, res.SessionID)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if dbStats.Total != 2048 {
		t.Errorf("DB total: want 2048, got %d", dbStats.Total)
	}
	if checker.calls.Load() != 2048 {
		// All 2048 candidates pass the fake deriver's "always valid" path,
		// so the checker MUST be called 2048 times.
		t.Errorf("checker calls: want 2048, got %d", checker.calls.Load())
	}
	if got := stats.Processed.Load(); got != 2048 {
		t.Errorf("stats.Processed: want 2048, got %d", got)
	}
}

func TestRunCancelMidRunMarksSessionPausedWithCheckpoint(t *testing.T) {
	repo := newRepo(t)
	cfg := newCfg()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	checker := &countingChecker{cancel: cancel, cancelAfter: 100}

	res, err := pipeline.Run(ctx, cfg, pipeline.Dependencies{
		Repository: repo,
		Deriver:    fakeDeriver{},
		Checker:    checker,
	}, pipeline.NewStats())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.FinalStatus != storage.StatusPaused {
		t.Errorf("FinalStatus: want paused, got %s", res.FinalStatus)
	}
	if !res.WasCancelled {
		t.Error("WasCancelled: want true after Ctrl+C")
	}
	// EndIndex should be at least 99 (the 100th call, 0-indexed) and at most
	// 2046 — there's some slack because in-flight items between the checker
	// and the logger may or may not have been committed before cancellation
	// propagates.
	if res.EndIndex < 99 {
		t.Errorf("EndIndex: want >= 99 (last committed), got %d", res.EndIndex)
	}
	if res.EndIndex >= 2047 {
		t.Errorf("EndIndex: want < 2047 on a cancelled run, got %d", res.EndIndex)
	}

	// The repository must reflect the same checkpoint via Resume.
	bgCtx := context.Background()
	resumeIdx, err := repo.Resume(bgCtx, storage.SessionSignature{
		TemplateHash: hashOfTemplate(t, cfg.Template),
		Position:     cfg.Position,
		API:          cfg.API,
		AddressType:  string(cfg.ScriptType),
		NAddresses:   cfg.NAddresses,
	})
	if err != nil {
		t.Fatalf("Resume after cancel: %v", err)
	}
	if resumeIdx != res.EndIndex {
		t.Errorf("Resume index disagrees with EndIndex: resume=%d end=%d", resumeIdx, res.EndIndex)
	}
}

func TestRunResumesPausedSessionAndFinishesRemainder(t *testing.T) {
	repo := newRepo(t)
	cfg := newCfg()

	// First run: cancel after 100 calls.
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel1()
	firstChecker := &countingChecker{cancel: cancel1, cancelAfter: 100}
	res1, err := pipeline.Run(ctx1, cfg, pipeline.Dependencies{
		Repository: repo,
		Deriver:    fakeDeriver{},
		Checker:    firstChecker,
	}, pipeline.NewStats())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if res1.FinalStatus != storage.StatusPaused {
		t.Fatalf("first run not paused: %s", res1.FinalStatus)
	}

	// Second run: same signature, fresh context. Should resume.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	secondChecker := &countingChecker{}
	res2, err := pipeline.Run(ctx2, cfg, pipeline.Dependencies{
		Repository: repo,
		Deriver:    fakeDeriver{},
		Checker:    secondChecker,
	}, pipeline.NewStats())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if !res2.WasResumed {
		t.Error("WasResumed: want true on the resumed run")
	}
	if res2.FinalStatus != storage.StatusCompleted {
		t.Errorf("FinalStatus: want completed, got %s", res2.FinalStatus)
	}
	if res2.SessionID != res1.SessionID {
		t.Errorf("session id changed across resume: %d -> %d", res1.SessionID, res2.SessionID)
	}
	if res2.EndIndex != 2047 {
		t.Errorf("resumed EndIndex: want 2047, got %d", res2.EndIndex)
	}

	// Total attempts in the DB should be exactly 2048 (no duplicates).
	dbStats, err := repo.Stats(context.Background(), res2.SessionID)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if dbStats.Total != 2048 {
		t.Errorf("DB total after resume: want 2048, got %d", dbStats.Total)
	}

	// The second checker only handled the remainder, so its call count must
	// be strictly less than 2048 and strictly greater than zero.
	calls := secondChecker.calls.Load()
	if calls <= 0 || calls >= 2048 {
		t.Errorf("second checker calls out of range: %d (want 1..2047)", calls)
	}
	// The two checkers together should account for at least 2048 calls (some
	// may be re-derived in flight on the cancelled run).
	if firstChecker.calls.Load()+calls < 2048 {
		t.Errorf("combined calls < 2048: first=%d second=%d", firstChecker.calls.Load(), calls)
	}
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	repo := newRepo(t)
	good := newCfg()

	cases := map[string]func(*pipeline.Config){
		"empty template":   func(c *pipeline.Config) { c.Template = nil },
		"short template":   func(c *pipeline.Config) { c.Template = c.Template[:11] },
		"bad position low": func(c *pipeline.Config) { c.Position = -1 },
		"bad position hi":  func(c *pipeline.Config) { c.Position = 12 },
		"bad n addresses":  func(c *pipeline.Config) { c.NAddresses = 0 },
		"bad script":       func(c *pipeline.Config) { c.ScriptType = "wat" },
		"empty api":        func(c *pipeline.Config) { c.API = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := good
			cfg.Template = append([]string(nil), good.Template...)
			mutate(&cfg)
			_, err := pipeline.Run(context.Background(), cfg, pipeline.Dependencies{
				Repository: repo,
				Deriver:    fakeDeriver{},
				Checker:    &countingChecker{},
			}, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestRunRequiresAllDependencies(t *testing.T) {
	cfg := newCfg()
	repo := newRepo(t)
	missing := []pipeline.Dependencies{
		{Deriver: fakeDeriver{}, Checker: &countingChecker{}}, // no repo
		{Repository: repo, Checker: &countingChecker{}},       // no deriver
		{Repository: repo, Deriver: fakeDeriver{}},            // no checker
	}
	for i, deps := range missing {
		_, err := pipeline.Run(context.Background(), cfg, deps, nil)
		if err == nil {
			t.Errorf("case %d: expected error for missing dependency", i)
		}
		if err != nil && !errors.Is(err, errors.New("")) && !strings.Contains(err.Error(), "dependencies") {
			// Just sanity-check the message mentions dependencies; we don't
			// pin the exact wording.
			t.Logf("case %d error: %v", i, err)
		}
	}
}

// hashOfTemplate mirrors pipeline.hashTemplate so the test can build a
// matching SessionSignature.
func hashOfTemplate(t *testing.T, template []string) string {
	t.Helper()
	// We can't import pipeline.hashTemplate (unexported); recompute via
	// crypto/sha256 of the joined template — same algorithm.
	return sha256Hex(strings.Join(template, " "))
}
