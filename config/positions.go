package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParsePositions turns a --positions spec string into the ordered list of
// word positions to sweep. Accepted shapes:
//
//	"5"        → [5]
//	"0-11"     → [0,1,2,3,4,5,6,7,8,9,10,11]
//	"3-7"      → [3,4,5,6,7]
//	"0,3,7"    → [0,3,7]
//	"0,3-5,9"  → [0,3,4,5,9]
//
// Whitespace around tokens is ignored. Every parsed value must be in
// [0,11]; ranges may be inclusive or single-element. The result preserves
// the order the user specified (so "7,3" yields [7,3], not [3,7]) and
// rejects duplicates so the cmd outer loop never visits the same position
// twice.
//
// Returns an error for empty input, malformed tokens, out-of-range values,
// or duplicates.
func ParsePositions(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty positions spec")
	}

	var out []int
	seen := make(map[int]struct{}, 12)
	add := func(p int) error {
		if p < 0 || p > 11 {
			return fmt.Errorf("position out of range [0,11]: %d", p)
		}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("duplicate position: %d", p)
		}
		seen[p] = struct{}{}
		out = append(out, p)
		return nil
	}

	for _, raw := range strings.Split(spec, ",") {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return nil, fmt.Errorf("empty token in positions spec %q", spec)
		}
		if strings.Contains(tok, "-") {
			parts := strings.SplitN(tok, "-", 2)
			lo, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid range start in %q: %w", tok, err)
			}
			hi, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid range end in %q: %w", tok, err)
			}
			if lo > hi {
				return nil, fmt.Errorf("range start %d > end %d in %q", lo, hi, tok)
			}
			for p := lo; p <= hi; p++ {
				if err := add(p); err != nil {
					return nil, err
				}
			}
		} else {
			p, err := strconv.Atoi(tok)
			if err != nil {
				return nil, fmt.Errorf("invalid position %q: %w", tok, err)
			}
			if err := add(p); err != nil {
				return nil, err
			}
		}
	}

	return out, nil
}
