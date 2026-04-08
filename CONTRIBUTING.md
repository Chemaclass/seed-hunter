# Contributing to seed-hunter

Thanks for your interest! `seed-hunter` is an open-source educational project
about why brute-forcing a BIP-39 seed phrase is impossible. The codebase is
deliberately small and idiomatic so it can be read in an afternoon.

## What's in scope

- Bug fixes and clarity improvements.
- Additional **read-only** Esplora-compatible balance providers.
- Better educational output: more illustrative dashboards, more math in the README, ASCII diagrams.
- Test improvements that exercise observable behaviour.
- Documentation, typos, accessibility, and example walkthroughs.

## What's out of scope

`seed-hunter` is a teaching tool. Pull requests that move it toward being a
practical attack tool will be closed without merge. Examples of out-of-scope
work:

- Parallel-position fuzzing or any iteration mode beyond the single `--position` brief.
- API rate-limit evasion, IP rotation, header spoofing, or proxy support.
- Anonymisation features (Tor, VPN integration, fingerprint obfuscation).
- "Found seed" alerting beyond what already lands in the SQLite `attempts.balance_sats` column.
- Anything that helps a user check addresses they don't own at scale.

If you're not sure whether a proposal fits, open an issue first and we'll talk it through.

## Code of conduct

By contributing you agree to behave in line with the [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/).
Be kind, be specific, be patient.

## Dev setup

```sh
git clone https://github.com/Chemaclass/seed-hunter
cd seed-hunter
make build
make test
make lint    # requires golangci-lint
```

If you don't have the linter installed (CI uses v2.11.4):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
```

You'll need Go 1.26 or later and `git`. Nothing else.

## Project conventions

### Commits

We use **conventional commits**. Allowed prefixes:

- `feat:` — a new user-visible capability
- `fix:` — a bug fix
- `ref:` — refactor with no behavioural change (we use `ref:`, not `refactor:`)
- `docs:` — documentation only
- `test:` — tests only
- `chore:` — tooling, build, dependencies
- `ci:` — CI/CD changes

One concern per commit, one concern per pull request. PR descriptions should
explain *why*, not just *what*.

### Tests

Every behavioural change comes with a behavioural test. We do not accept
coverage-padding tests for trivial accessors. A good test should:

- Have a name that describes a user-visible behaviour ("returns ErrRateLimited on 429", not "test_check_addresses_2").
- Run quickly. Use `httptest` for upstream APIs and a temp directory for SQLite.
- Be deterministic — no real network, no real time-of-day dependence beyond `time.Sleep` tolerances.

### Benchmarks

`internal/pipeline/pipeline_bench_test.go` ships a `BenchmarkRunWorkers`
that measures the wall-clock cost of one full 2048-candidate pipeline pass
at several `--workers` values, using the **real** BIP-32/44/84 deriver and
a no-op checker (no network, no rate limit). It exists so users can pick
the optimal `--workers` value for their hardware:

```sh
go test -bench=BenchmarkRunWorkers -benchtime=2x -run=^$ ./internal/pipeline/
```

If you change anything in the pipeline hot path (the `derive`, `reorder`,
`check`, or `logResults` functions), please re-run this benchmark and
include before/after numbers in your PR description.

### Style

- `make lint test` must be green before requesting review.
- Idiomatic Go. We use `log/slog`, table-driven tests with `t.Run`, sentinel errors checked with `errors.Is`, and `context.Context` as the first parameter of any function that does I/O.
- No globals beyond `cobra` command registration. Inject dependencies via constructors.
- Errors are wrapped with `%w` at boundaries; sentinel errors are exported as package-level `var Err...`.

## Adding a new block-explorer provider

The plumbing for additional Esplora-compatible providers is intentionally
small. To add one:

1. Implement `checker.BalanceChecker` in a new file under `internal/checker/`.
2. Register it in `checker.New` and add a constant to the `Provider` enum.
3. Add a `..._test.go` that uses `httptest.NewServer` to fake the upstream's responses, mirroring the pattern in `mempool_test.go` and `blockstream_test.go`.
4. Document the new provider in the README's `--api` table and in this file.
5. Bonus: add a `--rate` recommendation in the README based on the provider's published limits.

## Reporting security issues

If you find a **security** issue (e.g. accidental plaintext leak of a
mnemonic, a path traversal in `--db`, anything that affects user data), do
NOT open a public issue. Email the maintainer privately first; we'll
coordinate disclosure.

For everything else (bugs, ideas, questions), GitHub issues are the right
place.
