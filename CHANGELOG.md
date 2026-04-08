# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-04-08

The first stable release of `seed-hunter` — an educational Go CLI that
demonstrates the impossibility of brute-forcing a 12-word BIP-39 seed
phrase by actually trying.

### Added

#### Core pipeline

- **Channel-based pipeline** with five stages: generator → parallel
  derivers → reorder buffer → rate-limited checker → SQLite logger.
  Each stage runs in its own goroutine and honors a shared
  `context.Context` so `Ctrl+C` is always graceful.
- **`--workers N`** flag (default `2`) for parallel BIP-32/44/84
  derivation. A reorder goroutine keeps the SQLite checkpoint
  contiguous so resume-after-cancel is correct under any worker count.
- **`BenchmarkRunWorkers`** built-in benchmark that times one full
  2048-candidate pipeline pass at several worker counts so users can
  pick the optimal value for their hardware. See
  [`docs/performance.md`](docs/performance.md).
- **Sweep mode** (`--positions`) auto-advances through every BIP-39
  word position sequentially. Default `0-11` covers all 12 positions
  (12 × 2048 = 24,576 candidates). Custom specs supported:
  `--positions 5`, `--positions 0-11`, `--positions 0,3,7`,
  `--positions 0,3-5,9`.
- **Walk mode** — after the sweep finishes, `seed-hunter run`
  automatically transitions into a full 2048¹² keyspace walk that
  iterates the entire keyspace cursor-by-cursor, forever, until
  `Ctrl+C`. The walker is single-goroutine (the rate limiter dominates)
  and persists its 12-int cursor on every batch flush. The dashboard
  shows the live cursor and the literal "ETA full key" line in years.
- **`--no-walk`** to stop after the sweep, **`--skip-sweep`** to go
  straight into walk mode.

#### CLI and resume UX

- **`seed-hunter run`** — start (or resume) the brute-force loop. With
  no flags, reads the most recent paused session from SQLite and
  resumes it with all of its parameters intact (template, positions,
  addresses, api, script type, rate, wordlist, workers, mode, cursor).
  Any flag you pass overrides the corresponding inherited value.
- **`seed-hunter run --reset`** — ignore the most recent paused
  session and start a brand-new one.
- **`seed-hunter stats`** — print aggregate counters from the SQLite
  database, with optional `--session ID` for per-session breakdown.
- **`seed-hunter reset --yes`** — truncate `attempts` and `sessions`.
- **`seed-hunter version`** and **`seed-hunter --version`** — print the
  version, git commit, build date, Go version, and target platform.
- Live ANSI dashboard with two render modes (sweep / walk), repainted
  every 200ms via atomic counters.

#### Storage

- **SQLite repository** powered by the pure-Go
  [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) driver
  (no CGO, no C toolchain needed).
- **`sessions` table** stores everything needed to resume a run:
  template, position, addresses, api, script type, rate, wordlist
  path, workers, positions spec, mode (`sweep` or `walk`), and the
  walker cursor.
- **`attempts` table** records every candidate's SHA-256 fingerprint,
  derived addresses (JSON), balance, duration, and timestamp.
- **Forward-only `ALTER TABLE` migration** in `storage.Open` so older
  databases auto-upgrade with zero user intervention.
- **Resume helpers**: `LatestResumable`, `MarkPausedAsCompleted`,
  `Checkpoint`, `CheckpointCursor`, `BeginSession`, `EndSession`.

#### Address derivation

- **BIP-32 / BIP-44 (legacy P2PKH `1...`)** and **BIP-84 (native
  SegWit P2WPKH `bc1...`)** mainnet receiving address derivation,
  validated against the canonical BIP-39 test vector.
- Built on `btcsuite/btcd/btcutil/hdkeychain` and
  `tyler-smith/go-bip39`.

#### Balance checker

- **mempool.space** and **blockstream.info** Esplora-compatible
  clients sharing a unified JSON parser.
- **Token-bucket rate limiter** wrapper (`golang.org/x/time/rate`) so
  `--rate N` is honored across all goroutines.
- Sentinel errors `ErrRateLimited`, `ErrUnexpected`,
  `ErrUnknownProvider` for `errors.Is`-based handling.

#### Wordlist

- **Embedded English BIP-39 wordlist** at
  `internal/wordlist/english.txt`, byte-for-byte identical to
  [bitcoin/bips/bip-0039/english.txt](https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt).
  No file lookup needed for the default; the binary works on a system
  with no wordlist files at all.
- **`--wordlist PATH`** flag to load a different list (any of the
  9 official BIP-39 languages, or a custom 2048-line file). Validates
  shape (exactly 2048 unique non-empty UTF-8 lines) and rebinds the
  underlying BIP-39 library so the iterator and the deriver agree.

#### Configuration

- **Flag inheritance** from the most recent paused session, with
  per-flag override via `pflag.FlagSet.Changed`. Type `seed-hunter run`
  forever; the SQLite database is the source of truth.
- **`SEEDHUNTER_*` environment-variable fallbacks** for the most
  common settings (`DB`, `WORDLIST`, `TEMPLATE`, `API`, `SCRIPT_TYPE`).
- **`--positions` parser** with range and list syntax, validated for
  duplicates and out-of-range values at startup.

#### Documentation

- Top-level **README** structured as Why → How → What → links.
- **`docs/math.md`** — the impossibility math, comparisons, BIP-39
  passphrase, multisig.
- **`docs/architecture.md`** — pipeline diagram (sweep + walk),
  package responsibilities, schema notes, cancellation.
- **`docs/configuration.md`** — full flag and env-var reference,
  positions spec, resume semantics.
- **`docs/performance.md`** — `--workers` benchmark and tuning guide.
- **`docs/wordlist.md`** — wordlist source and how to use a different
  language.
- **`docs/privacy.md`** — what's stored in plaintext (the in-flight
  template), what isn't (per-attempt mnemonics), and how to opt out.
- **`CONTRIBUTING.md`** — open-source contributor guide with explicit
  in/out-of-scope lists.

#### Tests and CI

- **~50 behavioural tests** across 9 packages: `cmd`, `config`,
  `internal/bip39`, `internal/checker`, `internal/dashboard`,
  `internal/derivation`, `internal/pipeline`, `internal/storage`,
  `internal/wordlist`.
- **`go test -race -count=1 ./...`** is green; **`golangci-lint v2`**
  reports zero issues.
- **GitHub Actions CI** runs lint and tests on every push and pull
  request against Go 1.26.

### Privacy

- The high-volume `attempts` table never stores plaintext mnemonics —
  only SHA-256 hex fingerprints in the `mnemonic_hash` column.
- The much smaller `sessions` table stores the in-flight template in
  plaintext (column `template`) so resume can reload it without flags.
  See [`docs/privacy.md`](docs/privacy.md) for opt-outs.

### Notes

- This project is **strictly educational**. The 12 × 2048 = 24,576
  sweep finishes in minutes; the full 2048¹² keyspace walk *will not
  finish in the lifetime of the universe*. That is the entire point.

[Unreleased]: https://github.com/Chemaclass/seed-hunter/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Chemaclass/seed-hunter/releases/tag/v0.1.0
