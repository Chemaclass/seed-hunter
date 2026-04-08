# seed-hunter

> ## ⚠️ EDUCATIONAL ONLY — NOT A RECOVERY OR ATTACK TOOL ⚠️
>
> `seed-hunter` exists to make the **impossibility** of brute-forcing a BIP-39 seed phrase
> viscerally obvious. It is **not** a wallet recovery tool, **not** a hacking tool,
> and **not** a way to "get back" funds from someone else's seed. Do not point it
> at addresses you do not own. Do not run it at high request rates against public
> block-explorer APIs — be a polite citizen of the open Bitcoin infrastructure.

`seed-hunter` is a small Go CLI that takes a 12-word BIP-39 mnemonic template,
swaps every BIP-39 word into a single position one at a time, derives the first
N mainnet receiving addresses for each candidate, and queries a public Esplora
API (`mempool.space` or `blockstream.info`) for confirmed balances. Every
attempt is logged to SQLite, and a long run can be **stopped with `Ctrl+C` and
resumed later** from the exact word index it left off at.

The point is the math, not the search:

> **2048¹² ≈ 5.4 × 10³⁹ combinations** — see [The math](#the-math) below.

---

## Contents

- [Requirements](#requirements)
- [Setup](#setup)
- [Quickstart](#quickstart)
- [Commands](#commands)
- [Configuration](#configuration)
- [The math](#the-math)
- [Architecture](#architecture)
- [Privacy](#privacy)
- [Contributing](#contributing)
- [License](#license)

---

## Requirements

- **Go 1.26+** — uses range-over-func iterators (`iter.Seq[T]`) and other modern features.
- **git**
- **Internet access** to whichever public block-explorer API you select.
- **No C toolchain** — `seed-hunter` uses [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite), a pure-Go SQLite driver, so cross-compilation and CI are friction-free.

That's it. There are no native dependencies and no system services to install.

## Setup

```sh
git clone https://github.com/Chemaclass/seed-hunter
cd seed-hunter
make build         # produces ./seed-hunter
```

Or, equivalently:

```sh
go build -o seed-hunter .
```

Optionally copy the example environment file:

```sh
cp .env.example .env
```

## Quickstart

The simplest possible run — generate a random demo mnemonic, mutate position 0,
derive one SegWit address per candidate, and ask `mempool.space` once every
half-second:

```sh
./seed-hunter run --position 0 --addresses 1 --rate 2
```

The first thing it prints is the freshly generated demo seed, with a clear
"do not fund this" notice. Then a live ANSI dashboard repaints in place
showing attempts, rate, ETA for the current position, and — the punchline —
the ETA in years for the full 2048¹² keyspace at your current throughput.

Press `Ctrl+C` whenever you like. The pipeline drains, flushes the SQLite
batch, and marks the session **paused**. Run the same command again later
and `seed-hunter` will resume at the exact word index it left off at:

```sh
./seed-hunter run --position 0 --addresses 1 --rate 2   # ← resumes
```

When you're done, look at the totals:

```sh
./seed-hunter stats
```

Or wipe everything and start over:

```sh
./seed-hunter reset --yes
```

## Commands

```
seed-hunter run     [--position N] [--addresses N] [--api mempool|blockstream]
                    [--rate N] [--derive-workers N] [--api-workers N]
                    [--template "..."] [--script-type segwit|legacy]
                    [--db PATH] [--fresh] [--batch-size N] [--no-dashboard]

seed-hunter stats   [--db PATH] [--session ID]

seed-hunter reset   [--db PATH] [--yes]
```

### `run`

Starts the brute-force loop. The pipeline is `generator → deriver → checker → logger`,
each stage in its own goroutine, all linked by buffered channels and a shared
`context.Context` so `Ctrl+C` is always honoured.

Resume key: `(template_hash, position, api, address_type, n_addresses)`.
Two `run` invocations with the same key resume the same session. Pass `--fresh`
to force a brand-new session even if a paused one exists.

### `stats`

Prints aggregate counters from the SQLite database. Pass `--session ID` to
drill into a single session.

### `reset`

Truncates both the `attempts` and `sessions` tables. Asks for confirmation
unless `--yes` is given.

## Configuration

Every flag has an environment-variable fallback (where it makes sense). Flags
always win over environment values.

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--db` | `SEEDHUNTER_DB` | `./seed-hunter.db` | SQLite database path |
| `--template` | `SEEDHUNTER_TEMPLATE` | _(random)_ | 12-word BIP-39 starting mnemonic |
| `--position` | — | `0` | Word position to mutate (0–11) |
| `--addresses` | — | `1` | Receiving addresses to derive per candidate |
| `--api` | `SEEDHUNTER_API` | `mempool` | `mempool` or `blockstream` |
| `--script-type` | `SEEDHUNTER_SCRIPT_TYPE` | `segwit` | `segwit` (BIP-84, `bc1...`) or `legacy` (BIP-44, `1...`) |
| `--rate` | — | `2` | API requests per second (be polite!) |
| `--derive-workers` | — | `0` | Derivation goroutines (`0` = `runtime.NumCPU()`) |
| `--api-workers` | — | `1` | API goroutines (single worker is plenty at low rates) |
| `--batch-size` | — | `50` | SQLite insert batch size |
| `--fresh` | — | `false` | Ignore any paused session for the current signature |
| `--no-dashboard` | — | `false` | Disable the live dashboard (for non-TTY use) |

## The math

A standard 12-word BIP-39 mnemonic is one of:

```
2048^12  =  5,444,517,870,735,015,415,413,993,718,908,291,383,296
         ≈  5.4 × 10³⁹
```

…distinct combinations. To put that in perspective:

| Quantity | Order of magnitude |
|---|---|
| BIP-39 12-word combinations | ~5 × 10³⁹ |
| Atoms in a human body | ~7 × 10²⁷ |
| Seconds since the Big Bang | ~4 × 10¹⁷ |
| Total Bitcoin SHA-256 hashes ever computed | ~10²⁹ (and rising) |
| Atoms in the observable universe | ~10⁸⁰ |

So even if every Bitcoin miner that ever lived had been computing nothing but
BIP-39 candidates since the Big Bang, they would still be many orders of
magnitude short of finishing the search. `seed-hunter` makes that visceral by
printing a live `ETA full key` figure in years. At the polite default rate of
**2 req/s**, the ETA is on the order of **10³⁹ years**.

And we haven't even started yet:

- **BIP-39 passphrase ("25th word")** — adds an arbitrary user passphrase
  hashed with PBKDF2 (2048 rounds, HMAC-SHA512). For a strong passphrase
  this multiplies the keyspace by another ≈ 2¹²⁸, taking the cost from
  "absurd" to "absurd × 10³⁸".
- **Multisig (e.g. 2-of-3)** — every co-signer holds an independent BIP-39
  seed. Brute-forcing one is impossible; brute-forcing two simultaneously
  is impossible *squared*.

This is why "remember a phrase" is the only key-management story Bitcoin
needs. The numbers do the work.

## Architecture

```
                  ┌───────────┐    ┌──────────────┐    ┌───────────┐    ┌──────────┐
[2048 words] ──▶  │ generator │ ─▶ │   deriver    │ ─▶ │  checker  │ ─▶ │  logger  │
                  │ (1 g.r.)  │    │ (CPU pool)   │    │ (rate-    │    │ (1 g.r., │
                  └───────────┘    └──────────────┘    │  limited) │    │  batched)│
                                                       └───────────┘    └──────────┘
                                                                              │
                                                                              ▼
                                                                       ┌────────────┐
                                                                       │   SQLite   │
                                                                       └────────────┘
                                                                              │
                                                                              ▼
                                                                       ┌────────────┐
                                                                       │ dashboard  │
                                                                       │ (200ms)    │
                                                                       └────────────┘
```

| Package | Responsibility |
|---|---|
| `cmd/` | Cobra subcommands and flag wiring |
| `config/` | `Config` struct + validation + env-var fallbacks |
| `internal/bip39/` | Word iterator over a fixed `--position`, mnemonic fingerprinting |
| `internal/derivation/` | BIP-32/44/84 mainnet receive-address derivation |
| `internal/checker/` | Esplora-compatible balance checker + token-bucket rate limiter |
| `internal/storage/` | SQLite repository with embedded schema and resume support |
| `internal/pipeline/` | The four-stage channel pipeline that wires everything together |
| `internal/dashboard/` | Pure `Render`, plus a 200ms repaint loop driven by atomic counters |

## Privacy

`seed-hunter` **never** stores plaintext mnemonics. The SQLite `attempts`
table records only a SHA-256 hex fingerprint of each candidate (column
`mnemonic_hash`). The `sessions.template_hash` column hashes the starting
template the same way. Inspect the database directly to verify:

```sh
sqlite3 seed-hunter.db "select id, word_index, mnemonic_hash from attempts limit 5"
```

## Contributing

We welcome contributions that improve the educational value, fix bugs, or
add support for additional read-only Esplora-compatible providers. We do
**not** accept changes that turn `seed-hunter` into a real attack tool —
parallel-position fuzzing, rate-limit evasion, anonymisation, etc. are
explicitly out of scope.

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the full guide.

## License

[MIT](LICENSE)
