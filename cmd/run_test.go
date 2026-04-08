package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Chemaclass/seed-hunter/config"
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
	fs.Int("position", 0, "")
	fs.Int("addresses", 0, "")
	fs.String("api", "", "")
	fs.String("script-type", "", "")
	fs.Float64("rate", 0, "")
	fs.String("wordlist", "", "")
	return fs
}

func TestInheritFromSessionFillsAllUnsetFields(t *testing.T) {
	cfg := config.Default()
	last := &storage.Session{
		Template:     "the actual previous mnemonic words go here ok x y",
		Position:     7,
		API:          "blockstream",
		AddressType:  "legacy",
		NAddresses:   5,
		Rate:         3.5,
		WordlistPath: "/tmp/spanish.txt",
	}
	flags := makeFlagsForInherit()

	inheritFromSession(&cfg, last, flags)

	if cfg.Template != last.Template {
		t.Errorf("Template: want %q, got %q", last.Template, cfg.Template)
	}
	if cfg.Position != 7 {
		t.Errorf("Position: want 7, got %d", cfg.Position)
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
}

func TestInheritFromSessionDoesNotOverrideExplicitFlags(t *testing.T) {
	cfg := config.Default()
	cfg.Position = 11   // user passed --position 11
	cfg.API = "mempool" // user passed --api mempool
	cfg.Rate = 7.0      // user passed --rate 7
	last := &storage.Session{
		Template:    "previous template here this should still be inherited y",
		Position:    3,
		API:         "blockstream",
		AddressType: "legacy",
		NAddresses:  9,
		Rate:        2.0,
	}
	flags := makeFlagsForInherit()
	_ = flags.Set("position", "11")
	_ = flags.Set("api", "mempool")
	_ = flags.Set("rate", "7")

	inheritFromSession(&cfg, last, flags)

	// User flags must be preserved.
	if cfg.Position != 11 {
		t.Errorf("Position must stay 11 (user-set), got %d", cfg.Position)
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
		// cfg.Template was already empty by default, but the test still
		// asserts the inherited value isn't a non-empty placeholder.
		t.Errorf("Template should remain empty, got %q", cfg.Template)
	}
	if cfg.Position != 2 {
		t.Errorf("Position should be inherited, got %d", cfg.Position)
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
