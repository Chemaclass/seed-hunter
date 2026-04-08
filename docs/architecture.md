# Architecture

## Pipeline (sweep mode)

```
                  ┌───────────┐    ┌──────────────┐    ┌─────────┐    ┌───────────┐    ┌──────────┐
[2048 words] ──▶  │ generator │ ─▶ │ N derivers   │ ─▶ │ reorder │ ─▶ │  checker  │ ─▶ │  logger  │
                  │ (1 g.r.)  │    │ (CPU pool)   │    │ (1 g.r.)│    │ (rate-    │    │ (1 g.r., │
                  └───────────┘    └──────────────┘    └─────────┘    │  limited) │    │  batched)│
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

Each stage runs in its own goroutine, linked by buffered channels and a
shared `context.Context` so `Ctrl+C` is always honoured.

The **reorder** stage exists because the parallel deriver pool may finish
candidates out of order: worker A might complete index 5 before worker B
finishes index 3. The reorder buffer keeps a tiny `pending` map (≤
`workers` items) and emits Derived items in strict `WordIndex` order. This
keeps the SQLite `last_word_index` checkpoint always equal to the highest
**contiguous** index processed, which is what makes resume after `Ctrl+C`
correct under any `--workers` value.

## Walk mode (full keyspace)

After the sweep phase finishes (or if you pass `--skip-sweep`), the same
process transitions into the keyspace walker. The walker is single-
goroutine — the rate limiter dominates the cost so parallelism doesn't
help — and it iterates a 12-element `Cursor` like a base-2048 odometer:

```
[generator: walker.Inc()] ─▶ [deriver] ─▶ [checker] ─▶ [logger + cursor checkpoint]
```

The cursor is persisted to the `sessions.cursor` column as a
comma-separated string after every batch flush, so the next
`seed-hunter run` (no flags) reads it back and continues from exactly
where you stopped — same template, same parameters, same cursor.

Position 11 advances every iteration; position 10 every 2048; position 9
every 2048²; …; position 0 every 2048¹¹ ≈ 2.4 × 10³⁶ iterations. You will
not observe position 0 advance even once.

## Packages

| Package                  | Responsibility                                                               |
| ------------------------ | ---------------------------------------------------------------------------- |
| `cmd/`                   | Cobra subcommands and flag wiring                                            |
| `config/`                | `Config` struct, validation, env-var fallbacks, `--positions` parser         |
| `internal/wordlist/`     | Embedded English BIP-39 wordlist + file loader                               |
| `internal/bip39/`        | Word iterator + mnemonic SHA-256 fingerprinting                              |
| `internal/derivation/`   | BIP-32 / BIP-44 / BIP-84 mainnet receive-address derivation                  |
| `internal/checker/`      | Esplora-compatible balance checker + token-bucket rate limiter               |
| `internal/storage/`      | SQLite repository, embedded schema, forward-only migrations, resume helpers  |
| `internal/pipeline/`     | Sweep pipeline, walker, `Cursor` type, `Stats` (atomic counters), `Result`   |
| `internal/dashboard/`    | Pure `Render`, plus a 200ms repaint loop driven by atomic counters           |

## Storage schema

The `sessions` table is the single source of truth for everything needed
to resume a run: template, position, addresses, api, script type, rate,
wordlist path, workers, positions spec, mode (`sweep` or `walk`), and the
walker cursor. Older databases auto-upgrade via forward-only `ALTER TABLE`
calls in `storage.Open`.

The `attempts` table records every candidate's SHA-256 fingerprint, the
derived addresses (JSON), the balance, the duration, and the checked-at
timestamp. It is **never** queried by the runtime — it exists for `stats`
and for after-the-fact inspection with `sqlite3`.

## Cancellation and shutdown

`signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` wires Ctrl+C
into the root context. Every stage `select`s on `ctx.Done()` and exits
cleanly. The logger uses a **background context** for the final flush so
that even a cancelled run still persists its in-flight batch and
checkpoint before the process exits.

The session is marked `paused` on cancel and `completed` on natural
exhaustion (full position sweep, or — in the impossible case — full
keyspace walk).
