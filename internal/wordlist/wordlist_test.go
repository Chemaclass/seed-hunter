package wordlist_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chemaclass/seed-hunter/internal/wordlist"
)

func TestDefaultReturnsExactly2048Words(t *testing.T) {
	got := wordlist.Default()
	if len(got) != wordlist.Size {
		t.Fatalf("Default(): want %d words, got %d", wordlist.Size, len(got))
	}
}

func TestDefaultStartsWithAbandonAndEndsWithZoo(t *testing.T) {
	got := wordlist.Default()
	if got[0] != "abandon" {
		t.Errorf("first word: want %q, got %q", "abandon", got[0])
	}
	if got[len(got)-1] != "zoo" {
		t.Errorf("last word: want %q, got %q", "zoo", got[len(got)-1])
	}
}

func TestDefaultIsADefensiveCopy(t *testing.T) {
	first := wordlist.Default()
	first[0] = "MUTATED"
	second := wordlist.Default()
	if second[0] != "abandon" {
		t.Errorf("Default() must return a fresh copy each call; got mutation leak: %q", second[0])
	}
}

func TestDefaultHasNoDuplicates(t *testing.T) {
	if err := wordlist.Validate(wordlist.Default()); err != nil {
		t.Errorf("embedded default failed validation: %v", err)
	}
}

func TestLoadEmptyPathReturnsEmbeddedDefault(t *testing.T) {
	got, err := wordlist.Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if len(got) != wordlist.Size {
		t.Errorf("len: want %d, got %d", wordlist.Size, len(got))
	}
	if got[0] != "abandon" || got[len(got)-1] != "zoo" {
		t.Errorf("Load(\"\") must return the canonical default")
	}
}

func TestLoadFromFileSucceedsForValidWordlist(t *testing.T) {
	path := writeTestFile(t, generateWords(t, wordlist.Size))
	got, err := wordlist.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != wordlist.Size {
		t.Errorf("len: want %d, got %d", wordlist.Size, len(got))
	}
}

func TestLoadFromMissingFileReturnsError(t *testing.T) {
	_, err := wordlist.Load("/nonexistent/path/to/wordlist.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadRejectsWordlistOfWrongLength(t *testing.T) {
	cases := map[string]int{
		"too short": 100,
		"one short": wordlist.Size - 1,
		"one long":  wordlist.Size + 1,
	}
	for name, n := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTestFile(t, generateWords(t, n))
			_, err := wordlist.Load(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, wordlist.ErrWrongLength) {
				t.Errorf("expected ErrWrongLength, got %v", err)
			}
		})
	}
}

func TestLoadRejectsWordlistWithDuplicates(t *testing.T) {
	words := generateWords(t, wordlist.Size)
	words[100] = words[200] // inject a duplicate
	path := writeTestFile(t, words)
	_, err := wordlist.Load(path)
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
	if !errors.Is(err, wordlist.ErrDuplicateWord) {
		t.Errorf("expected ErrDuplicateWord, got %v", err)
	}
}

func TestLoadRejectsWordlistWithEmptyLine(t *testing.T) {
	words := generateWords(t, wordlist.Size)
	words[42] = ""
	path := writeTestFile(t, words)
	_, err := wordlist.Load(path)
	if err == nil {
		t.Fatal("expected error for empty line")
	}
	if !errors.Is(err, wordlist.ErrEmptyLine) {
		t.Errorf("expected ErrEmptyLine, got %v", err)
	}
}

func TestLoadAcceptsTrailingNewline(t *testing.T) {
	// Generate a file that ends with one newline (the canonical UNIX shape).
	words := generateWords(t, wordlist.Size)
	dir := t.TempDir()
	path := filepath.Join(dir, "words.txt")
	body := strings.Join(words, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := wordlist.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != wordlist.Size {
		t.Errorf("trailing newline must not be counted as a word")
	}
}

// generateWords returns n unique placeholder words. They are NOT BIP-39
// words — that's intentional, the wordlist package only validates shape, not
// content.
func generateWords(t *testing.T, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		out[i] = "word" + itoa(i)
	}
	return out
}

func writeTestFile(t *testing.T, words []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte(strings.Join(words, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// itoa avoids importing strconv just for one helper.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
