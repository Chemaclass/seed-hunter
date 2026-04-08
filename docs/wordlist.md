# Wordlist

The 2048 candidate words are loaded from a file at startup, not pulled
from a hard-coded library.

## Default: embedded English

By default `seed-hunter` uses the canonical English BIP-39 wordlist that
ships **embedded** in the binary at build time
(`internal/wordlist/english.txt`, byte-for-byte identical to
[bitcoin/bips/bip-0039/english.txt](https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt)).
No file lookup is needed for the default; the binary works on a system
that doesn't have any wordlist files at all.

## Using a different language

Point `--wordlist` (or `SEEDHUNTER_WORDLIST`) at one of the other official
BIP-39 wordlist files from the
[bitcoin/bips repository](https://github.com/bitcoin/bips/tree/master/bip-0039):

```sh
curl -fsSL https://raw.githubusercontent.com/bitcoin/bips/master/bip-0039/spanish.txt -o spanish.txt
./seed-hunter run --wordlist ./spanish.txt --template "..."
```

When you supply `--wordlist`, `seed-hunter` also rebinds the underlying
BIP-39 library so that checksum validation and PBKDF2 seed derivation use
the same words — the iterator and the deriver are always in sync. The
file must contain exactly **2048 unique non-empty lines** (UTF-8, one
word per line). Anything else is rejected at startup with a clear error.

## Custom (non-BIP-39) wordlists

A custom wordlist that is not one of the 9 official BIP-39 lists will
still load, but **every candidate will fail the BIP-39 checksum** and no
mnemonic will reach the balance check. That's only useful as a "show me
how the iterator walks 2048 candidates" demo.

## Persistence

The path you pass to `--wordlist` is persisted on the session row, so
`seed-hunter run` (no flags) will reuse the same wordlist on resume. If
you want to switch languages mid-experiment, pass `--wordlist <new path>`
explicitly to override the inherited value.
