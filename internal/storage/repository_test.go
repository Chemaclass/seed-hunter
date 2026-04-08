package storage_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/storage"
)

func newTestRepo(t *testing.T) *storage.Repository {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	repo, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func newSig(template string) storage.SessionSignature {
	return storage.SessionSignature{
		TemplateHash: template,
		Position:     3,
		API:          "mempool",
		AddressType:  "segwit",
		NAddresses:   3,
	}
}

// initFor wraps a signature in a minimal SessionInit so legacy tests that
// only care about the signature (not the persisted template/rate) stay
// terse. New tests that exercise the persistence build their own SessionInit.
func initFor(sig storage.SessionSignature) storage.SessionInit {
	return storage.SessionInit{SessionSignature: sig}
}

func TestResumeReturnsMinusOneForBrandNewSignature(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	got, err := repo.Resume(ctx, newSig("template-a"))
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got != -1 {
		t.Fatalf("expected last_word_index=-1 for new signature, got %d", got)
	}
}

func TestBeginSessionAndCheckpointPersistProgress(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	sig := newSig("template-b")

	sessionID, err := repo.BeginSession(ctx, initFor(sig))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if sessionID == 0 {
		t.Fatal("expected non-zero session id")
	}

	if err := repo.Checkpoint(ctx, sessionID, 42); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	resumeIdx, err := repo.Resume(ctx, sig)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumeIdx != 42 {
		t.Fatalf("expected resume index 42, got %d", resumeIdx)
	}
}

func TestEndSessionPausedThenResumeFindsIt(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	sig := newSig("template-c")

	sessionID, err := repo.BeginSession(ctx, initFor(sig))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := repo.Checkpoint(ctx, sessionID, 100); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if err := repo.EndSession(ctx, sessionID, storage.StatusPaused); err != nil {
		t.Fatalf("EndSession paused: %v", err)
	}

	resumeIdx, err := repo.Resume(ctx, sig)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumeIdx != 100 {
		t.Fatalf("expected resume index 100 from paused session, got %d", resumeIdx)
	}
}

func TestEndSessionCompletedIsNotResumed(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	sig := newSig("template-d")

	sessionID, err := repo.BeginSession(ctx, initFor(sig))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := repo.Checkpoint(ctx, sessionID, 2047); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if err := repo.EndSession(ctx, sessionID, storage.StatusCompleted); err != nil {
		t.Fatalf("EndSession completed: %v", err)
	}

	resumeIdx, err := repo.Resume(ctx, sig)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumeIdx != -1 {
		t.Fatalf("completed session must not resume; got %d", resumeIdx)
	}
}

func TestInsertAttemptsAndStatsRoundTrip(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	sig := newSig("template-e")

	sessionID, err := repo.BeginSession(ctx, initFor(sig))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}

	attempts := []storage.Attempt{
		{SessionID: sessionID, WordIndex: 0, MnemonicHash: "h0", AddressesJSON: `["a"]`, BalanceSats: 0, ValidChecksum: true, DurationMS: 5, CheckedAtUnix: 100},
		{SessionID: sessionID, WordIndex: 1, MnemonicHash: "h1", AddressesJSON: `["b"]`, BalanceSats: 0, ValidChecksum: false, DurationMS: 6, CheckedAtUnix: 101},
		{SessionID: sessionID, WordIndex: 2, MnemonicHash: "h2", AddressesJSON: `["c"]`, BalanceSats: 1234, ValidChecksum: true, DurationMS: 7, CheckedAtUnix: 102},
	}
	if err := repo.InsertAttempts(ctx, attempts); err != nil {
		t.Fatalf("InsertAttempts: %v", err)
	}

	stats, err := repo.Stats(ctx, sessionID)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 3 {
		t.Errorf("Total: want 3, got %d", stats.Total)
	}
	if stats.ValidMnemonics != 2 {
		t.Errorf("ValidMnemonics: want 2, got %d", stats.ValidMnemonics)
	}
	if stats.Hits != 1 {
		t.Errorf("Hits: want 1, got %d", stats.Hits)
	}
}

func TestResetClearsAllSessionsAndAttempts(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	sessionID, err := repo.BeginSession(ctx, initFor(newSig("template-f")))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := repo.InsertAttempts(ctx, []storage.Attempt{
		{SessionID: sessionID, WordIndex: 0, MnemonicHash: "h", AddressesJSON: `["x"]`, ValidChecksum: true, DurationMS: 1, CheckedAtUnix: 1},
	}); err != nil {
		t.Fatalf("InsertAttempts: %v", err)
	}

	if err := repo.Reset(ctx); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	resumeIdx, err := repo.Resume(ctx, newSig("template-f"))
	if err != nil {
		t.Fatalf("Resume after reset: %v", err)
	}
	if resumeIdx != -1 {
		t.Fatalf("after reset, resume must return -1, got %d", resumeIdx)
	}
}

func TestConcurrentInsertsDoNotDeadlock(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	sessionID, err := repo.BeginSession(ctx, initFor(newSig("template-g")))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}

	const goroutines = 4
	const batchSize = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			batch := make([]storage.Attempt, batchSize)
			for i := 0; i < batchSize; i++ {
				batch[i] = storage.Attempt{
					SessionID:     sessionID,
					WordIndex:     g*batchSize + i,
					MnemonicHash:  "h",
					AddressesJSON: `["x"]`,
					ValidChecksum: true,
					DurationMS:    1,
					CheckedAtUnix: 1,
				}
			}
			if err := repo.InsertAttempts(ctx, batch); err != nil {
				errs <- err
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent insert error: %v", err)
	}

	stats, err := repo.Stats(ctx, sessionID)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != goroutines*batchSize {
		t.Errorf("Total: want %d, got %d", goroutines*batchSize, stats.Total)
	}
}

func TestLatestResumableReturnsNilWhenEmpty(t *testing.T) {
	repo := newTestRepo(t)
	got, err := repo.LatestResumable(context.Background())
	if err != nil {
		t.Fatalf("LatestResumable: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty db, got %+v", got)
	}
}

func TestLatestResumablePersistsAndReturnsFullSessionMetadata(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	init := storage.SessionInit{
		SessionSignature: storage.SessionSignature{
			TemplateHash: "deadbeef",
			Position:     5,
			API:          "blockstream",
			AddressType:  "legacy",
			NAddresses:   2,
		},
		Template:     "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
		Rate:         3.5,
		WordlistPath: "/some/spanish.txt",
	}
	id, err := repo.BeginSession(ctx, init)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := repo.Checkpoint(ctx, id, 137); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if err := repo.EndSession(ctx, id, storage.StatusPaused); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	got, err := repo.LatestResumable(ctx)
	if err != nil {
		t.Fatalf("LatestResumable: %v", err)
	}
	if got == nil {
		t.Fatal("expected a session, got nil")
	}
	if got.ID != id {
		t.Errorf("ID: want %d, got %d", id, got.ID)
	}
	if got.Template != init.Template {
		t.Errorf("Template: want %q, got %q", init.Template, got.Template)
	}
	if got.Position != 5 {
		t.Errorf("Position: want 5, got %d", got.Position)
	}
	if got.API != "blockstream" {
		t.Errorf("API: want blockstream, got %s", got.API)
	}
	if got.AddressType != "legacy" {
		t.Errorf("AddressType: want legacy, got %s", got.AddressType)
	}
	if got.NAddresses != 2 {
		t.Errorf("NAddresses: want 2, got %d", got.NAddresses)
	}
	if got.Rate != 3.5 {
		t.Errorf("Rate: want 3.5, got %g", got.Rate)
	}
	if got.WordlistPath != "/some/spanish.txt" {
		t.Errorf("WordlistPath: want /some/spanish.txt, got %s", got.WordlistPath)
	}
	if got.LastWordIndex != 137 {
		t.Errorf("LastWordIndex: want 137, got %d", got.LastWordIndex)
	}
	if got.Status != storage.StatusPaused {
		t.Errorf("Status: want paused, got %s", got.Status)
	}
}

func TestLatestResumablePicksTheMostRecentPausedSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	mkInit := func(hash string) storage.SessionInit {
		return storage.SessionInit{
			SessionSignature: storage.SessionSignature{
				TemplateHash: hash,
				Position:     0,
				API:          "mempool",
				AddressType:  "segwit",
				NAddresses:   1,
			},
			Template: hash,
		}
	}

	older, err := repo.BeginSession(ctx, mkInit("older-hash"))
	if err != nil {
		t.Fatalf("BeginSession older: %v", err)
	}
	if err := repo.EndSession(ctx, older, storage.StatusPaused); err != nil {
		t.Fatalf("EndSession older: %v", err)
	}
	// Sleep one second so started_at_unix is strictly greater for the next.
	time.Sleep(1100 * time.Millisecond)
	newer, err := repo.BeginSession(ctx, mkInit("newer-hash"))
	if err != nil {
		t.Fatalf("BeginSession newer: %v", err)
	}
	if err := repo.EndSession(ctx, newer, storage.StatusPaused); err != nil {
		t.Fatalf("EndSession newer: %v", err)
	}

	got, err := repo.LatestResumable(ctx)
	if err != nil {
		t.Fatalf("LatestResumable: %v", err)
	}
	if got == nil || got.ID != newer {
		t.Errorf("expected newer session id %d, got %+v", newer, got)
	}
}

func TestLatestResumableSkipsCompletedSessions(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	id, err := repo.BeginSession(ctx, initFor(newSig("done-hash")))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := repo.EndSession(ctx, id, storage.StatusCompleted); err != nil {
		t.Fatalf("EndSession completed: %v", err)
	}
	got, err := repo.LatestResumable(ctx)
	if err != nil {
		t.Fatalf("LatestResumable: %v", err)
	}
	if got != nil {
		t.Errorf("completed session must not be returned: %+v", got)
	}
}

func TestMarkPausedAsCompletedRetiresMatchingSessions(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	sig := newSig("retire-hash")
	id, err := repo.BeginSession(ctx, initFor(sig))
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if err := repo.EndSession(ctx, id, storage.StatusPaused); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	n, err := repo.MarkPausedAsCompleted(ctx, sig)
	if err != nil {
		t.Fatalf("MarkPausedAsCompleted: %v", err)
	}
	if n != 1 {
		t.Errorf("rows affected: want 1, got %d", n)
	}

	// Resume() must now return -1 because the session is completed.
	resumeIdx, err := repo.Resume(ctx, sig)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumeIdx != -1 {
		t.Errorf("expected -1 after MarkPausedAsCompleted, got %d", resumeIdx)
	}
}
