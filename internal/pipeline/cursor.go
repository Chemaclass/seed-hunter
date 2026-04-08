package pipeline

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CursorLength is the number of word slots in a 12-word BIP-39 mnemonic.
// This is also the length of a Cursor.
const CursorLength = 12

// CursorBase is the size of the BIP-39 wordlist (and therefore the base of
// the keyspace odometer).
const CursorBase = 2048

// Cursor is the iteration state of the full-keyspace walk: a 12-element
// array of word indices, each in [0, 2048). It is incremented like a
// base-2048 odometer (the rightmost slot advances fastest).
//
// The full keyspace is 2048^12 ≈ 5.4 × 10^39 cursors. Position 11 advances
// every iteration, position 10 every 2048 iterations, position 9 every
// 2048² iterations, ..., position 0 every 2048¹¹ ≈ 2.4 × 10³⁶ iterations.
// In other words: position 0 will not advance even once in the lifetime of
// the universe at any realistic rate. That is the educational point.
type Cursor [CursorLength]int

// String serialises the cursor as a comma-separated decimal list, e.g.
// "0,0,0,0,0,0,0,0,0,0,5,123". Used for SQLite persistence and dashboard
// display.
func (c Cursor) String() string {
	parts := make([]string, CursorLength)
	for i, v := range c {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// ParseCursor parses a comma-separated string into a Cursor. The empty
// string returns Cursor{} (all zeros).
func ParseCursor(s string) (Cursor, error) {
	var c Cursor
	s = strings.TrimSpace(s)
	if s == "" {
		return c, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != CursorLength {
		return c, fmt.Errorf("cursor: want %d parts, got %d", CursorLength, len(parts))
	}
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return c, fmt.Errorf("cursor[%d]: %w", i, err)
		}
		if v < 0 || v >= CursorBase {
			return c, fmt.Errorf("cursor[%d]=%d out of range [0,%d)", i, v, CursorBase)
		}
		c[i] = v
	}
	return c, nil
}

// Inc advances the cursor by one (rightmost slot fastest). Returns true
// if the increment overflowed past the end of the keyspace, in which case
// the cursor wraps to all zeros — i.e. the full 2048^12 walk is complete.
//
// (You will never observe this. The math says you will run out of universe
// long before the cursor overflows.)
func (c *Cursor) Inc() (overflowed bool) {
	for i := CursorLength - 1; i >= 0; i-- {
		c[i]++
		if c[i] < CursorBase {
			return false
		}
		c[i] = 0
	}
	return true
}

// Mnemonic builds the 12-word mnemonic for this cursor by indexing into
// words at each slot. words must contain exactly CursorBase entries (the
// loaded BIP-39 wordlist).
func (c Cursor) Mnemonic(words []string) (string, error) {
	if len(words) != CursorBase {
		return "", fmt.Errorf("wordlist must have %d entries, got %d", CursorBase, len(words))
	}
	parts := make([]string, CursorLength)
	for i, idx := range c {
		parts[i] = words[idx]
	}
	return strings.Join(parts, " "), nil
}

// ErrKeyspaceExhausted is returned by Walk when the cursor overflows past
// the end of the 2048^12 keyspace. You will not see this error.
var ErrKeyspaceExhausted = errors.New("pipeline: full keyspace exhausted (impossible)")
