package bip39

import (
	"errors"
	"strings"
	"testing"

	extbip39 "github.com/tyler-smith/go-bip39"
)

// validTemplate is the canonical BIP-39 test vector "all abandon" mnemonic
// with a valid checksum. We only need it as a shape-valid 12-word template
// composed of BIP-39 words; the iterator itself does not care about
// checksums.
var validTemplate = []string{
	"abandon", "abandon", "abandon", "abandon",
	"abandon", "abandon", "abandon", "abandon",
	"abandon", "abandon", "abandon", "about",
}

func TestIterateYields2048UniqueMnemonics(t *testing.T) {
	t.Parallel()

	seq, err := Iterate(validTemplate, 3)
	if err != nil {
		t.Fatalf("Iterate returned unexpected error: %v", err)
	}

	seen := make(map[string]struct{}, 2048)
	for m := range seq {
		seen[m] = struct{}{}
	}

	if got, want := len(seen), 2048; got != want {
		t.Fatalf("expected %d unique mnemonics, got %d", want, got)
	}
}

func TestIterateRespectsPosition(t *testing.T) {
	t.Parallel()

	const pos = 5
	seq, err := Iterate(validTemplate, pos)
	if err != nil {
		t.Fatalf("Iterate returned unexpected error: %v", err)
	}

	wordset := make(map[string]struct{}, 2048)
	for _, w := range extbip39.GetWordList() {
		wordset[w] = struct{}{}
	}

	checked := 0
	for m := range seq {
		parts := strings.Split(m, " ")
		if len(parts) != 12 {
			t.Fatalf("expected 12 words, got %d in %q", len(parts), m)
		}
		for i, w := range parts {
			if i == pos {
				if _, ok := wordset[w]; !ok {
					t.Fatalf("word at position %d (%q) is not a BIP-39 word", pos, w)
				}
				continue
			}
			if w != validTemplate[i] {
				t.Fatalf("position %d: expected %q (template), got %q", i, validTemplate[i], w)
			}
		}
		checked++
		if checked >= 50 {
			// Sampling the first 50 entries is enough to demonstrate the
			// invariant — no need to walk the full 2048.
			break
		}
	}

	if checked == 0 {
		t.Fatal("iterator yielded zero entries")
	}
}

func TestIterateInvalidTemplateLengthErrors(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"eleven words": {
			"abandon", "abandon", "abandon", "abandon",
			"abandon", "abandon", "abandon", "abandon",
			"abandon", "abandon", "abandon",
		},
		"thirteen words": {
			"abandon", "abandon", "abandon", "abandon",
			"abandon", "abandon", "abandon", "abandon",
			"abandon", "abandon", "abandon", "about", "abandon",
		},
	}

	for name, tmpl := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Iterate(tmpl, 0)
			if !errors.Is(err, ErrInvalidTemplate) {
				t.Fatalf("expected ErrInvalidTemplate, got %v", err)
			}
		})
	}
}

func TestIterateInvalidWordErrors(t *testing.T) {
	t.Parallel()

	tmpl := append([]string(nil), validTemplate...)
	tmpl[7] = "notabip39word"

	_, err := Iterate(tmpl, 0)
	if !errors.Is(err, ErrInvalidTemplate) {
		t.Fatalf("expected ErrInvalidTemplate, got %v", err)
	}
}

func TestIterateInvalidPositionErrors(t *testing.T) {
	t.Parallel()

	for _, pos := range []int{-1, 12} {
		if _, err := Iterate(validTemplate, pos); !errors.Is(err, ErrInvalidPosition) {
			t.Fatalf("pos=%d: expected ErrInvalidPosition, got %v", pos, err)
		}
	}
}

func TestCandidateAtMatchesIteratorOrder(t *testing.T) {
	t.Parallel()

	const pos = 4
	seq, err := Iterate(validTemplate, pos)
	if err != nil {
		t.Fatalf("Iterate returned unexpected error: %v", err)
	}

	yielded := make([]string, 0, 2048)
	for m := range seq {
		yielded = append(yielded, m)
	}

	for _, i := range []int{0, 1, 7, 42, 123, 1024, 2047} {
		got, err := CandidateAt(validTemplate, pos, i)
		if err != nil {
			t.Fatalf("CandidateAt(%d) returned error: %v", i, err)
		}
		if got != yielded[i] {
			t.Fatalf("CandidateAt(%d) = %q, iterator[%d] = %q", i, got, i, yielded[i])
		}
	}
}

func TestCandidateAtInvalidIndexErrors(t *testing.T) {
	t.Parallel()

	for _, i := range []int{-1, 2048} {
		if _, err := CandidateAt(validTemplate, 0, i); !errors.Is(err, ErrInvalidPosition) {
			t.Fatalf("i=%d: expected ErrInvalidPosition, got %v", i, err)
		}
	}
}
