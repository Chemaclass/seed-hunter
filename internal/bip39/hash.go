package bip39

import (
	"crypto/sha256"
	"encoding/hex"
)

// Fingerprint returns the lowercase hex-encoded SHA-256 digest of the given
// mnemonic. It is deterministic and one-way; the seed-hunter pipeline uses
// it to log attempts to SQLite without persisting plaintext mnemonics.
func Fingerprint(mnemonic string) string {
	sum := sha256.Sum256([]byte(mnemonic))
	return hex.EncodeToString(sum[:])
}
