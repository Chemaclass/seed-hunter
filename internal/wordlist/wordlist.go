// Package wordlist loads a BIP-39 wordlist from disk and validates it.
//
// The package embeds the canonical English BIP-39 wordlist (taken verbatim
// from https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt) so
// the binary always has a working default. Callers can opt into a different
// list — another official BIP-39 language file, or a custom list of any 2048
// unique words — by passing a path to Load.
//
// Note: BIP-39 checksum verification and PBKDF2 seed derivation in the
// underlying tyler-smith/go-bip39 library use a process-global wordlist set
// via bip39.SetWordList. cmd/run.go calls SetWordList(...) on whatever this
// package returns so the iterator and the deriver always agree on the words.
package wordlist

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
)

//go:embed english.txt
var englishRaw string

// Size is the number of words a valid BIP-39 wordlist must contain.
const Size = 2048

// Sentinel errors returned by Load.
var (
	// ErrWrongLength is returned when a wordlist file does not contain
	// exactly Size non-empty lines.
	ErrWrongLength = errors.New("wordlist: must contain exactly 2048 words")
	// ErrDuplicateWord is returned when a wordlist file contains the same
	// word twice. BIP-39 forbids duplicates because the word index must be
	// unambiguous.
	ErrDuplicateWord = errors.New("wordlist: duplicate word")
	// ErrEmptyLine is returned when a wordlist file contains an empty line
	// (after trimming) somewhere other than the trailing newline.
	ErrEmptyLine = errors.New("wordlist: empty line")
)

// Default returns the embedded canonical English BIP-39 wordlist. The
// returned slice is a defensive copy — callers may mutate it without
// affecting the embedded original.
func Default() []string {
	return parseLines(englishRaw)
}

// Load reads a wordlist from path and returns it. If path is empty the
// embedded English default is returned. The file must contain exactly 2048
// unique non-empty lines (UTF-8, one word per line); any other shape is an
// error.
func Load(path string) ([]string, error) {
	if path == "" {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wordlist: read %s: %w", path, err)
	}
	words, err := parseAndValidate(string(data))
	if err != nil {
		return nil, fmt.Errorf("wordlist: %s: %w", path, err)
	}
	return words, nil
}

// Validate enforces the BIP-39-shaped invariants on words: exactly 2048
// entries, no empty lines, no duplicates. It is exported so callers that
// already have a slice (e.g. tests) can reuse the same checks.
func Validate(words []string) error {
	if len(words) != Size {
		return fmt.Errorf("%w: got %d", ErrWrongLength, len(words))
	}
	seen := make(map[string]struct{}, Size)
	for i, w := range words {
		if w == "" {
			return fmt.Errorf("%w at index %d", ErrEmptyLine, i)
		}
		if _, dup := seen[w]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateWord, w)
		}
		seen[w] = struct{}{}
	}
	return nil
}

func parseAndValidate(raw string) ([]string, error) {
	words := parseLines(raw)
	if err := Validate(words); err != nil {
		return nil, err
	}
	return words, nil
}

// parseLines splits raw into trimmed lines, dropping any trailing newline
// but preserving internal empty lines so Validate can flag them as errors.
func parseLines(raw string) []string {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, "\r\t ")
	}
	return out
}
