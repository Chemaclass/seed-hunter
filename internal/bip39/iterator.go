// Package bip39 contains helpers for iterating BIP-39 mnemonic candidates and
// for fingerprinting mnemonics so they can be logged without persisting the
// plaintext words.
package bip39

import (
	"errors"
	"iter"
	"strings"

	extbip39 "github.com/tyler-smith/go-bip39"
)

const (
	templateLength = 12
	wordlistSize   = 2048
)

// ErrInvalidTemplate is returned when the supplied template is not exactly
// 12 words long or contains a word outside the BIP-39 English wordlist.
var ErrInvalidTemplate = errors.New("bip39: invalid template")

// ErrInvalidPosition is returned when the supplied position (or candidate
// index) is outside the valid range.
var ErrInvalidPosition = errors.New("bip39: position out of range")

// Iterate returns an iter.Seq[string] that yields the 2048 candidate
// mnemonics produced by substituting every BIP-39 word into the given
// position of the template. The other 11 words remain fixed.
//
// Note: not every yielded mnemonic will have a valid checksum — that is the
// consumer's job to check.
func Iterate(template []string, pos int) (iter.Seq[string], error) {
	tmpl, err := validateTemplate(template)
	if err != nil {
		return nil, err
	}
	if pos < 0 || pos >= templateLength {
		return nil, ErrInvalidPosition
	}

	words := extbip39.GetWordList()

	return func(yield func(string) bool) {
		// Local copy so concurrent iterators don't stomp on each other.
		buf := append([]string(nil), tmpl...)
		for _, w := range words {
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
func CandidateAt(template []string, pos, i int) (string, error) {
	tmpl, err := validateTemplate(template)
	if err != nil {
		return "", err
	}
	if pos < 0 || pos >= templateLength {
		return "", ErrInvalidPosition
	}
	if i < 0 || i >= wordlistSize {
		return "", ErrInvalidPosition
	}

	words := extbip39.GetWordList()
	buf := append([]string(nil), tmpl...)
	buf[pos] = words[i]
	return strings.Join(buf, " "), nil
}

// validateTemplate enforces the 12-word length and that every word belongs
// to the BIP-39 English wordlist. It returns a defensive copy of the
// template so callers cannot mutate the iterator's state after construction.
func validateTemplate(template []string) ([]string, error) {
	if len(template) != templateLength {
		return nil, ErrInvalidTemplate
	}

	wordset := wordSet()
	for _, w := range template {
		if _, ok := wordset[w]; !ok {
			return nil, ErrInvalidTemplate
		}
	}

	return append([]string(nil), template...), nil
}

func wordSet() map[string]struct{} {
	words := extbip39.GetWordList()
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		set[w] = struct{}{}
	}
	return set
}
