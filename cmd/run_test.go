package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validTwelveWordMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// fixedGenerator returns the same mnemonic every time and records how often
// it was called. Useful for verifying that the sidecar short-circuits the
// generator on subsequent runs.
type fixedGenerator struct {
	mnemonic string
	calls    int
}

func (g *fixedGenerator) generate() (string, error) {
	g.calls++
	return g.mnemonic, nil
}

func TestResolveTemplateUsesExplicitTemplateVerbatim(t *testing.T) {
	gen := &fixedGenerator{mnemonic: validTwelveWordMnemonic}
	var out bytes.Buffer

	got, generated, err := resolveTemplate(validTwelveWordMnemonic, "", &out, gen.generate)
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if generated {
		t.Error("generated should be false when template is supplied")
	}
	if len(got) != 12 || got[0] != "abandon" || got[11] != "about" {
		t.Errorf("template not parsed correctly: %v", got)
	}
	if gen.calls != 0 {
		t.Errorf("generator must not be called when template is supplied; calls=%d", gen.calls)
	}
}

func TestResolveTemplateRejectsTemplateWithWrongWordCount(t *testing.T) {
	gen := &fixedGenerator{mnemonic: validTwelveWordMnemonic}
	var out bytes.Buffer

	_, _, err := resolveTemplate("abandon abandon", "", &out, gen.generate)
	if err == nil {
		t.Fatal("expected error for short template")
	}
	if !strings.Contains(err.Error(), "12 words") {
		t.Errorf("error should mention 12 words, got: %v", err)
	}
}

func TestResolveTemplateGeneratesAndWritesSidecarOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "test.db.template")
	gen := &fixedGenerator{mnemonic: validTwelveWordMnemonic}
	var out bytes.Buffer

	got, generated, err := resolveTemplate("", sidecar, &out, gen.generate)
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if !generated {
		t.Error("generated should be true on first run")
	}
	if len(got) != 12 {
		t.Errorf("expected 12 words, got %d", len(got))
	}
	if gen.calls != 1 {
		t.Errorf("generator should be called exactly once; calls=%d", gen.calls)
	}

	// Sidecar must exist and contain the mnemonic.
	data, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	if strings.TrimSpace(string(data)) != validTwelveWordMnemonic {
		t.Errorf("sidecar content mismatch: got %q", string(data))
	}

	// User must have been told about the demo seed.
	if !strings.Contains(out.String(), "DO NOT FUND") {
		t.Error("output should contain the 'DO NOT FUND' notice on first generation")
	}
	if !strings.Contains(out.String(), sidecar) {
		t.Errorf("output should mention the sidecar path %s", sidecar)
	}
}

func TestResolveTemplateReusesExistingSidecarOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "test.db.template")
	if err := os.WriteFile(sidecar, []byte(validTwelveWordMnemonic+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gen := &fixedGenerator{mnemonic: "this should not be used"}
	var out bytes.Buffer

	got, generated, err := resolveTemplate("", sidecar, &out, gen.generate)
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if generated {
		t.Error("generated should be false when sidecar is reused")
	}
	if len(got) != 12 || got[0] != "abandon" || got[11] != "about" {
		t.Errorf("sidecar template not loaded correctly: %v", got)
	}
	if gen.calls != 0 {
		t.Errorf("generator must not be called when sidecar is reused; calls=%d", gen.calls)
	}
	if !strings.Contains(out.String(), "Reusing saved demo seed") {
		t.Error("output should announce that the saved seed is being reused")
	}
	// Privacy: the second-run "reuse" message must NOT contain the actual
	// mnemonic words, just the file path. (The first-run output is allowed
	// to print the mnemonic because that's where it's generated.)
	if strings.Contains(out.String(), "abandon abandon abandon") {
		t.Error("reuse message must not echo the mnemonic in plaintext")
	}
}

func TestResolveTemplateRegeneratesIfSidecarIsMalformed(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "test.db.template")
	if err := os.WriteFile(sidecar, []byte("only three words here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gen := &fixedGenerator{mnemonic: validTwelveWordMnemonic}
	var out bytes.Buffer

	got, generated, err := resolveTemplate("", sidecar, &out, gen.generate)
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if !generated {
		t.Error("generated should be true when malformed sidecar triggers regeneration")
	}
	if len(got) != 12 {
		t.Errorf("expected 12 words, got %d", len(got))
	}
	if gen.calls != 1 {
		t.Errorf("generator should be called once; calls=%d", gen.calls)
	}

	// The sidecar should now be overwritten with the new mnemonic.
	data, _ := os.ReadFile(sidecar)
	if strings.TrimSpace(string(data)) != validTwelveWordMnemonic {
		t.Errorf("sidecar should be overwritten with new mnemonic, got %q", string(data))
	}
}

func TestResolveTemplatePropagatesGeneratorError(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "test.db.template")
	failGen := func() (string, error) {
		return "", errors.New("entropy boom")
	}
	var out bytes.Buffer

	_, _, err := resolveTemplate("", sidecar, &out, failGen)
	if err == nil {
		t.Fatal("expected error from generator")
	}
	if !strings.Contains(err.Error(), "entropy boom") {
		t.Errorf("expected wrapped generator error, got: %v", err)
	}
}

func TestSidecarPathFromDBPath(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"./seed-hunter.db": "./seed-hunter.db.template",
		"/tmp/run.db":      "/tmp/run.db.template",
		"a/b/c/db.sqlite":  "a/b/c/db.sqlite.template",
	}
	for in, want := range cases {
		if got := sidecarPath(in); got != want {
			t.Errorf("sidecarPath(%q) = %q, want %q", in, got, want)
		}
	}
}
