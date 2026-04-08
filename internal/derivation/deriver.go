// Package derivation turns BIP-39 mnemonics into Bitcoin mainnet receiving
// addresses following BIP-44 (legacy P2PKH) or BIP-84 (native SegWit P2WPKH).
package derivation

import (
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/tyler-smith/go-bip39"
)

// ScriptType selects the derivation path and address encoding.
type ScriptType string

const (
	// ScriptLegacy uses BIP-44 (m/44'/0'/0'/0/i) and P2PKH encoding (1...).
	ScriptLegacy ScriptType = "legacy"
	// ScriptSegwit uses BIP-84 (m/84'/0'/0'/0/i) and P2WPKH encoding (bc1...).
	ScriptSegwit ScriptType = "segwit"
)

// Sentinel errors returned by Derive.
var (
	ErrInvalidMnemonic = errors.New("derivation: invalid mnemonic")
	ErrInvalidScript   = errors.New("derivation: invalid script type")
	ErrInvalidCount    = errors.New("derivation: address count must be >= 1")
)

// Deriver derives Bitcoin mainnet receiving addresses from a BIP-39 mnemonic.
// It is safe for concurrent use: it holds no mutable state.
type Deriver struct{}

// New returns a Deriver.
func New() *Deriver {
	return &Deriver{}
}

// Derive returns the first n mainnet receiving addresses (the m/.../0/0..n-1
// branch) for the given mnemonic and script type. The mnemonic must pass
// BIP-39 checksum validation; if not, ErrInvalidMnemonic is returned. n must
// be >= 1.
//
// Derivation paths:
//
//	ScriptLegacy: m/44'/0'/0'/0/i  -> P2PKH  (1...)
//	ScriptSegwit: m/84'/0'/0'/0/i  -> P2WPKH (bc1...)
func (d *Deriver) Derive(mnemonic string, n int, scriptType ScriptType) ([]string, error) {
	if n < 1 {
		return nil, ErrInvalidCount
	}
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, ErrInvalidMnemonic
	}

	var purposeIndex uint32
	switch scriptType {
	case ScriptLegacy:
		purposeIndex = 44
	case ScriptSegwit:
		purposeIndex = 84
	default:
		return nil, ErrInvalidScript
	}

	seed := bip39.NewSeed(mnemonic, "")

	params := &chaincfg.MainNetParams
	master, err := hdkeychain.NewMaster(seed, params)
	if err != nil {
		return nil, fmt.Errorf("derivation: create master key: %w", err)
	}

	// m / purpose'
	purpose, err := master.Derive(hdkeychain.HardenedKeyStart + purposeIndex)
	if err != nil {
		return nil, fmt.Errorf("derivation: derive purpose: %w", err)
	}
	// m / purpose' / 0' (coin type = Bitcoin)
	coin, err := purpose.Derive(hdkeychain.HardenedKeyStart + 0)
	if err != nil {
		return nil, fmt.Errorf("derivation: derive coin type: %w", err)
	}
	// m / purpose' / 0' / 0' (account 0)
	account, err := coin.Derive(hdkeychain.HardenedKeyStart + 0)
	if err != nil {
		return nil, fmt.Errorf("derivation: derive account: %w", err)
	}
	// m / purpose' / 0' / 0' / 0 (external / receiving chain)
	change, err := account.Derive(0)
	if err != nil {
		return nil, fmt.Errorf("derivation: derive change: %w", err)
	}

	addresses := make([]string, 0, n)
	for i := 0; i < n; i++ {
		child, err := change.Derive(uint32(i))
		if err != nil {
			return nil, fmt.Errorf("derivation: derive child %d: %w", i, err)
		}

		pubKey, err := child.ECPubKey()
		if err != nil {
			return nil, fmt.Errorf("derivation: extract pubkey %d: %w", i, err)
		}
		pubKeyHash := btcutil.Hash160(pubKey.SerializeCompressed())

		var encoded string
		switch scriptType {
		case ScriptSegwit:
			addr, err := btcutil.NewAddressWitnessPubKeyHash(pubKeyHash, params)
			if err != nil {
				return nil, fmt.Errorf("derivation: build p2wpkh address %d: %w", i, err)
			}
			encoded = addr.EncodeAddress()
		case ScriptLegacy:
			addr, err := btcutil.NewAddressPubKeyHash(pubKeyHash, params)
			if err != nil {
				return nil, fmt.Errorf("derivation: build p2pkh address %d: %w", i, err)
			}
			encoded = addr.EncodeAddress()
		default:
			// Unreachable: validated above.
			return nil, ErrInvalidScript
		}

		addresses = append(addresses, encoded)
	}

	return addresses, nil
}
