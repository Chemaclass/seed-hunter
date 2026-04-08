package bip39_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Chemaclass/seed-hunter/internal/bip39"
	"github.com/Chemaclass/seed-hunter/internal/wordlist"
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

// newIterator builds an Iterator backed by the embedded English wordlist.
// All tests in this file share this constructor so a wordlist change in one
// place propagates everywhere.
func newIterator(t *testing.T) *bip39.Iterator {
	t.Helper()
	it, err := bip39.NewIterator(wordlist.Default())
	if err != nil {
		t.Fatalf("NewIterator: %v", err)
	}
	return it
}

func TestNewIteratorRejectsWordlistOfWrongSize(t *testing.T) {
	t.Parallel()

	_, err := bip39.NewIterator([]string{"only", "three", "words"})
	if !errors.Is(err, bip39.ErrInvalidWordlist) {
		t.Fatalf("expected ErrInvalidWordlist, got %v", err)
	}
}

func TestIterateYields2048UniqueMnemonics(t *testing.T) {
	t.Parallel()

	it := newIterator(t)
	seq, err := it.Iterate(validTemplate, 3)
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
	it := newIterator(t)
	seq, err := it.Iterate(validTemplate, pos)
	if err != nil {
		t.Fatalf("Iterate returned unexpected error: %v", err)
	}

	wordset := make(map[string]struct{}, 2048)
	for _, w := range wordlist.Default() {
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
			break
		}
	}

	if checked == 0 {
		t.Fatal("iterator yielded zero entries")
	}
}

func TestIterateInvalidTemplateLengthErrors(t *testing.T) {
	t.Parallel()

	it := newIterator(t)
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
			_, err := it.Iterate(tmpl, 0)
			if !errors.Is(err, bip39.ErrInvalidTemplate) {
				t.Fatalf("expected ErrInvalidTemplate, got %v", err)
			}
		})
	}
}

func TestIterateInvalidWordErrors(t *testing.T) {
	t.Parallel()

	it := newIterator(t)
	tmpl := append([]string(nil), validTemplate...)
	tmpl[7] = "notabip39word"

	_, err := it.Iterate(tmpl, 0)
	if !errors.Is(err, bip39.ErrInvalidTemplate) {
		t.Fatalf("expected ErrInvalidTemplate, got %v", err)
	}
}

func TestIterateInvalidPositionErrors(t *testing.T) {
	t.Parallel()

	it := newIterator(t)
	for _, pos := range []int{-1, 12} {
		if _, err := it.Iterate(validTemplate, pos); !errors.Is(err, bip39.ErrInvalidPosition) {
			t.Fatalf("pos=%d: expected ErrInvalidPosition, got %v", pos, err)
		}
	}
}

func TestCandidateAtMatchesIteratorOrder(t *testing.T) {
	t.Parallel()

	const pos = 4
	it := newIterator(t)
	seq, err := it.Iterate(validTemplate, pos)
	if err != nil {
		t.Fatalf("Iterate returned unexpected error: %v", err)
	}

	yielded := make([]string, 0, 2048)
	for m := range seq {
		yielded = append(yielded, m)
	}

	for _, i := range []int{0, 1, 7, 42, 123, 1024, 2047} {
		got, err := it.CandidateAt(validTemplate, pos, i)
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

	it := newIterator(t)
	for _, i := range []int{-1, 2048} {
		if _, err := it.CandidateAt(validTemplate, 0, i); !errors.Is(err, bip39.ErrInvalidPosition) {
			t.Fatalf("i=%d: expected ErrInvalidPosition, got %v", i, err)
		}
	}
}

func TestIteratorWordsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	it := newIterator(t)
	first := it.Words()
	first[0] = "MUTATED"
	second := it.Words()
	if second[0] == "MUTATED" {
		t.Errorf("Words() must return a defensive copy")
	}
}
