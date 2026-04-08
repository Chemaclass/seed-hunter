// Package config holds the user-facing configuration for a seed-hunter run.
//
// Config is intentionally a plain struct: cobra populates it from flags and
// environment variables in cmd/, while internal/pipeline consumes only the
// fields it needs. Validation lives here so the same rules apply to every
// invocation.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Default values reused by both flag wiring and tests.
const (
	DefaultDBPath     = "./seed-hunter.db"
	DefaultPositions  = "0-11" // sweep all 12 positions sequentially
	DefaultNAddresses = 1
	DefaultAPI        = "mempool"
	DefaultScriptType = "segwit"
	DefaultRate       = 2.0
	DefaultWorkers    = 2 // parallel deriver goroutines
	DefaultAPIWorkers = 1
	DefaultBatchSize  = 50
)

// Config is the validated user-supplied configuration for one CLI invocation.
type Config struct {
	DBPath       string
	WordlistPath string // "" means use the embedded English BIP-39 default
	Template     string // raw flag value; "" means generate one
	Positions    string // raw --positions value; e.g. "0-11", "5", "0,3,7"
	NAddresses   int
	API          string
	ScriptType   string
	Rate         float64
	Workers      int // parallel deriver goroutines
	APIWorkers   int // currently always 1 (rate limiter serializes upstream)
	BatchSize    int
	Reset        bool // ignore the most-recent paused session and start fresh
	NoDashboard  bool
}

// Default returns a Config populated with the package defaults.
func Default() Config {
	return Config{
		DBPath:     DefaultDBPath,
		Positions:  DefaultPositions,
		NAddresses: DefaultNAddresses,
		API:        DefaultAPI,
		ScriptType: DefaultScriptType,
		Rate:       DefaultRate,
		Workers:    DefaultWorkers,
		APIWorkers: DefaultAPIWorkers,
		BatchSize:  DefaultBatchSize,
	}
}

// ApplyEnv overlays env-var fallbacks (SEEDHUNTER_*) for any field that the
// caller wants to support via the environment. Flags always win — call this
// BEFORE flag parsing so flag values can override env values.
func (c *Config) ApplyEnv() {
	if v, ok := os.LookupEnv("SEEDHUNTER_DB"); ok {
		c.DBPath = v
	}
	if v, ok := os.LookupEnv("SEEDHUNTER_WORDLIST"); ok {
		c.WordlistPath = v
	}
	if v, ok := os.LookupEnv("SEEDHUNTER_TEMPLATE"); ok {
		c.Template = v
	}
	if v, ok := os.LookupEnv("SEEDHUNTER_API"); ok {
		c.API = v
	}
	if v, ok := os.LookupEnv("SEEDHUNTER_SCRIPT_TYPE"); ok {
		c.ScriptType = v
	}
}

// ValidScriptTypes lists every accepted --script-type value.
var ValidScriptTypes = []string{"segwit", "legacy"}

// ValidAPIs lists every accepted --api value.
var ValidAPIs = []string{"mempool", "blockstream"}

// Validate enforces the cross-cutting invariants on c. It does NOT validate
// that template words appear in the BIP-39 wordlist — the iterator does
// that and gives a clearer error. The --positions spec is parsed and
// range-checked here too: every value must be in [0,11] and the spec must
// resolve to at least one position.
func (c Config) Validate() error {
	if _, err := ParsePositions(c.Positions); err != nil {
		return fmt.Errorf("positions: %w", err)
	}
	if c.NAddresses < 1 {
		return fmt.Errorf("addresses must be >= 1, got %d", c.NAddresses)
	}
	if c.Rate <= 0 {
		return fmt.Errorf("rate must be > 0, got %g", c.Rate)
	}
	if c.BatchSize < 1 {
		return fmt.Errorf("batch-size must be >= 1, got %d", c.BatchSize)
	}
	if c.Workers < 1 {
		return fmt.Errorf("workers must be >= 1, got %d", c.Workers)
	}
	if !contains(ValidAPIs, c.API) {
		return fmt.Errorf("api must be one of %v, got %q", ValidAPIs, c.API)
	}
	if !contains(ValidScriptTypes, c.ScriptType) {
		return fmt.Errorf("script-type must be one of %v, got %q", ValidScriptTypes, c.ScriptType)
	}
	if c.Template != "" {
		words := strings.Fields(c.Template)
		if len(words) != 12 {
			return fmt.Errorf("template must be 12 words, got %d", len(words))
		}
	}
	if c.DBPath == "" {
		return errors.New("db path must not be empty")
	}
	if c.WordlistPath != "" {
		info, err := os.Stat(c.WordlistPath)
		if err != nil {
			return fmt.Errorf("wordlist path %q: %w", c.WordlistPath, err)
		}
		if info.IsDir() {
			return fmt.Errorf("wordlist path %q is a directory, expected a file", c.WordlistPath)
		}
	}
	return nil
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}
