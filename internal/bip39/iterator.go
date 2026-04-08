// Package bip39 contains helpers for iterating BIP-39 mnemonic candidates and
// for fingerprinting mnemonics so they can be logged without persisting the
// plaintext words.
//
// The wordlist is not hard-coded inside this package: callers construct an
// Iterator with whatever wordlist they want (typically loaded from disk via
// internal/wordlist). This keeps the iterator agnostic of language choice.
package bip39

import (
	"errors"
	"iter"
	"strings"
)

const (
	templateLength = 12
	wordlistSize   = 2048
)

// ErrInvalidTemplate is returned when the supplied template is not exactly
// 12 words long or contains a word outside the iterator's wordlist.
var ErrInvalidTemplate = errors.New("bip39: invalid template")

// ErrInvalidPosition is returned when the supplied position (or candidate
// index) is outside the valid range.
var ErrInvalidPosition = errors.New("bip39: position out of range")

// ErrInvalidWordlist is returned by NewIterator when the supplied wordlist
// is not exactly 2048 entries.
var ErrInvalidWordlist = errors.New("bip39: wordlist must be 2048 words")

// Iterator yields candidate mnemonics by substituting every word from a
// fixed wordlist into a single position of a 12-word template.
type Iterator struct {
	words   []string
	wordSet map[string]struct{}
}

// NewIterator returns an Iterator that uses the given wordlist. The
// wordlist must contain exactly 2048 entries; entries are not de-duplicated
// (the wordlist package handles that). The slice is copied defensively so
// callers can mutate it after construction without affecting the iterator.
func NewIterator(words []string) (*Iterator, error) {
	if len(words) != wordlistSize {
		return nil, ErrInvalidWordlist
	}
	cp := append([]string(nil), words...)
	set := make(map[string]struct{}, len(cp))
	for _, w := range cp {
		set[w] = struct{}{}
	}
	return &Iterator{words: cp, wordSet: set}, nil
}

// Words returns a defensive copy of the iterator's wordlist. Useful for
// callers that need to bind the same wordlist into another package.
func (it *Iterator) Words() []string {
	return append([]string(nil), it.words...)
}

// Iterate returns an iter.Seq[string] that yields the 2048 candidate
// mnemonics produced by substituting every word from the iterator's
// wordlist into the given position of the template. The other 11 words
// remain fixed.
//
// Note: not every yielded mnemonic will have a valid checksum — that is the
// consumer's job to check.
func (it *Iterator) Iterate(template []string, pos int) (iter.Seq[string], error) {
	tmpl, err := it.validateTemplate(template)
	if err != nil {
		return nil, err
	}
	if pos < 0 || pos >= templateLength {
		return nil, ErrInvalidPosition
	}

	return func(yield func(string) bool) {
		// Local copy so concurrent iterators don't stomp on each other.
		buf := append([]string(nil), tmpl...)
		for _, w := range it.words {
			buf[pos] = w
			if !yield(strings.Join(buf, " ")) {
				return
			}
		}
	}, nil
}

// CandidateAt returns the mnemonic that the iterator returned by Iterate
// would yield at index i (0..2047) for the given template/pos. It is the
// resume primitive: pass the resume index and pick up from there.
func (it *Iterator) CandidateAt(template []string, pos, i int) (string, error) {
	tmpl, err := it.validateTemplate(template)
	if err != nil {
		return "", err
	}
	if pos < 0 || pos >= templateLength {
		return "", ErrInvalidPosition
	}
	if i < 0 || i >= wordlistSize {
		return "", ErrInvalidPosition
	}

	buf := append([]string(nil), tmpl...)
	buf[pos] = it.words[i]
	return strings.Join(buf, " "), nil
}

// validateTemplate enforces the 12-word length and that every word belongs
// to the iterator's wordlist. It returns a defensive copy of the template
// so callers cannot mutate the iterator's state after construction.
func (it *Iterator) validateTemplate(template []string) ([]string, error) {
	if len(template) != templateLength {
		return nil, ErrInvalidTemplate
	}
	for _, w := range template {
		if _, ok := it.wordSet[w]; !ok {
			return nil, ErrInvalidTemplate
		}
	}
	return append([]string(nil), template...), nil
}
