// Package dashboard renders a multi-line ANSI-repaint frame that displays
// live progress of a seed-hunter pipeline run. It reads atomic counters from
// *pipeline.Stats and computes human-readable rates, ETAs, and the
// educational "brute-force the full keyspace" number in years.
//
// Render is pure: given a Frame, it always returns the same text. Run
// drives a ticker loop that repaints the frame on a fixed interval, writing
// a cursor-home + clear-screen sequence before each frame so the terminal
// displays a stable in-place dashboard.
package dashboard

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/Chemaclass/seed-hunter/internal/pipeline"
)

// keyspaceCombinations is 2048^12, the total number of BIP-39 mnemonic
// combinations for a 12-word seed. This is the educational punchline the
// dashboard prints as "ETA full key".
const keyspaceCombinations = 5.444517870735015e+39

// secondsPerYear is the Julian year used for the ETA-in-years conversion.
const secondsPerYear = 365.25 * 24 * 3600

// templatePositions is the size of the BIP-39 wordlist, which is also the
// number of candidates iterated at a single --position slot.
const templatePositions = 2048

// placeholder is rendered for values that are unknown (e.g. rate when
// elapsed is zero, ETAs when rate is zero).
const placeholder = "—"

// clearScreen is the ANSI sequence "cursor home + erase display" used by
// Run to repaint the frame in place.
const clearScreen = "\033[H\033[2J"

// Mode tells the renderer which dashboard layout to draw.
type Mode int

const (
	// ModeSweep is the default per-position 2048-candidate progress bar.
	ModeSweep Mode = iota
	// ModeWalk is the full-keyspace walker, where the denominator is
	// 2048^12 and the per-position attempts counter is replaced with the
	// raw cursor and an absolute counter.
	ModeWalk
)

// Meta carries the static metadata that is rendered above the live
// counters. It is supplied once before the run begins and does not change
// across frames. (SessionID and ResumedAt are NOT here — they are populated
// by the pipeline after BeginSession runs and are read from *pipeline.Stats
// at frame time.)
type Meta struct {
	Mode         Mode
	TemplateHash string // already hashed; the dashboard never sees plaintext words
	Position     int    // sweep mode only
	API          string
	ScriptType   string // "segwit" or "legacy"
	Workers      int    // parallel deriver goroutines
	APIWorkers   int
	RateLimit    float64 // requests per second
	NAddresses   int
}

// keyspaceTotal is the full BIP-39 12-word keyspace size, 2048^12. Walk
// mode uses it as the denominator for the progress bar.
const keyspaceTotal = keyspaceCombinations

// Frame is a single rendered snapshot of the dashboard. Render consumes a
// Frame and returns the corresponding text block; Run builds a Frame from a
// *pipeline.Stats on every tick.
//
// In ModeWalk, Cursor is the latest 12-int cursor as a comma-separated
// string (e.g. "0,0,0,0,0,0,0,0,0,0,0,42") and Processed is the absolute
// number of candidates the walker has tried in this run. ResumedAt has no
// meaning in walk mode.
type Frame struct {
	Meta           Meta
	SessionID      int64
	Resumed        bool
	ResumedAt      int // sweep mode only; word index this run picked up at, -1 if fresh
	Cursor         string
	Processed      int64
	ValidMnemonics int64
	Hits           int64
	Errors         int64
	Elapsed        time.Duration
}

// Render returns the multi-line text frame for f. It does not include any
// terminal control sequences; the caller is responsible for clearing the
// screen between frames. Render is pure: same input, same output.
//
// The Mode field on f.Meta switches between the per-position sweep layout
// and the full-keyspace walk layout. Sweep mode shows attempts vs the
// 2048-candidate position keyspace; walk mode shows the cursor and the
// absolute counter against 2048^12.
func Render(f Frame) string {
	if f.Meta.Mode == ModeWalk {
		return renderWalk(f)
	}
	var b strings.Builder

	// Header.
	b.WriteString("seed-hunter — educational BIP-39 brute-force demo\n")
	b.WriteString("─────────────────────────────────────────────────\n")

	// Session/meta block.
	sessionLabel := fmt.Sprintf("session #%d", f.SessionID)
	if f.Resumed {
		sessionLabel += " (resumed)"
	}
	fmt.Fprintf(&b, "%-34s api      : %s\n", sessionLabel, f.Meta.API)
	fmt.Fprintf(&b, "template hash : %-20s script   : %s\n",
		truncateHash(f.Meta.TemplateHash), f.Meta.ScriptType)
	fmt.Fprintf(&b, "position      : %-20d workers  : derive=%d api=%d\n",
		f.Meta.Position, f.Meta.Workers, f.Meta.APIWorkers)
	fmt.Fprintf(&b, "rate limit    : %.2f req/s          addresses/candidate : %d\n",
		f.Meta.RateLimit, f.Meta.NAddresses)
	b.WriteString("\n")

	// Counters.
	totalProcessed := f.Processed
	if f.Resumed {
		totalProcessed = int64(f.ResumedAt+1) + f.Processed
	}
	if totalProcessed < 0 {
		totalProcessed = 0
	}
	if totalProcessed > templatePositions {
		totalProcessed = templatePositions
	}

	percent := float64(totalProcessed) / float64(templatePositions) * 100.0
	attemptsLine := fmt.Sprintf("attempts      : %d / %d           (%.2f%%)",
		totalProcessed, templatePositions, percent)
	if f.Resumed {
		attemptsLine += fmt.Sprintf("    [resumed at %d]", f.ResumedAt)
	}
	b.WriteString(attemptsLine)
	b.WriteString("\n")

	fmt.Fprintf(&b, "valid mnem    : %d\n", f.ValidMnemonics)
	fmt.Fprintf(&b, "hits          : %d\n", f.Hits)
	fmt.Fprintf(&b, "errors        : %d\n", f.Errors)
	fmt.Fprintf(&b, "elapsed       : %s\n", formatDuration(f.Elapsed))

	// Rate and ETAs.
	rate := computeRate(f.Processed, f.Elapsed)
	if rate > 0 && !math.IsInf(rate, 0) && !math.IsNaN(rate) {
		fmt.Fprintf(&b, "rate          : %.2f attempts/s\n", rate)

		remaining := int64(templatePositions) - totalProcessed
		if remaining < 0 {
			remaining = 0
		}
		etaSecs := float64(remaining) / rate
		etaDuration := time.Duration(etaSecs * float64(time.Second))
		fmt.Fprintf(&b, "ETA position  : %s\n", formatDuration(etaDuration))

		years := keyspaceCombinations / rate / secondsPerYear
		fmt.Fprintf(&b, "ETA full key  : %.1e years         ← (2048^12 / current rate)\n", years)
	} else {
		fmt.Fprintf(&b, "rate          : %s attempts/s\n", placeholder)
		fmt.Fprintf(&b, "ETA position  : %s\n", placeholder)
		fmt.Fprintf(&b, "ETA full key  : %s years         ← (2048^12 / current rate)\n", placeholder)
	}

	b.WriteString("\n")
	b.WriteString("press Ctrl+C to stop — progress is saved automatically\n")

	return b.String()
}

// renderWalk renders the full-keyspace walk dashboard. The denominator is
// 2048^12 ≈ 5.4e+39, the per-position progress bar is replaced with the
// raw cursor and an absolute counter, and the ETA line becomes the
// LITERAL projected wall time to walk the full keyspace at the current
// rate (not a hypothetical comparison).
func renderWalk(f Frame) string {
	var b strings.Builder

	b.WriteString("seed-hunter — full BIP-39 keyspace walk\n")
	b.WriteString("───────────────────────────────────────\n")

	sessionLabel := fmt.Sprintf("session #%d", f.SessionID)
	if f.Resumed {
		sessionLabel += " (resumed)"
	}
	fmt.Fprintf(&b, "%-34s api      : %s\n", sessionLabel, f.Meta.API)
	fmt.Fprintf(&b, "mode          : %-20s script   : %s\n", "walk", f.Meta.ScriptType)
	fmt.Fprintf(&b, "rate limit    : %.2f req/s          addresses/candidate : %d\n",
		f.Meta.RateLimit, f.Meta.NAddresses)
	b.WriteString("\n")

	cursorDisplay := f.Cursor
	if cursorDisplay == "" {
		cursorDisplay = "0,0,0,0,0,0,0,0,0,0,0,0"
	}
	fmt.Fprintf(&b, "cursor        : %s\n", cursorDisplay)
	fmt.Fprintf(&b, "attempts      : %d / 5.4e+39          (≈ 0%% of the keyspace)\n", f.Processed)
	fmt.Fprintf(&b, "valid mnem    : %d\n", f.ValidMnemonics)
	fmt.Fprintf(&b, "hits          : %d\n", f.Hits)
	fmt.Fprintf(&b, "errors        : %d\n", f.Errors)
	fmt.Fprintf(&b, "elapsed       : %s\n", formatDuration(f.Elapsed))

	rate := computeRate(f.Processed, f.Elapsed)
	if rate > 0 && !math.IsInf(rate, 0) && !math.IsNaN(rate) {
		fmt.Fprintf(&b, "rate          : %.2f attempts/s\n", rate)
		years := keyspaceTotal / rate / secondsPerYear
		fmt.Fprintf(&b, "ETA full key  : %.1e years\n", years)
	} else {
		fmt.Fprintf(&b, "rate          : %s attempts/s\n", placeholder)
		fmt.Fprintf(&b, "ETA full key  : %s years\n", placeholder)
	}

	b.WriteString("\n")
	b.WriteString("This walk will not finish in the lifetime of the universe.\n")
	b.WriteString("That is the entire point. Press Ctrl+C any time.\n")

	return b.String()
}

// Run periodically reads s and meta, renders a frame, and writes it to w
// after a cursor-home + clear-screen sequence. It returns when ctx is
// cancelled. interval controls the repaint cadence (e.g. 200ms).
func Run(ctx context.Context, w io.Writer, meta Meta, s *pipeline.Stats, interval time.Duration) {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Paint an initial frame immediately so callers see output without
	// waiting a full tick.
	paint(w, meta, s)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			paint(w, meta, s)
		}
	}
}

// paint builds a Frame from the current *pipeline.Stats snapshot and writes
// the clear-screen sequence followed by the rendered frame to w.
func paint(w io.Writer, meta Meta, s *pipeline.Stats) {
	frame := snapshot(meta, s)
	_, _ = io.WriteString(w, clearScreen)
	_, _ = io.WriteString(w, Render(frame))
}

// snapshot copies the atomic counters and the fixed fields from s into a
// Frame. The resumed flag is derived from the ResumedAt sentinel (-1 means
// fresh run). In walk mode, the live Cursor is also copied so the
// dashboard can render the current keyspace position.
func snapshot(meta Meta, s *pipeline.Stats) Frame {
	resumedAt := int(s.ResumedAt.Load())
	var cursor string
	if p := s.Cursor.Load(); p != nil {
		cursor = *p
	}
	return Frame{
		Meta:           meta,
		SessionID:      s.SessionID.Load(),
		Resumed:        resumedAt >= 0,
		ResumedAt:      resumedAt,
		Cursor:         cursor,
		Processed:      s.Processed.Load(),
		ValidMnemonics: s.ValidMnemonics.Load(),
		Hits:           s.Hits.Load(),
		Errors:         s.Errors.Load(),
		Elapsed:        time.Since(s.StartedAt),
	}
}

// computeRate returns attempts per second. It returns 0 for zero or
// negative elapsed times, and 0 for non-finite results so callers can treat
// "no rate yet" uniformly.
func computeRate(processed int64, elapsed time.Duration) float64 {
	secs := elapsed.Seconds()
	if secs <= 0 {
		return 0
	}
	rate := float64(processed) / secs
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0
	}
	return rate
}

// formatDuration formats d as "MMm SSs" for sub-hour durations and
// "Hh MMm SSs" for durations of one hour or more. Negative durations are
// clamped to zero.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	return fmt.Sprintf("%02dm %02ds", m, s)
}

// truncateHash returns the first 8 characters of h. Shorter hashes are
// returned unchanged so tests and callers do not need to pad.
func truncateHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}
