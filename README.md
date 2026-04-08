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

The simplest possible run — generate a random demo mnemonic and start
hunting:

```sh
./seed-hunter run
```

The first thing it prints is the freshly generated demo seed (with a clear
"do not fund this" notice). By default, `seed-hunter` then **sweeps every
word position from 0 to 11 sequentially**: 12 × 2048 = 24,576 candidates
total. After each position completes, the next one starts automatically.

A live ANSI dashboard repaints in place showing the current position,
attempts, rate, ETA for that position, and — the punchline — the ETA in
years for the full 2048¹² keyspace at your current throughput.

Press `Ctrl+C` whenever you like. The pipeline drains, flushes the SQLite
batch, and marks the current position's session **paused**. To pick up
exactly where you left off (same word index, same position, then auto-
advance through the remaining positions), run the same command with **no
flags at all**:

```sh
./seed-hunter run                # ← resumes wherever you stopped, then keeps going
```

Every parameter (template, positions list, addresses, api, script type,
rate, wordlist, workers) is stored in SQLite and read back automatically.
You don't need to remember or retype anything.

### Sweeping a custom subset of positions

Pass `--positions` to control which positions are visited and in what
order:

```sh
./seed-hunter run --positions 5         # just position 5 (2048 candidates)
./seed-hunter run --positions 0-11      # all 12 positions (default)
./seed-hunter run --positions 3-7       # positions 3,4,5,6,7
./seed-hunter run --positions 0,3,7     # just those three, in that order
./seed-hunter run --positions 0,3-5,9   # mixed: 0, 3, 4, 5, 9
```

The `--positions` value gets persisted on every session row, so the same
sweep continues across `Ctrl+C` / resume cycles.

### Overriding individual parameters on resume

If you want to resume but change one knob (say, bump the rate), pass just
that flag:

```sh
./seed-hunter run --rate 5       # resumes the same session with rate=5
```

Any flag you pass wins over the stored value; any flag you omit is inherited
from the last paused session.

### Starting over

To abandon the current paused session and start fresh, use `--reset`:

```sh
./seed-hunter run --reset        # generates a new demo seed, new session
```

`--reset` retires the previous paused session (it stays in the DB as
`completed` for inspection but is no longer the resume target) and starts
a brand-new run. You can combine it with explicit flags to control the new
session's parameters.

### Checking progress and starting clean

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

Starts (or resumes) the brute-force loop. The pipeline is
`generator → deriver → checker → logger`, each stage in its own goroutine,
all linked by buffered channels and a shared `context.Context` so `Ctrl+C`
is always honoured.

**With no flags**, `run` reads the most recent paused session from the
database and resumes it with all of its parameters intact. Any flag you do
pass overrides the corresponding inherited value.

Pass `--reset` to ignore the last paused session and start a new one. Any
other flags you pass alongside `--reset` configure the new run; everything
else uses the package defaults.

The resume key is `(template_hash, position, api, address_type, n_addresses)`.
Two runs with the same key are considered the same session.

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
Every flag (except `--reset` and `--no-dashboard`) is **inherited from the
last paused session** if you don't pass it. The defaults below apply only
when there is no previous session to inherit from (or when you pass
`--reset`).

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--db` | `SEEDHUNTER_DB` | `./seed-hunter.db` | SQLite database path |
| `--wordlist` | `SEEDHUNTER_WORDLIST` | _(embedded English)_ | Path to a 2048-word BIP-39 wordlist file |
| `--template` | `SEEDHUNTER_TEMPLATE` | _(random)_ | 12-word BIP-39 starting mnemonic |
| `--positions` | — | `0-11` | Positions to sweep: `5`, `0-11`, `0,3,7`, `0,3-5,9` |
| `--addresses` | — | `1` | Receiving addresses to derive per candidate |
| `--api` | `SEEDHUNTER_API` | `mempool` | `mempool` or `blockstream` |
| `--script-type` | `SEEDHUNTER_SCRIPT_TYPE` | `segwit` | `segwit` (BIP-84, `bc1...`) or `legacy` (BIP-44, `1...`) |
| `--rate` | — | `2` | API requests per second (be polite!) |
| `--workers` | — | `2` | Parallel deriver goroutines (≥ 1, see notes below) |
| `--api-workers` | — | `1` | Reserved; the rate limiter currently serializes upstream calls |
| `--batch-size` | — | `50` | SQLite insert batch size |
| `--reset` | — | `false` | Ignore the most recent paused session and start over |
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

## Parallelism (`--workers`)

`seed-hunter` runs the BIP-32/44/84 derivation stage in `N` parallel
goroutines (default `2`). The pipeline becomes:

```
generator → candidates → [N derivers] → reorder → checker → logger
```

The new **reorder** stage is what makes parallel derivers safe: even though
the workers may finish out of order, the reorder buffer emits items in
strict `word_index` order so the SQLite `last_word_index` checkpoint always
reflects the highest **contiguous** index processed. This means resume
after `Ctrl+C` is correct regardless of `--workers`.

```sh
./seed-hunter run --workers 8        # 8 parallel derivers
```

Pick whatever `--workers` value you like (1 → `runtime.NumCPU()` is
sensible). The dashboard's `attempts/s` rate will scale roughly linearly
with this number — which makes the educational ETA-in-years figure go
**down**, not up.

### Honest performance note

Parallel derivers do **not** beat the rate limiter. ~15 of every 16 BIP-39
candidates fail the checksum and skip the API entirely; the ~1 in 16 that
*do* hit the API are still capped at `--rate` requests per second. So
`--workers 16` makes the dashboard look ~16× faster on the candidate-rate
line, but the irreducible per-position wall time is still
`(2048 / 16) / rate ≈ 64 seconds` at the default `--rate 2`.

This is the educational point: even if you parallelize the cheap stage to
infinity, the slow stage is still slow, and the **full keyspace ETA never
gets meaningfully smaller**. Try it:

```sh
./seed-hunter run --workers 1   # ETA full key ≈ 10^39 years
./seed-hunter run --workers 16  # ETA full key ≈ 10^38 years
./seed-hunter run --workers 64  # ETA full key ≈ 10^37 years
```

You can throw all the cores in the universe at it. You will not finish.

### Finding the optimal `--workers` value for your machine

The default `--workers 2` is intentionally conservative — it works
everywhere from a Raspberry Pi to a 64-core workstation. To find the
**actual** sweet spot for your hardware, run the built-in benchmark:

```sh
go test -bench=BenchmarkRunWorkers -benchtime=2x -run=^$ ./internal/pipeline/
```

That command runs one full 2048-candidate pipeline pass with the **real**
BIP-32/44/84 deriver and a no-op checker (no network, no rate limit) for
several `--workers` values, and prints the wall time for each. Look for
the row where adding more workers stops cutting the time — that's your
sweet spot.

Sample output on an Apple M4 Pro (10 performance + 4 efficiency cores):

```
BenchmarkRunWorkers/workers=1-14   2  74_508_166 ns/op   ← 1.00× baseline
BenchmarkRunWorkers/workers=2-14   2  39_913_375 ns/op   ← 1.87×
BenchmarkRunWorkers/workers=4-14   2  22_155_521 ns/op   ← 3.36×
BenchmarkRunWorkers/workers=6-14   2  16_408_125 ns/op   ← 4.54×
BenchmarkRunWorkers/workers=8-14   2  13_552_500 ns/op   ← 5.50×
BenchmarkRunWorkers/workers=10-14  2  12_653_104 ns/op   ← 5.87×  ← sweet spot
BenchmarkRunWorkers/workers=12-14  2  12_670_500 ns/op   ← 5.87×  (flat)
BenchmarkRunWorkers/workers=14-14  2  12_870_730 ns/op   ← 5.78×  (slight drop)
BenchmarkRunWorkers/workers=16-14  2  13_687_042 ns/op   ← 5.44×  (worse)
```

For this machine, **`--workers 10` is optimal** — exactly the number of
performance cores. Beyond that the run spills onto efficiency cores and
the reorder/checker serialization points eat the gains.

#### Rule of thumb

- On **Apple Silicon (M-series)**: pick the number of **performance** cores
  (`sysctl -n hw.perflevel0.physicalcpu`). The realistic ceiling is about
  6× the single-worker rate.
- On **Intel/AMD with SMT**: start with the number of **physical** cores
  (`getconf _NPROCESSORS_ONLN` for logical, `lscpu | grep '^Core(s)'` for
  physical). Hyperthreading helps a little but the per-thread gains are
  small.
- On a **VPS or container** with shared CPUs: stay at the default `2` —
  you have no idea what else is on the host, and being a polite neighbour
  matters more than the marginal speedup.

Once you find your number, set it once and forget it — the value gets
**persisted in the session row** alongside everything else, so future
`seed-hunter run` invocations inherit it automatically.

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

## Wordlist

The 2048 candidate words are loaded from a file at startup, not pulled from a
hard-coded library. By default `seed-hunter` uses the canonical English
BIP-39 wordlist that ships **embedded** in the binary at build time
(`internal/wordlist/english.txt`, byte-for-byte identical to
[bitcoin/bips/bip-0039/english.txt](https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt)).
No file lookup is needed for the default; the binary works on a system that
doesn't have any wordlist files at all.

To use a different language, point `--wordlist` (or `SEEDHUNTER_WORDLIST`) at
one of the other official BIP-39 wordlist files from the
[bitcoin/bips repository](https://github.com/bitcoin/bips/tree/master/bip-0039):

```sh
curl -fsSL https://raw.githubusercontent.com/bitcoin/bips/master/bip-0039/spanish.txt -o spanish.txt
./seed-hunter run --wordlist ./spanish.txt --template "..."
```

When you supply `--wordlist`, `seed-hunter` also rebinds the underlying
BIP-39 library so that checksum validation and PBKDF2 seed derivation use the
same words — the iterator and the deriver are always in sync. The file must
contain exactly **2048 unique non-empty lines** (UTF-8, one word per line).
Anything else is rejected at startup with a clear error.

> ⚠️ A custom wordlist that is not one of the 9 official BIP-39 lists will
> still load, but every candidate will fail the BIP-39 checksum and no
> mnemonic will reach the balance check. That's only useful as a "show me how
> the iterator walks 2048 candidates" demo.

## Privacy

The high-volume `attempts` table **never** stores plaintext mnemonics. Each
of the 2048 candidate rows records only a SHA-256 hex fingerprint
(`mnemonic_hash` column). Verify it for yourself:

```sh
sqlite3 seed-hunter.db "select id, word_index, mnemonic_hash from attempts limit 5"
```

The much smaller `sessions` table **does** store the *one* in-flight
template in plaintext (column `template`), so that `seed-hunter run` with
no flags can recover and resume the session automatically. This is the
single concession we make to keep the resume UX zero-friction. If that's
not acceptable for your use case:

- Use `--template "..."` explicitly on every run, point `--db` at an
  in-memory or short-lived path, and never keep the database file around.
- Or run `./seed-hunter reset --yes` after every session — it truncates
  both tables and removes the plaintext.

This is, again, an **educational** tool. The plaintext templates it stores
are auto-generated demo seeds that already get printed to stdout on the
first run; they were never funded.

## Contributing

We welcome contributions that improve the educational value, fix bugs, or
add support for additional read-only Esplora-compatible providers. We do
**not** accept changes that turn `seed-hunter` into a real attack tool —
parallel-position fuzzing, rate-limit evasion, anonymisation, etc. are
explicitly out of scope.

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the full guide.

## License

[MIT](LICENSE)
