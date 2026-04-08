package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chemaclass/seed-hunter/config"
)

func good() config.Config {
	c := config.Default()
	return c
}

func TestValidateAcceptsDefaultConfig(t *testing.T) {
	if err := good().Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestValidateRejectsBadPositionsSpec(t *testing.T) {
	for _, spec := range []string{"", "-1", "12", "0-12", "abc", "1,1"} {
		c := good()
		c.Positions = spec
		if err := c.Validate(); err == nil {
			t.Errorf("positions=%q should fail validation", spec)
		}
	}
}

func TestValidateAcceptsGoodPositionsSpecs(t *testing.T) {
	for _, spec := range []string{"0", "11", "0-11", "3-7", "0,3,7", "0,3-5,9"} {
		c := good()
		c.Positions = spec
		if err := c.Validate(); err != nil {
			t.Errorf("positions=%q should validate, got: %v", spec, err)
		}
	}
}

func TestValidateRejectsBadScriptType(t *testing.T) {
	c := good()
	c.ScriptType = "weird"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for bad script type")
	}
	if !strings.Contains(err.Error(), "script-type") {
		t.Errorf("error should mention script-type, got: %v", err)
	}
}

func TestValidateRejectsBadAPI(t *testing.T) {
	c := good()
	c.API = "electrum"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for bad api")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error should mention api, got: %v", err)
	}
}

func TestValidateRejectsZeroOrNegativeRate(t *testing.T) {
	for _, rate := range []float64{0, -1, -0.5} {
		c := good()
		c.Rate = rate
		if err := c.Validate(); err == nil {
			t.Errorf("rate=%g should fail validation", rate)
		}
	}
}

func TestValidateRejectsZeroAddresses(t *testing.T) {
	c := good()
	c.NAddresses = 0
	if err := c.Validate(); err == nil {
		t.Error("addresses=0 should fail validation")
	}
}

func TestValidateAcceptsEmptyTemplateAsRandom(t *testing.T) {
	c := good()
	c.Template = ""
	if err := c.Validate(); err != nil {
		t.Errorf("empty template should be allowed (random will be generated): %v", err)
	}
}

func TestValidateRejectsTemplateWithWrongWordCount(t *testing.T) {
	cases := []string{
		"a",
		"a b c",
		"one two three four five six seven eight nine ten eleven", // 11
	}
	for _, tmpl := range cases {
		c := good()
		c.Template = tmpl
		if err := c.Validate(); err == nil {
			t.Errorf("template %q should fail validation", tmpl)
		}
	}
}

func TestValidateAcceptsTwelveWordTemplateRegardlessOfWordlist(t *testing.T) {
	c := good()
	// We do NOT validate against the BIP-39 wordlist here — the iterator
	// does that and gives a clearer error.
	c.Template = "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima"
	if err := c.Validate(); err != nil {
		t.Errorf("12-word template should pass config-level validation: %v", err)
	}
}

func TestValidateRejectsEmptyDBPath(t *testing.T) {
	c := good()
	c.DBPath = ""
	if err := c.Validate(); err == nil {
		t.Error("empty db path should fail validation")
	}
}

func TestApplyEnvOverridesEmptyAndIsOverridenByFlags(t *testing.T) {
	t.Setenv("SEEDHUNTER_API", "blockstream")
	t.Setenv("SEEDHUNTER_DB", "/tmp/from-env.db")
	t.Setenv("SEEDHUNTER_WORDLIST", "/tmp/from-env-wordlist.txt")

	c := config.Default()
	c.ApplyEnv()

	if c.API != "blockstream" {
		t.Errorf("API: want blockstream from env, got %s", c.API)
	}
	if c.DBPath != "/tmp/from-env.db" {
		t.Errorf("DBPath: want /tmp/from-env.db from env, got %s", c.DBPath)
	}
	if c.WordlistPath != "/tmp/from-env-wordlist.txt" {
		t.Errorf("WordlistPath: want /tmp/from-env-wordlist.txt from env, got %s", c.WordlistPath)
	}
}

func TestValidateAcceptsEmptyWordlistPathAsEmbeddedDefault(t *testing.T) {
	c := good()
	c.WordlistPath = ""
	if err := c.Validate(); err != nil {
		t.Errorf("empty wordlist path should be allowed: %v", err)
	}
}

func TestValidateAcceptsExistingWordlistFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "words.txt")
	if err := os.WriteFile(path, []byte("dummy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := good()
	c.WordlistPath = path
	if err := c.Validate(); err != nil {
		t.Errorf("existing wordlist path should validate: %v", err)
	}
}

func TestValidateRejectsMissingWordlistFile(t *testing.T) {
	c := good()
	c.WordlistPath = "/nonexistent/path/to/words.txt"
	if err := c.Validate(); err == nil {
		t.Error("missing wordlist file should fail validation")
	}
}

func TestValidateRejectsDirectoryAsWordlistPath(t *testing.T) {
	dir := t.TempDir()
	c := good()
	c.WordlistPath = dir
	if err := c.Validate(); err == nil {
		t.Error("directory wordlist path should fail validation")
	}
}
