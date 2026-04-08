# seed-hunter

> **Educational only.** Not a wallet recovery tool. Not a hacking tool.
> Not a way to "get back" funds from someone else's seed. Don't point
> it at addresses you don't own. Be polite to the public block-explorer
> APIs you query.

`seed-hunter` is a small Go CLI that brute-forces a 12-word BIP-39 seed
phrase, on purpose, against the real Bitcoin network, so you can watch
how impossible it actually is.

## Why

People hear "12 words from a list of 2048" and think *guessable*. It
isn't. A 12-word BIP-39 mnemonic is one of:

```
2048¹² ≈ 5.4 × 10³⁹ combinations
```

To put that in human terms: a burglar checking one trillion seed phrases
per second since the Big Bang would have completed `0.000_000_008%` of
the search by now. The Sun will swallow the Earth before they get to
one percent of one percent of one percent.

It's not that we don't have fast enough computers. The laws of
thermodynamics forbid the search. Every bit-flip dissipates energy
(Landauer's limit), and counting through 2¹²⁸ candidates costs more
energy than we can produce. Quantum computers don't save you either:
Grover's algorithm halves the bit count and you still need millions of
years on hardware that doesn't exist yet.

> If 256-bit security is unbreakable, 128-bit security is also
> unbreakable. The gap is enormous in absolute terms, but both numbers
> are on the same side of the impossibility line.

For the full math, see [`docs/math.md`](docs/math.md). For the same
intuition with much better animations than any README can pull off,
watch 3Blue1Brown's
[**How secure is 256 bit security?**](https://www.youtube.com/watch?v=S9JGmA5_unY).

This project exists so you can run a brute-forcer against the real
Bitcoin network and see, in real time, how little progress it makes.
The dashboard's `ETA full key` line is always honest about your current
throughput, and it always reads in the order of `10²⁹` to `10³⁹` years.

## How

`seed-hunter` is a small Go CLI that:

1. Generates (or accepts) a 12-word BIP-39 seed phrase template.
2. Sweeps every word position one at a time (12 × 2048 = 24,576
   candidates), deriving mainnet receiving addresses and querying a
   public Esplora API ([mempool.space](https://mempool.space) or
   [blockstream.info](https://blockstream.info)) for confirmed balances.
3. After the sweep finishes, transitions automatically into a full
   2048¹² keyspace walk that runs forever until you `Ctrl+C`.
4. Logs every attempt to a local SQLite database. Run `seed-hunter run`
   with no flags and it picks up exactly where you left off.

Under the hood it's a parallel pipeline (see
[`docs/architecture.md`](docs/architecture.md)) with a configurable
number of derivation workers, a token-bucket rate-limited HTTP checker,
and a reorder buffer that keeps the SQLite checkpoint correct under
out-of-order completion. Plaintext mnemonics never touch the per-attempt
table — see [`docs/privacy.md`](docs/privacy.md).

## What

```sh
git clone https://github.com/Chemaclass/seed-hunter
cd seed-hunter
make build
```

```sh
./seed-hunter run                # generate a demo seed and start hunting
./seed-hunter run                # no flags = resume wherever you stopped
./seed-hunter run --reset        # abandon the current session, start over
./seed-hunter stats              # counters from the SQLite db
./seed-hunter reset --yes        # wipe everything
```

That's the whole UX. The SQLite database is the source of truth and
the tool reads it back on every invocation. No flags to memorise.

### Requirements

- Go 1.26+
- git
- Internet access for the chosen public block-explorer API
- No C toolchain. `seed-hunter` uses
  [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite),
  a pure-Go SQLite driver.

## Documentation

- [`docs/math.md`](docs/math.md) — the impossibility math
- [`docs/architecture.md`](docs/architecture.md) — pipeline diagram, package layout, sweep vs walk modes
- [`docs/configuration.md`](docs/configuration.md) — flag and env-var reference
- [`docs/performance.md`](docs/performance.md) — `--workers`, the built-in benchmark
- [`docs/wordlist.md`](docs/wordlist.md) — wordlist source, using a different language
- [`docs/privacy.md`](docs/privacy.md) — what's stored, what isn't
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — contributor guide

## License

[MIT](LICENSE)
