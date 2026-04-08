package pipeline_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Chemaclass/seed-hunter/internal/pipeline"
)

func TestCursorStringIsZeroCommaSeparated(t *testing.T) {
	var c pipeline.Cursor
	got := c.String()
	want := "0,0,0,0,0,0,0,0,0,0,0,0"
	if got != want {
		t.Errorf("zero cursor: want %q, got %q", want, got)
	}
}

func TestParseCursorEmptyReturnsZero(t *testing.T) {
	c, err := pipeline.ParseCursor("")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	for i, v := range c {
		if v != 0 {
			t.Errorf("cursor[%d] = %d, want 0", i, v)
		}
	}
}

func TestParseCursorRoundTrip(t *testing.T) {
	in := "0,1,2,3,4,5,6,7,8,9,10,2047"
	c, err := pipeline.ParseCursor(in)
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	if got := c.String(); got != in {
		t.Errorf("round trip: want %q, got %q", in, got)
	}
}

func TestParseCursorRejectsBadInput(t *testing.T) {
	cases := []string{
		"1,2,3",                         // too few
		"1,2,3,4,5,6,7,8,9,10,11,12,13", // too many
		"a,b,c,d,e,f,g,h,i,j,k,l",       // not numbers
		"-1,0,0,0,0,0,0,0,0,0,0,0",      // out of range low
		"2048,0,0,0,0,0,0,0,0,0,0,0",    // out of range high
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := pipeline.ParseCursor(in); err == nil {
				t.Errorf("expected error for %q", in)
			}
		})
	}
}

func TestCursorIncRightmostFirst(t *testing.T) {
	var c pipeline.Cursor
	overflow := c.Inc()
	if overflow {
		t.Fatal("zero cursor inc should not overflow")
	}
	if c[11] != 1 {
		t.Errorf("expected position 11 to advance, got cursor=%v", c)
	}
	for i := 0; i < 11; i++ {
		if c[i] != 0 {
			t.Errorf("expected position %d to stay 0, got %d", i, c[i])
		}
	}
}

func TestCursorIncCarriesAcrossSlots(t *testing.T) {
	var c pipeline.Cursor
	c[11] = 2047 // about to roll over
	overflow := c.Inc()
	if overflow {
		t.Fatal("should not overflow yet")
	}
	if c[11] != 0 {
		t.Errorf("expected position 11 to wrap to 0, got %d", c[11])
	}
	if c[10] != 1 {
		t.Errorf("expected position 10 to carry to 1, got %d", c[10])
	}
}

func TestCursorIncMultipleCarries(t *testing.T) {
	c := pipeline.Cursor{0, 0, 0, 0, 0, 0, 0, 0, 0, 2047, 2047, 2047}
	overflow := c.Inc()
	if overflow {
		t.Fatal("should not overflow yet")
	}
	want := pipeline.Cursor{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0}
	if c != want {
		t.Errorf("multi-carry: want %v, got %v", want, c)
	}
}

func TestCursorIncOverflowsAtEndOfKeyspace(t *testing.T) {
	c := pipeline.Cursor{2047, 2047, 2047, 2047, 2047, 2047, 2047, 2047, 2047, 2047, 2047, 2047}
	overflow := c.Inc()
	if !overflow {
		t.Error("expected overflow at end of keyspace")
	}
	for i, v := range c {
		if v != 0 {
			t.Errorf("after overflow, cursor[%d] should be 0, got %d", i, v)
		}
	}
}

func TestCursorMnemonicBuildsFromWordlist(t *testing.T) {
	// Build a fake 2048-word list of "wN" strings.
	words := make([]string, pipeline.CursorBase)
	for i := range words {
		words[i] = "w" + itoa(i)
	}
	c := pipeline.Cursor{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	m, err := c.Mnemonic(words)
	if err != nil {
		t.Fatalf("Mnemonic: %v", err)
	}
	want := "w0 w1 w2 w3 w4 w5 w6 w7 w8 w9 w10 w11"
	if m != want {
		t.Errorf("Mnemonic: want %q, got %q", want, m)
	}
}

func TestCursorMnemonicRejectsWrongSizedWordlist(t *testing.T) {
	var c pipeline.Cursor
	_, err := c.Mnemonic([]string{"only", "three", "words"})
	if err == nil {
		t.Fatal("expected error for short wordlist")
	}
	if !strings.Contains(err.Error(), "2048") {
		t.Errorf("error should mention 2048, got: %v", err)
	}
}

func TestErrKeyspaceExhaustedExists(t *testing.T) {
	if !errors.Is(pipeline.ErrKeyspaceExhausted, pipeline.ErrKeyspaceExhausted) {
		t.Error("ErrKeyspaceExhausted should be a sentinel")
	}
}

// itoa is a tiny stdlib-free integer-to-string used by the wordlist
// fake. Avoids importing strconv just for this helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(b[pos:])
}
