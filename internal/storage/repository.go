// Package storage persists seed-hunter run metadata in SQLite.
//
// The repository never stores plaintext mnemonics — only SHA-256 fingerprints
// (see internal/bip39.Fingerprint). It also tracks "sessions" so that an
// interrupted run can be resumed from the last checkpointed word index.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver
)

//go:embed schema.sql
var schemaSQL string

// Status values for the sessions table.
const (
	StatusRunning   = "running"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
)

// SessionSignature uniquely identifies a logical run. Two runs with the same
// signature are considered the same session for resume purposes.
type SessionSignature struct {
	TemplateHash string
	Position     int
	API          string
	AddressType  string
	NAddresses   int
}

// Attempt is a single candidate-mnemonic check that the pipeline persists.
type Attempt struct {
	SessionID     int64
	WordIndex     int
	MnemonicHash  string
	AddressesJSON string
	BalanceSats   int64
	ValidChecksum bool
	Error         string
	DurationMS    int64
	CheckedAtUnix int64
}

// Stats summarises the work recorded for a single session.
type Stats struct {
	Total          int64
	ValidMnemonics int64
	Hits           int64
	Errors         int64
}

// Repository is the SQLite-backed store. It is safe for concurrent use.
type Repository struct {
	db *sql.DB
	mu sync.Mutex // serialises writes to avoid SQLITE_BUSY under heavy load
}

// Open opens (or creates) the SQLite database at path and applies the
// embedded schema. The returned Repository must be closed by the caller.
func Open(path string) (*Repository, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // single writer keeps modernc/sqlite happy under contention
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Repository{db: db}, nil
}

// Close releases the underlying database handle.
func (r *Repository) Close() error {
	return r.db.Close()
}

// Resume returns the last_word_index for an existing running or paused
// session matching sig, or -1 if no such session exists.
func (r *Repository) Resume(ctx context.Context, sig SessionSignature) (int, error) {
	const q = `
SELECT last_word_index
  FROM sessions
 WHERE template_hash = ?
   AND position      = ?
   AND api           = ?
   AND address_type  = ?
   AND n_addresses   = ?
   AND status IN (?, ?)
 ORDER BY id DESC
 LIMIT 1`
	var idx int
	err := r.db.QueryRowContext(ctx, q,
		sig.TemplateHash, sig.Position, sig.API, sig.AddressType, sig.NAddresses,
		StatusRunning, StatusPaused,
	).Scan(&idx)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("query resume: %w", err)
	}
	return idx, nil
}

// BeginSession reuses an existing running/paused session matching sig (so
// the caller can resume it) or creates a new one. The returned session ID
// is the row to write checkpoints and attempts against.
func (r *Repository) BeginSession(ctx context.Context, sig SessionSignature) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reuse the most recent paused/running session for this signature.
	const reuseQ = `
SELECT id FROM sessions
 WHERE template_hash = ? AND position = ? AND api = ?
   AND address_type  = ? AND n_addresses = ?
   AND status IN (?, ?)
 ORDER BY id DESC LIMIT 1`
	var id int64
	err := r.db.QueryRowContext(ctx, reuseQ,
		sig.TemplateHash, sig.Position, sig.API, sig.AddressType, sig.NAddresses,
		StatusRunning, StatusPaused,
	).Scan(&id)
	if err == nil {
		// Mark it back to running.
		if _, err := r.db.ExecContext(ctx,
			`UPDATE sessions SET status = ?, ended_at_unix = NULL WHERE id = ?`,
			StatusRunning, id,
		); err != nil {
			return 0, fmt.Errorf("resume session: %w", err)
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("lookup session: %w", err)
	}

	const insertQ = `
INSERT INTO sessions
  (started_at_unix, template_hash, position, api, address_type, n_addresses, status)
VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, insertQ,
		time.Now().Unix(), sig.TemplateHash, sig.Position, sig.API,
		sig.AddressType, sig.NAddresses, StatusRunning,
	)
	if err != nil {
		return 0, fmt.Errorf("insert session: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("session id: %w", err)
	}
	return id, nil
}

// Checkpoint records that all word indices ≤ wordIndex have been processed
// for the given session. It is safe to call repeatedly during a run.
func (r *Repository) Checkpoint(ctx context.Context, sessionID int64, wordIndex int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET last_word_index = ? WHERE id = ? AND last_word_index < ?`,
		wordIndex, sessionID, wordIndex,
	)
	if err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	return nil
}

// EndSession marks the session as paused or completed and stamps ended_at_unix.
func (r *Repository) EndSession(ctx context.Context, sessionID int64, status string) error {
	if status != StatusPaused && status != StatusCompleted {
		return fmt.Errorf("invalid end status %q", status)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET status = ?, ended_at_unix = ? WHERE id = ?`,
		status, time.Now().Unix(), sessionID,
	)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

// InsertAttempts persists a batch of attempts in a single transaction.
func (r *Repository) InsertAttempts(ctx context.Context, batch []Attempt) error {
	if len(batch) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const q = `
INSERT INTO attempts
  (session_id, word_index, mnemonic_hash, addresses_json, balance_sats, valid_checksum, error, duration_ms, checked_at_unix)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i := range batch {
		a := &batch[i]
		var errStr any
		if a.Error != "" {
			errStr = a.Error
		}
		valid := 0
		if a.ValidChecksum {
			valid = 1
		}
		if _, err := stmt.ExecContext(ctx,
			a.SessionID, a.WordIndex, a.MnemonicHash, a.AddressesJSON,
			a.BalanceSats, valid, errStr, a.DurationMS, a.CheckedAtUnix,
		); err != nil {
			return fmt.Errorf("insert attempt: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Stats summarises a single session.
func (r *Repository) Stats(ctx context.Context, sessionID int64) (Stats, error) {
	const q = `
SELECT
  COUNT(*),
  COALESCE(SUM(valid_checksum), 0),
  COALESCE(SUM(CASE WHEN balance_sats > 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN error IS NOT NULL AND error <> '' THEN 1 ELSE 0 END), 0)
FROM attempts
WHERE session_id = ?`
	var s Stats
	err := r.db.QueryRowContext(ctx, q, sessionID).
		Scan(&s.Total, &s.ValidMnemonics, &s.Hits, &s.Errors)
	if err != nil {
		return Stats{}, fmt.Errorf("stats: %w", err)
	}
	return s, nil
}

// AggregateStats summarises every session in the database.
func (r *Repository) AggregateStats(ctx context.Context) (Stats, error) {
	const q = `
SELECT
  COUNT(*),
  COALESCE(SUM(valid_checksum), 0),
  COALESCE(SUM(CASE WHEN balance_sats > 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN error IS NOT NULL AND error <> '' THEN 1 ELSE 0 END), 0)
FROM attempts`
	var s Stats
	err := r.db.QueryRowContext(ctx, q).
		Scan(&s.Total, &s.ValidMnemonics, &s.Hits, &s.Errors)
	if err != nil {
		return Stats{}, fmt.Errorf("aggregate stats: %w", err)
	}
	return s, nil
}

// Reset truncates both attempts and sessions tables.
func (r *Repository) Reset(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM attempts`); err != nil {
		return fmt.Errorf("delete attempts: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}
