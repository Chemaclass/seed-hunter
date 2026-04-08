package config_test

import (
	"strings"
	"testing"

	"github.com/Chemaclass/seed-hunter/config"
)

func TestParsePositionsAcceptsSinglePosition(t *testing.T) {
	got, err := config.ParsePositions("5")
	if err != nil {
		t.Fatalf("ParsePositions: %v", err)
	}
	if len(got) != 1 || got[0] != 5 {
		t.Errorf("want [5], got %v", got)
	}
}

func TestParsePositionsAcceptsFullRange(t *testing.T) {
	got, err := config.ParsePositions("0-11")
	if err != nil {
		t.Fatalf("ParsePositions: %v", err)
	}
	want := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if !equalInts(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestParsePositionsAcceptsPartialRange(t *testing.T) {
	got, err := config.ParsePositions("3-7")
	if err != nil {
		t.Fatalf("ParsePositions: %v", err)
	}
	want := []int{3, 4, 5, 6, 7}
	if !equalInts(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestParsePositionsAcceptsCommaList(t *testing.T) {
	got, err := config.ParsePositions("0,3,7")
	if err != nil {
		t.Fatalf("ParsePositions: %v", err)
	}
	want := []int{0, 3, 7}
	if !equalInts(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestParsePositionsAcceptsMixedRangesAndLists(t *testing.T) {
	got, err := config.ParsePositions("0,3-5,9")
	if err != nil {
		t.Fatalf("ParsePositions: %v", err)
	}
	want := []int{0, 3, 4, 5, 9}
	if !equalInts(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestParsePositionsPreservesUserOrder(t *testing.T) {
	got, err := config.ParsePositions("7,3,11")
	if err != nil {
		t.Fatalf("ParsePositions: %v", err)
	}
	want := []int{7, 3, 11}
	if !equalInts(got, want) {
		t.Errorf("user order must be preserved: want %v, got %v", want, got)
	}
}

func TestParsePositionsRejectsEmpty(t *testing.T) {
	if _, err := config.ParsePositions(""); err == nil {
		t.Error("expected error for empty spec")
	}
	if _, err := config.ParsePositions("   "); err == nil {
		t.Error("expected error for whitespace-only spec")
	}
}

func TestParsePositionsRejectsOutOfRange(t *testing.T) {
	cases := []string{"-1", "12", "99", "0-12", "-1-5"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := config.ParsePositions(c)
			if err == nil {
				t.Errorf("expected error for %q", c)
			}
		})
	}
}

func TestParsePositionsRejectsDuplicates(t *testing.T) {
	cases := []string{"0,0", "3,5,3", "0-5,3"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := config.ParsePositions(c)
			if err == nil {
				t.Errorf("expected error for duplicate in %q", c)
				return
			}
			if !strings.Contains(err.Error(), "duplicate") {
				t.Errorf("error should mention duplicate, got: %v", err)
			}
		})
	}
}

func TestParsePositionsRejectsMalformedTokens(t *testing.T) {
	cases := []string{"abc", "1,abc", "1-abc", "1--3", "1,,2"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if _, err := config.ParsePositions(c); err == nil {
				t.Errorf("expected error for malformed %q", c)
			}
		})
	}
}

func TestParsePositionsRejectsBackwardsRange(t *testing.T) {
	if _, err := config.ParsePositions("7-3"); err == nil {
		t.Error("expected error for backwards range")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
