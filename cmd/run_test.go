package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Chemaclass/seed-hunter/config"
	"github.com/Chemaclass/seed-hunter/internal/pipeline"
	"github.com/Chemaclass/seed-hunter/internal/storage"
)

const validTwelveWordMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestResolveTemplateUsesExplicitTemplateVerbatim(t *testing.T) {
	called := false
	gen := func() (string, error) {
		called = true
		return validTwelveWordMnemonic, nil
	}
	var out bytes.Buffer

	got, err := resolveTemplate(validTwelveWordMnemonic, &out, gen)
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if len(got) != 12 || got[0] != "abandon" || got[11] != "about" {
		t.Errorf("template not parsed correctly: %v", got)
	}
	if called {
		t.Error("generator must not be called when template is supplied")
	}
}

func TestResolveTemplateRejectsTemplateWithWrongWordCount(t *testing.T) {
	var out bytes.Buffer
	_, err := resolveTemplate("abandon abandon", &out, func() (string, error) { return "", nil })
	if err == nil {
		t.Fatal("expected error for short template")
	}
	if !strings.Contains(err.Error(), "12 words") {
		t.Errorf("error should mention 12 words, got: %v", err)
	}
}

func TestResolveTemplateGeneratesWhenTemplateIsEmpty(t *testing.T) {
	gen := func() (string, error) { return validTwelveWordMnemonic, nil }
	var out bytes.Buffer

	got, err := resolveTemplate("", &out, gen)
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if len(got) != 12 {
		t.Errorf("expected 12 words, got %d", len(got))
	}
	if !strings.Contains(out.String(), "DO NOT FUND") {
		t.Error("output must contain the 'DO NOT FUND' notice")
	}
}

func TestResolveTemplatePropagatesGeneratorError(t *testing.T) {
	gen := func() (string, error) { return "", errors.New("entropy boom") }
	var out bytes.Buffer

	_, err := resolveTemplate("", &out, gen)
	if err == nil {
		t.Fatal("expected error from generator")
	}
	if !strings.Contains(err.Error(), "entropy boom") {
		t.Errorf("expected wrapped generator error, got: %v", err)
	}
}

// makeFlagsForInherit builds a pflag.FlagSet that mirrors the production
// run command's flags so we can drive inheritFromSession in tests. Calls to
// fs.Set(name, value) mark a flag as Changed, which is exactly the signal
// inheritFromSession uses to decide whether to keep the user's value or
// overlay the previous-session value.
func makeFlagsForInherit() *pflag.FlagSet {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("template", "", "")
	fs.String("positions", "", "")
	fs.Int("addresses", 0, "")
	fs.String("api", "", "")
	fs.String("script-type", "", "")
	fs.Float64("rate", 0, "")
	fs.String("wordlist", "", "")
	fs.Int("workers", 0, "")
	return fs
}

func TestInheritFromSessionFillsAllUnsetFields(t *testing.T) {
	cfg := config.Default()
	last := &storage.Session{
		Template:      "the actual previous mnemonic words go here ok x y",
		Position:      7,
		API:           "blockstream",
		AddressType:   "legacy",
		NAddresses:    5,
		Rate:          3.5,
		WordlistPath:  "/tmp/spanish.txt",
		Workers:       6,
		PositionsSpec: "0,3,7",
	}
	flags := makeFlagsForInherit()

	inheritFromSession(&cfg, last, flags)

	if cfg.Template != last.Template {
		t.Errorf("Template: want %q, got %q", last.Template, cfg.Template)
	}
	if cfg.Positions != "0,3,7" {
		t.Errorf("Positions: want 0,3,7, got %s", cfg.Positions)
	}
	if cfg.NAddresses != 5 {
		t.Errorf("NAddresses: want 5, got %d", cfg.NAddresses)
	}
	if cfg.API != "blockstream" {
		t.Errorf("API: want blockstream, got %s", cfg.API)
	}
	if cfg.ScriptType != "legacy" {
		t.Errorf("ScriptType: want legacy, got %s", cfg.ScriptType)
	}
	if cfg.Rate != 3.5 {
		t.Errorf("Rate: want 3.5, got %g", cfg.Rate)
	}
	if cfg.WordlistPath != "/tmp/spanish.txt" {
		t.Errorf("WordlistPath: want /tmp/spanish.txt, got %s", cfg.WordlistPath)
	}
	if cfg.Workers != 6 {
		t.Errorf("Workers: want 6, got %d", cfg.Workers)
	}
}

func TestInheritFromSessionDoesNotOverrideExplicitFlags(t *testing.T) {
	cfg := config.Default()
	cfg.Positions = "11" // user passed --positions 11
	cfg.API = "mempool"  // user passed --api mempool
	cfg.Rate = 7.0       // user passed --rate 7
	last := &storage.Session{
		Template:      "previous template here this should still be inherited y",
		Position:      3,
		API:           "blockstream",
		AddressType:   "legacy",
		NAddresses:    9,
		Rate:          2.0,
		PositionsSpec: "0-11",
	}
	flags := makeFlagsForInherit()
	_ = flags.Set("positions", "11")
	_ = flags.Set("api", "mempool")
	_ = flags.Set("rate", "7")

	inheritFromSession(&cfg, last, flags)

	// User flags must be preserved.
	if cfg.Positions != "11" {
		t.Errorf("Positions must stay 11 (user-set), got %s", cfg.Positions)
	}
	if cfg.API != "mempool" {
		t.Errorf("API must stay mempool (user-set), got %s", cfg.API)
	}
	if cfg.Rate != 7.0 {
		t.Errorf("Rate must stay 7 (user-set), got %g", cfg.Rate)
	}
	// Unset fields must be inherited.
	if cfg.Template != last.Template {
		t.Errorf("Template should be inherited, got %q", cfg.Template)
	}
	if cfg.NAddresses != 9 {
		t.Errorf("NAddresses should be inherited as 9, got %d", cfg.NAddresses)
	}
	if cfg.ScriptType != "legacy" {
		t.Errorf("ScriptType should be inherited as legacy, got %s", cfg.ScriptType)
	}
}

func TestInheritFromSessionIgnoresEmptyTemplateInLastSession(t *testing.T) {
	// A session row whose Template is "" (e.g. created before the migration
	// added the column) must NOT clobber the cfg.Template default with empty.
	cfg := config.Default()
	last := &storage.Session{
		Template:    "",
		Position:    2,
		API:         "mempool",
		AddressType: "segwit",
		NAddresses:  1,
		Rate:        2,
	}
	flags := makeFlagsForInherit()

	inheritFromSession(&cfg, last, flags)

	if cfg.Template != "" {
		t.Errorf("Template should remain empty, got %q", cfg.Template)
	}
}

func TestInheritFromSessionIgnoresZeroRateInLastSession(t *testing.T) {
	cfg := config.Default()
	defaultRate := cfg.Rate
	last := &storage.Session{
		Position:    0,
		API:         "mempool",
		AddressType: "segwit",
		NAddresses:  1,
		Rate:        0, // legacy rows pre-migration have rate=0
	}
	flags := makeFlagsForInherit()

	inheritFromSession(&cfg, last, flags)

	if cfg.Rate != defaultRate {
		t.Errorf("Rate should remain default %g when last.Rate==0, got %g", defaultRate, cfg.Rate)
	}
}

// ── sweepPositions and startIndexInPositions ──────────────────────────────

func TestStartIndexInPositionsHandlesMatchAndMissAndNegative(t *testing.T) {
	pos := []int{0, 3, 5, 9}
	cases := []struct {
		current int
		want    int
	}{
		{0, 0}, // first
		{3, 1},
		{5, 2},
		{9, 3},  // last
		{4, 0},  // not in list → start at head
		{-1, 0}, // sentinel "no inherited position"
	}
	for _, c := range cases {
		got := startIndexInPositions(pos, c.current)
		if got != c.want {
			t.Errorf("startIndexInPositions(_, %d) = %d, want %d", c.current, got, c.want)
		}
	}
}

func TestSweepPositionsVisitsEveryPositionInOrder(t *testing.T) {
	var visited []int
	runOne := func(_ context.Context, pos int) (pipeline.Result, error) {
		visited = append(visited, pos)
		return pipeline.Result{FinalStatus: storage.StatusCompleted, EndIndex: 2047}, nil
	}
	var out bytes.Buffer
	outcome, err := sweepPositionsAndReport(context.Background(), &out, []int{0, 3, 5, 9}, 0, runOne)
	if err != nil {
		t.Fatalf("sweepPositionsAndReport: %v", err)
	}
	if outcome != sweepCompleted {
		t.Errorf("outcome: want sweepCompleted, got %v", outcome)
	}
	want := []int{0, 3, 5, 9}
	if len(visited) != len(want) {
		t.Fatalf("visited %d positions, want %d (visited=%v)", len(visited), len(want), visited)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Errorf("visit order: want %v, got %v", want, visited)
			break
		}
	}
	if !strings.Contains(out.String(), "Sweep complete") {
		t.Errorf("missing sweep complete message; output:\n%s", out.String())
	}
}

func TestSweepPositionsHonorsStartIdx(t *testing.T) {
	var visited []int
	runOne := func(_ context.Context, pos int) (pipeline.Result, error) {
		visited = append(visited, pos)
		return pipeline.Result{FinalStatus: storage.StatusCompleted}, nil
	}
	var out bytes.Buffer
	outcome, err := sweepPositionsAndReport(context.Background(), &out, []int{0, 1, 2, 3}, 2, runOne)
	if err != nil {
		t.Fatalf("sweepPositionsAndReport: %v", err)
	}
	if outcome != sweepCompleted {
		t.Errorf("outcome: want sweepCompleted, got %v", outcome)
	}
	want := []int{2, 3}
	if len(visited) != len(want) || visited[0] != 2 || visited[1] != 3 {
		t.Errorf("startIdx not honored: got %v, want %v", visited, want)
	}
}

func TestSweepPositionsStopsOnPaused(t *testing.T) {
	var visited []int
	runOne := func(_ context.Context, pos int) (pipeline.Result, error) {
		visited = append(visited, pos)
		// First position pauses (Ctrl+C), sweep must stop.
		return pipeline.Result{FinalStatus: storage.StatusPaused, EndIndex: 137}, nil
	}
	var out bytes.Buffer
	outcome, err := sweepPositionsAndReport(context.Background(), &out, []int{0, 1, 2}, 0, runOne)
	if err != nil {
		t.Fatalf("sweepPositionsAndReport: %v", err)
	}
	if outcome != sweepPaused {
		t.Errorf("outcome: want sweepPaused, got %v", outcome)
	}
	if len(visited) != 1 || visited[0] != 0 {
		t.Errorf("paused result should stop sweep at first visited position, got %v", visited)
	}
	if !strings.Contains(out.String(), "Position paused") {
		t.Errorf("missing 'Position paused' message; output:\n%s", out.String())
	}
}

func TestSweepPositionsStopsOnContextCancel(t *testing.T) {
	var visited []int
	runOne := func(_ context.Context, pos int) (pipeline.Result, error) {
		visited = append(visited, pos)
		return pipeline.Result{FinalStatus: storage.StatusCompleted}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE starting
	var out bytes.Buffer
	outcome, err := sweepPositionsAndReport(ctx, &out, []int{0, 1, 2}, 0, runOne)
	if err != nil {
		t.Fatalf("sweepPositionsAndReport: %v", err)
	}
	if outcome != sweepPaused {
		t.Errorf("outcome: want sweepPaused after ctx cancel, got %v", outcome)
	}
	if len(visited) != 0 {
		t.Errorf("cancelled ctx should prevent any visits, got %v", visited)
	}
}

func TestSweepPositionsPropagatesRunOneError(t *testing.T) {
	bang := errors.New("runner exploded")
	runOne := func(_ context.Context, _ int) (pipeline.Result, error) {
		return pipeline.Result{}, bang
	}
	var out bytes.Buffer
	_, err := sweepPositionsAndReport(context.Background(), &out, []int{0}, 0, runOne)
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	if !errors.Is(err, bang) {
		t.Errorf("expected wrapped %v, got %v", bang, err)
	}
}

func TestSweepPositionsHandlesStartIdxPastEnd(t *testing.T) {
	var visited []int
	runOne := func(_ context.Context, pos int) (pipeline.Result, error) {
		visited = append(visited, pos)
		return pipeline.Result{FinalStatus: storage.StatusCompleted}, nil
	}
	var out bytes.Buffer
	outcome, err := sweepPositionsAndReport(context.Background(), &out, []int{0, 1}, 5, runOne)
	if err != nil {
		t.Fatalf("sweepPositionsAndReport: %v", err)
	}
	if outcome != sweepNoWork {
		t.Errorf("outcome: want sweepNoWork, got %v", outcome)
	}
	if len(visited) != 0 {
		t.Errorf("startIdx past end should yield no visits, got %v", visited)
	}
	if !strings.Contains(out.String(), "nothing to sweep") {
		t.Errorf("missing 'nothing to sweep' message; output:\n%s", out.String())
	}
}
