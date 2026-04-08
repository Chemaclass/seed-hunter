# seed-hunter

> ## ⚠️ EDUCATIONAL ONLY — NOT A RECOVERY OR ATTACK TOOL ⚠️
>
> `seed-hunter` exists to make the **impossibility** of brute-forcing a
> BIP-39 seed phrase viscerally obvious. It is **not** a wallet recovery
> tool, **not** a hacking tool, and **not** a way to "get back" funds
> from someone else's seed. Do not point it at addresses you do not own.
> Be a polite citizen of the open Bitcoin infrastructure.
>
> The whole point of this project is that **it does not work**, and that
> **it cannot work**, no matter how clever you are. Watch it run for ten
> minutes and you will understand why nobody is going to brute-force
> your wallet — not today, not in a thousand years, not with quantum
> computers, not with an AI ten thousand times smarter than today.
> Not ever. The math is on your side.

## Why

Bitcoin wallets are protected by a single 12-word seed phrase. Twelve
words from a list of 2048. People hear that and think *"guessable"*. It is
not.

```
2048¹² ≈ 5.4 × 10³⁹ combinations
```

To put that number in human terms:

> A burglar checking **one trillion seed phrases per second** since the
> Big Bang would have completed **0.000_000_008%** of the search by now.
> The Sun will swallow the Earth before they get to 1% of 1% of 1%.

It is **not** that we don't have fast enough computers. It is that the
laws of thermodynamics forbid the search. Even an AI 10¹⁰⁰ × smarter
than today, running on every atom in the observable universe, hits a
hard floor called **Landauer's limit** and cannot afford the
bit-flips. Quantum computers don't save you either: Grover's algorithm
brings the cost down to ~2⁶⁶, which is *still* "millions of years on
hardware that doesn't exist yet". See [`docs/math.md`](docs/math.md) for
the full story with jokes — or, if you prefer video, 3Blue1Brown's
[**How secure is 256 bit security?**](https://www.youtube.com/watch?v=S9JGmA5_unY)
explains the same intuition with much better animations than any README
could.

This project exists so you can **watch a real brute-forcer running
against the real Bitcoin network** and feel the impossibility in your
gut. The dashboard's `ETA full key` line is always honest about your
current throughput, and it always reads in the order of `10²⁹` to `10³⁹`
years. The educational message is not the README — it's the dashboard
ticking, the cursor crawling, and the realisation that **you are
watching it not happen**.

## How

`seed-hunter` is a small Go CLI that:

1. Generates (or accepts) a 12-word BIP-39 seed phrase template.
2. **Sweeps** every word position in turn (12 × 2048 = 24,576 candidates),
   deriving mainnet receiving addresses from each candidate and querying a
   public Esplora API ([mempool.space](https://mempool.space) /
   [blockstream.info](https://blockstream.info)) for confirmed balances.
3. After the sweep finishes, **automatically transitions into a full
   keyspace walk** that tries every possible 12-word combination one at a
   time, forever. (This will not finish in the lifetime of the universe.
   That is the entire point.)
4. Logs every attempt to a local SQLite database, so you can `Ctrl+C` any
   time and `seed-hunter run` (no flags) picks up exactly where you left
   off — same template, same cursor, same parameters.

Under the hood it's a parallel pipeline (see [architecture](docs/architecture.md))
with a configurable number of derivation workers, a token-bucket
rate-limited HTTP checker, and a reorder buffer that keeps the SQLite
checkpoint correct under out-of-order completion. Plaintext mnemonics
never touch the high-volume `attempts` table — see [privacy](docs/privacy.md).

## What

```sh
git clone https://github.com/Chemaclass/seed-hunter
cd seed-hunter
make build
```

```sh
./seed-hunter run                # generate a demo seed and start hunting
./seed-hunter run                # ← no flags = resume wherever you stopped
./seed-hunter run --reset        # abandon the current session and start over
./seed-hunter stats              # show counters from the SQLite db
./seed-hunter reset --yes        # wipe everything
```

That's the whole UX. No flags to memorize, no copy-pasting commands. The
SQLite database is the source of truth and the tool reads it back on every
invocation.

### Requirements

- **Go 1.26+** (the build uses range-over-func iterators)
- **git**
- **Internet access** for the chosen public block-explorer API
- **No C toolchain needed** — `seed-hunter` uses the pure-Go
  [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) driver

## Documentation

- [**`docs/math.md`**](docs/math.md) — the impossibility math, comparisons, BIP-39 passphrase and multisig
- [**`docs/architecture.md`**](docs/architecture.md) — pipeline diagram, package responsibilities, sweep vs walk modes
- [**`docs/configuration.md`**](docs/configuration.md) — full flag and environment-variable reference
- [**`docs/performance.md`**](docs/performance.md) — `--workers`, the built-in benchmark, how to find your sweet spot
- [**`docs/wordlist.md`**](docs/wordlist.md) — wordlist source, using a different language
- [**`docs/privacy.md`**](docs/privacy.md) — what's stored, what isn't, how to opt out
- [**`CONTRIBUTING.md`**](CONTRIBUTING.md) — open-source contributor guide

## License

[MIT](LICENSE)
