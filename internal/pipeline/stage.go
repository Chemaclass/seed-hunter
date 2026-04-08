// Package pipeline wires the BIP-39 iterator, the address deriver, the
// balance checker, and the SQLite repository together as a stop-and-resume
// channel pipeline.
package pipeline

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/derivation"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

// Candidate is the unit emitted by the generator stage. WordIndex is the
// position in the BIP-39 wordlist of the substituted word (0..2047).
type Candidate struct {
	WordIndex int
	Mnemonic  string
}

// Derived is the unit emitted by the deriver stage. ValidChecksum is false
// for mnemonics that fail the BIP-39 checksum (those still pass through the
// pipeline so we can log them, but no balance lookup is attempted).
type Derived struct {
	Candidate
	Addresses     []string
	ValidChecksum bool
}

// Checked is the unit emitted by the checker stage. BalanceSats is zero when
// the checksum was invalid (the balance lookup is skipped) or when the
// checker reported no funds.
type Checked struct {
	Derived
	BalanceSats int64
	CheckErr    error
	DurationMS  int64
	CheckedAt   time.Time
}

// Stats is the live counter snapshot the dashboard reads while a run is in
// flight. All mutable fields are atomic so the dashboard goroutine can read
// them without coordinating with the pipeline.
type Stats struct {
	SessionID atomic.Int64
	ResumedAt atomic.Int64 // word index the current run picked up at; -1 if fresh
	StartedAt time.Time    // set once before the run starts; immutable after

	Processed      atomic.Int64 // candidates processed in THIS run
	ValidMnemonics atomic.Int64
	Hits           atomic.Int64
	Errors         atomic.Int64
}

// NewStats returns a fresh Stats with StartedAt=now and ResumedAt=-1.
func NewStats() *Stats {
	s := &Stats{StartedAt: time.Now()}
	s.ResumedAt.Store(-1)
	return s
}

// Config drives Run. Template/Position/ScriptType/NAddresses/API contribute
// to the session signature used for resume; Rate, WordlistPath, Workers and
// PositionsSpec are persisted as session metadata so a later
// "seed-hunter run" with no flags can recover them.
type Config struct {
	Template      []string
	Position      int
	ScriptType    derivation.ScriptType
	NAddresses    int
	API           string
	Rate          float64 // persisted to sessions for resume convenience
	WordlistPath  string  // persisted to sessions for resume convenience
	Workers       int     // number of parallel deriver goroutines (>= 1)
	PositionsSpec string  // raw --positions value the cmd layer sweeps
	BatchSize     int     // sqlite insert batch size; defaults to 50 if <= 0
	Fresh         bool    // ignore any paused session for this signature
}

// Dependencies bundles the collaborators Run needs. Tests inject fakes here.
type Dependencies struct {
	Repository *storage.Repository
	Iterator   Iterator
	Deriver    Deriver
	Checker    Checker
}

// Iterator is the subset of *bip39.Iterator Run uses. It is defined as an
// interface so tests can inject a tiny stub instead of constructing a real
// 2048-word iterator.
type Iterator interface {
	CandidateAt(template []string, pos, i int) (string, error)
}

// Deriver is the subset of derivation.Deriver Run uses. Defining it here lets
// tests inject a fake without pulling btcsuite into the test path.
type Deriver interface {
	Derive(mnemonic string, n int, scriptType derivation.ScriptType) ([]string, error)
}

// Checker is the subset of checker.BalanceChecker Run uses.
type Checker interface {
	CheckAddresses(ctx context.Context, addresses []string) (int64, error)
}
