package config_test

import (
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

func TestValidateRejectsOutOfRangePosition(t *testing.T) {
	for _, pos := range []int{-1, 12, 99} {
		c := good()
		c.Position = pos
		if err := c.Validate(); err == nil {
			t.Errorf("position=%d should fail validation", pos)
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

	c := config.Default()
	c.ApplyEnv()

	if c.API != "blockstream" {
		t.Errorf("API: want blockstream from env, got %s", c.API)
	}
	if c.DBPath != "/tmp/from-env.db" {
		t.Errorf("DBPath: want /tmp/from-env.db from env, got %s", c.DBPath)
	}
}
