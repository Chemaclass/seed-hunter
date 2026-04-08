package derivation

import (
	"errors"
	"testing"
)

// testMnemonic is the standard BIP-39 test vector mnemonic.
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestDeriveSegwitMatchesBIP84TestVector(t *testing.T) {
	d := New()
	got, err := d.Derive(testMnemonic, 1, ScriptSegwit)
	if err != nil {
		t.Fatalf("Derive returned unexpected error: %v", err)
	}
	want := []string{"bc1qcr8te4kr609gcawutmrza0j4xv80jy8z306fyu"}
	if len(got) != len(want) {
		t.Fatalf("got %d addresses, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDeriveLegacyMatchesBIP44TestVector(t *testing.T) {
	d := New()
	got, err := d.Derive(testMnemonic, 1, ScriptLegacy)
	if err != nil {
		t.Fatalf("Derive returned unexpected error: %v", err)
	}
	want := []string{"1LqBGSKuX5yYUonjxT5qGfpUsXKYYWeabA"}
	if len(got) != len(want) {
		t.Fatalf("got %d addresses, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDeriveReturnsRequestedNumberOfAddresses(t *testing.T) {
	d := New()
	const n = 3
	got, err := d.Derive(testMnemonic, n, ScriptSegwit)
	if err != nil {
		t.Fatalf("Derive returned unexpected error: %v", err)
	}
	if len(got) != n {
		t.Fatalf("got %d addresses, want %d", len(got), n)
	}
	seen := make(map[string]struct{}, n)
	for _, addr := range got {
		if _, dup := seen[addr]; dup {
			t.Errorf("duplicate address found: %q", addr)
		}
		seen[addr] = struct{}{}
	}
}

func TestDeriveInvalidMnemonicReturnsError(t *testing.T) {
	d := New()
	_, err := d.Derive("not a valid mnemonic", 1, ScriptSegwit)
	if err == nil {
		t.Fatal("expected error for invalid mnemonic, got nil")
	}
	if !errors.Is(err, ErrInvalidMnemonic) {
		t.Errorf("expected ErrInvalidMnemonic, got %v", err)
	}
}

func TestDeriveInvalidScriptTypeReturnsError(t *testing.T) {
	d := New()
	_, err := d.Derive(testMnemonic, 1, ScriptType("foo"))
	if err == nil {
		t.Fatal("expected error for invalid script type, got nil")
	}
	if !errors.Is(err, ErrInvalidScript) {
		t.Errorf("expected ErrInvalidScript, got %v", err)
	}
}

func TestDeriveZeroOrNegativeAddressesReturnsError(t *testing.T) {
	d := New()
	for _, n := range []int{0, -1} {
		_, err := d.Derive(testMnemonic, n, ScriptSegwit)
		if err == nil {
			t.Errorf("expected error for n=%d, got nil", n)
		}
	}
}
