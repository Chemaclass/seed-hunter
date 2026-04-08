# Configuration

`seed-hunter run` accepts a handful of flags. Most of them have an env-var
fallback (`SEEDHUNTER_*`). **Flags always win over environment values**,
and **flags you don't pass are inherited from the most recent paused
session in the database**, so the defaults below only apply when there is
nothing to inherit and nothing in the environment either.

## Flags

| Flag             | Env var                  | Default               | Description                                                                              |
| ---------------- | ------------------------ | --------------------- | ---------------------------------------------------------------------------------------- |
| `--db`           | `SEEDHUNTER_DB`          | `./seed-hunter.db`    | SQLite database path                                                                     |
| `--wordlist`     | `SEEDHUNTER_WORDLIST`    | _(embedded English)_  | Path to a 2048-word BIP-39 wordlist file (see [wordlist](wordlist.md))                   |
| `--template`     | `SEEDHUNTER_TEMPLATE`    | _(random)_            | 12-word BIP-39 starting mnemonic                                                         |
| `--positions`    | —                        | `0-11`                | Positions to sweep: `5`, `0-11`, `0,3,7`, `0,3-5,9`                                       |
| `--addresses`    | —                        | `1`                   | Receiving addresses to derive per candidate                                              |
| `--api`          | `SEEDHUNTER_API`         | `mempool`             | `mempool` or `blockstream`                                                               |
| `--script-type`  | `SEEDHUNTER_SCRIPT_TYPE` | `segwit`              | `segwit` (BIP-84, `bc1...`) or `legacy` (BIP-44, `1...`)                                 |
| `--rate`         | —                        | `2`                   | API requests per second. Be polite to public block-explorer APIs.                        |
| `--workers`      | —                        | `2`                   | Parallel deriver goroutines (≥ 1, see [performance](performance.md))                     |
| `--api-workers`  | —                        | `1`                   | Reserved; the rate limiter currently serializes upstream calls                           |
| `--batch-size`   | —                        | `50`                  | SQLite insert batch size                                                                 |
| `--reset`        | —                        | `false`               | Ignore the most recent paused session and start over                                     |
| `--no-walk`      | —                        | `false`               | Stop after the 12-position sweep; do **not** auto-transition into the keyspace walk      |
| `--skip-sweep`   | —                        | `false`               | Skip the 12-position sweep entirely and go straight to the full keyspace walk            |
| `--no-dashboard` | —                        | `false`               | Disable the live dashboard (useful for non-TTY use)                                      |

## Positions spec

`--positions` accepts a flexible spec language:

```sh
./seed-hunter run --positions 5         # just position 5 (2048 candidates)
./seed-hunter run --positions 0-11      # all 12 positions (default)
./seed-hunter run --positions 3-7       # positions 3,4,5,6,7
./seed-hunter run --positions 0,3,7     # those three, in that order
./seed-hunter run --positions 0,3-5,9   # mixed: 0,3,4,5,9
```

The spec is parsed left-to-right, preserves user order, and rejects
duplicates and out-of-range values at startup with a clear error.

## Resume semantics

When you run `seed-hunter run` with no flags, the tool:

1. Opens the SQLite database at `--db`.
2. Reads the most recent session whose status is `paused` or `running`.
3. **Inherits every persisted parameter** from that session: template,
   positions, addresses, api, script type, rate, wordlist path, workers,
   and the in-flight cursor (in walk mode).
4. Uses any flag you DID pass on the command line to override the
   inherited value.
5. Resumes from that session's checkpoint:
   - **Sweep mode**: starts at the in-flight position's `last_word_index + 1`
     and auto-advances through any remaining positions afterwards.
   - **Walk mode**: starts at the persisted cursor's next position and
     keeps going forever.

This means typing `seed-hunter run` over and over is the entire UX. You
never need to remember a long command line.

## Three ways to start over

| Goal                               | Command                          |
| ---------------------------------- | -------------------------------- |
| Drop the latest session, start anew | `seed-hunter run --reset`        |
| Wipe the entire database           | `seed-hunter reset --yes`        |
| Use a fresh database file          | `seed-hunter run --db /tmp/x.db` |

## Other commands

```
seed-hunter stats   [--db PATH] [--session ID]
seed-hunter reset   [--db PATH] [--yes]
```

- `stats` prints aggregate counters from the SQLite database. Pass
  `--session ID` to drill into a single session.
- `reset` truncates both the `attempts` and `sessions` tables. Asks for
  confirmation unless `--yes` is given.
