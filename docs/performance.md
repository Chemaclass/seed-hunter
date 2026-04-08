# Performance and `--workers`

`seed-hunter` runs the BIP-32 / BIP-44 / BIP-84 derivation stage in `N`
parallel goroutines (default `2`). The pipeline is:

```
generator → candidates → [N derivers] → reorder → checker → logger
```

The **reorder** stage is what makes parallel derivers safe: even though
the workers may finish out of order, the reorder buffer emits items in
strict `word_index` order so the SQLite `last_word_index` checkpoint
always reflects the highest **contiguous** index processed. Resume after
`Ctrl+C` is correct regardless of `--workers`.

```sh
./seed-hunter run --workers 8        # 8 parallel derivers
```

## Honest performance note

Parallel derivers do **not** beat the rate limiter. ~15 of every 16
BIP-39 candidates fail the checksum and skip the API entirely; the ~1 in
16 that *do* hit the API are still capped at `--rate` requests per
second. So `--workers 16` makes the dashboard look ~16× faster on the
candidate-rate line, but the irreducible per-position wall time is still
`(2048 / 16) / rate ≈ 64 seconds` at the default `--rate 2`.

This is the educational point: even if you parallelize the cheap stage to
infinity, the slow stage is still slow, and the **full keyspace ETA never
gets meaningfully smaller**. Try it:

```sh
./seed-hunter run --workers 1   # ETA full key ≈ 10³⁹ years
./seed-hunter run --workers 16  # ETA full key ≈ 10³⁸ years
./seed-hunter run --workers 64  # ETA full key ≈ 10³⁷ years
```

You can throw all the cores in the universe at it. You will not finish.

## Finding the optimal `--workers` value for your machine

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

## Rule of thumb

- **Apple Silicon (M-series)**: pick the number of **performance** cores
  (`sysctl -n hw.perflevel0.physicalcpu`). The realistic ceiling is about
  6× the single-worker rate.
- **Intel/AMD with SMT**: start with the number of **physical** cores
  (`getconf _NPROCESSORS_ONLN` for logical, `lscpu | grep '^Core(s)'` for
  physical). Hyperthreading helps a little but the per-thread gains are
  small.
- **VPS or container** with shared CPUs: stay at the default `2` — you
  have no idea what else is on the host, and being a polite neighbour
  matters more than the marginal speedup.

Once you find your number, set it once and forget it — the value gets
**persisted in the session row** alongside everything else, so future
`seed-hunter run` invocations inherit it automatically.
