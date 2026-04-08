package pipeline_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/pipeline"
	"github.com/Chemaclass/seed-hunter/internal/storage"
	"github.com/Chemaclass/seed-hunter/internal/wordlist"
)

// alwaysValidDeriver pretends every mnemonic is BIP-39 valid and returns a
// fixed pretend address. The walker test uses it because the real deriver
// would only validate ~1/16 of cursors and we want every iteration to
// exercise the full code path (derive → check → log).
type alwaysValidDeriver struct{}

func (alwaysValidDeriver) Derive(_ string, n int, _ derivation.ScriptType) ([]string, error) {
	out := make([]string, n)
	for i := range out {
		out[i] = "bc1qfakeaddress"
	}
	return out, nil
}

// stopAfterChecker cancels the parent context once the configured number
// of CheckAddresses calls have been made. Used to bound the walker test
// to a small known number of iterations instead of running forever.
type stopAfterChecker struct {
	calls atomic.Int64
	limit int64
	stop  context.CancelFunc
}

func (s *stopAfterChecker) CheckAddresses(_ context.Context, _ []string) (int64, error) {
	n := s.calls.Add(1)
	if s.limit > 0 && n >= s.limit {
		s.stop()
	}
	return 0, nil
}

func newWalkRepo(t *testing.T) *storage.Repository {
	t.Helper()
	repo, err := storage.Open(filepath.Join(t.TempDir(), "walker.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func newWalkCfg() pipeline.WalkConfig {
	return pipeline.WalkConfig{
		Words:        wordlist.Default(),
		NAddresses:   1,
		ScriptType:   derivation.ScriptSegwit,
		API:          "fake",
		Rate:         1,
		WordlistPath: "",
		BatchSize:    10,
	}
}

func TestWalkProcessesUntilContextIsCancelled(t *testing.T) {
	repo := newWalkRepo(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	checker := &stopAfterChecker{limit: 50, stop: cancel}
	stats := pipeline.NewStats()

	res, err := pipeline.Walk(ctx, newWalkCfg(), pipeline.WalkDependencies{
		Repository: repo,
		Deriver:    alwaysValidDeriver{},
		Checker:    checker,
	}, stats)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if res.FinalStatus != storage.StatusPaused {
		t.Errorf("FinalStatus: want paused (cancel ends a walk), got %s", res.FinalStatus)
	}
	if !res.WasCancelled {
		t.Error("WasCancelled: want true after ctx cancel")
	}
	if checker.calls.Load() < 50 {
		t.Errorf("expected at least 50 checker calls before cancel, got %d", checker.calls.Load())
	}
	if got := stats.Processed.Load(); got < 50 {
		t.Errorf("stats.Processed: want at least 50, got %d", got)
	}

	// The persisted cursor should be a valid 12-int comma list and not
	// the empty string (we DID process candidates).
	got, err := repo.LatestResumable(context.Background())
	if err != nil {
		t.Fatalf("LatestResumable: %v", err)
	}
	if got == nil {
		t.Fatal("expected a paused walk session")
	}
	if got.Mode != storage.ModeWalk {
		t.Errorf("Mode: want walk, got %s", got.Mode)
	}
	if got.Cursor == "" {
		t.Error("Cursor: persisted walk session should have a non-empty cursor")
	}
	if _, err := pipeline.ParseCursor(got.Cursor); err != nil {
		t.Errorf("persisted cursor must parse: %v", err)
	}
}

func TestWalkResumesAtPersistedCursor(t *testing.T) {
	repo := newWalkRepo(t)

	// First walk: process about 30 then cancel.
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel1()
	first := &stopAfterChecker{limit: 30, stop: cancel1}
	res1, err := pipeline.Walk(ctx1, newWalkCfg(), pipeline.WalkDependencies{
		Repository: repo,
		Deriver:    alwaysValidDeriver{},
		Checker:    first,
	}, pipeline.NewStats())
	if err != nil {
		t.Fatalf("first Walk: %v", err)
	}
	if res1.FinalStatus != storage.StatusPaused {
		t.Fatalf("first walk should be paused, got %s", res1.FinalStatus)
	}

	persistedCursor := res1.EndCursor

	// Second walk: same signature, fresh context. Should resume.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	second := &stopAfterChecker{limit: 30, stop: cancel2}
	res2, err := pipeline.Walk(ctx2, newWalkCfg(), pipeline.WalkDependencies{
		Repository: repo,
		Deriver:    alwaysValidDeriver{},
		Checker:    second,
	}, pipeline.NewStats())
	if err != nil {
		t.Fatalf("second Walk: %v", err)
	}
	if !res2.WasResumed {
		t.Error("WasResumed: want true on the resumed walk")
	}
	if res2.SessionID != res1.SessionID {
		t.Errorf("session id changed across resume: %d -> %d", res1.SessionID, res2.SessionID)
	}

	// The second walk should start strictly AFTER the first walk's
	// persisted cursor (resume = persisted cursor + 1).
	gotStart := res2.StartCursor
	if gotStart == persistedCursor {
		t.Errorf("second walk should start AFTER persisted cursor, got %v == %v", gotStart, persistedCursor)
	}
}

func TestWalkFreshFlagDropsExistingPausedSession(t *testing.T) {
	repo := newWalkRepo(t)

	// Run a walk and pause it.
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel1()
	first := &stopAfterChecker{limit: 30, stop: cancel1}
	if _, err := pipeline.Walk(ctx1, newWalkCfg(), pipeline.WalkDependencies{
		Repository: repo,
		Deriver:    alwaysValidDeriver{},
		Checker:    first,
	}, pipeline.NewStats()); err != nil {
		t.Fatalf("first Walk: %v", err)
	}

	// Now run with Fresh=true; the previous session should be retired
	// and a new one started at the zero cursor.
	cfg := newWalkCfg()
	cfg.Fresh = true
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	second := &stopAfterChecker{limit: 5, stop: cancel2}
	res2, err := pipeline.Walk(ctx2, cfg, pipeline.WalkDependencies{
		Repository: repo,
		Deriver:    alwaysValidDeriver{},
		Checker:    second,
	}, pipeline.NewStats())
	if err != nil {
		t.Fatalf("second Walk: %v", err)
	}
	if res2.WasResumed {
		t.Error("WasResumed: want false when --fresh was set")
	}
	if res2.StartCursor != (pipeline.Cursor{}) {
		t.Errorf("StartCursor: want zero cursor with --fresh, got %v", res2.StartCursor)
	}
}

func TestWalkRejectsBadConfig(t *testing.T) {
	repo := newWalkRepo(t)
	good := newWalkCfg()
	cases := map[string]func(*pipeline.WalkConfig){
		"empty wordlist":  func(c *pipeline.WalkConfig) { c.Words = nil },
		"short wordlist":  func(c *pipeline.WalkConfig) { c.Words = good.Words[:100] },
		"bad n addresses": func(c *pipeline.WalkConfig) { c.NAddresses = 0 },
		"bad script type": func(c *pipeline.WalkConfig) { c.ScriptType = "wat" },
		"empty api":       func(c *pipeline.WalkConfig) { c.API = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := good
			cfg.Words = append([]string(nil), good.Words...)
			mutate(&cfg)
			_, err := pipeline.Walk(context.Background(), cfg, pipeline.WalkDependencies{
				Repository: repo,
				Deriver:    alwaysValidDeriver{},
				Checker:    &stopAfterChecker{},
			}, pipeline.NewStats())
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestWalkRequiresAllDependencies(t *testing.T) {
	cfg := newWalkCfg()
	repo := newWalkRepo(t)
	missing := []pipeline.WalkDependencies{
		{Deriver: alwaysValidDeriver{}, Checker: &stopAfterChecker{}}, // no repo
		{Repository: repo, Checker: &stopAfterChecker{}},              // no deriver
		{Repository: repo, Deriver: alwaysValidDeriver{}},             // no checker
	}
	for i, deps := range missing {
		_, err := pipeline.Walk(context.Background(), cfg, deps, pipeline.NewStats())
		if err == nil {
			t.Errorf("case %d: expected error for missing dependency", i)
		}
	}
}
