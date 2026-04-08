package bip39

import (
	"strings"
	"testing"
)

const sampleMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestFingerprintIsDeterministic(t *testing.T) {
	t.Parallel()

	first := Fingerprint(sampleMnemonic)
	for range 5 {
		if got := Fingerprint(sampleMnemonic); got != first {
			t.Fatalf("Fingerprint not deterministic: first=%q got=%q", first, got)
		}
	}
}

func TestFingerprintDiffersForDifferentInputs(t *testing.T) {
	t.Parallel()

	other := "legal winner thank year wave sausage worth useful legal winner thank yellow"

	if Fingerprint(sampleMnemonic) == Fingerprint(other) {
		t.Fatal("Fingerprint collision on two different mnemonics")
	}
}

func TestFingerprintDoesNotLeakOriginal(t *testing.T) {
	t.Parallel()

	fp := Fingerprint(sampleMnemonic)
	for _, w := range strings.Fields(sampleMnemonic) {
		if strings.Contains(fp, w) {
			t.Fatalf("Fingerprint %q contains original word %q", fp, w)
		}
	}
}

func TestFingerprintLengthIsStable(t *testing.T) {
	t.Parallel()

	inputs := []string{
		sampleMnemonic,
		"legal winner thank year wave sausage worth useful legal winner thank yellow",
		"",
		"single",
	}
	const want = 64 // SHA-256 hex digest length
	for _, in := range inputs {
		if got := len(Fingerprint(in)); got != want {
			t.Fatalf("Fingerprint(%q) length = %d, want %d", in, got, want)
		}
	}
}
