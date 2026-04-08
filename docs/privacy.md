# Privacy

> The only "secret" `seed-hunter` ever stores is an auto-generated demo
> mnemonic that has never been funded, never will be funded, and is
> printed to your terminal on the first run anyway. Brute-forcing a real
> seed is [thermodynamically forbidden](math.md), not "hard". Even if
> the demo mnemonic leaked, attacking it would take longer than the heat
> death of the universe and find an empty wallet at the end.

`seed-hunter` makes a deliberate trade-off: the high-volume per-attempt
data is hash-only, but the much smaller per-session metadata stores the
in-flight template in plaintext to keep the resume UX zero-friction.

## What is not stored in plaintext

The `attempts` table never stores plaintext mnemonics. Each of the
(potentially millions of) candidate rows records only a SHA-256 hex
fingerprint in the `mnemonic_hash` column. Verify it yourself:

```sh
sqlite3 seed-hunter.db "select id, word_index, mnemonic_hash from attempts limit 5"
```

You'll see only `mnemonic_hash` values like
`9f2eab10cafebabedeadbeef0123456789abcdef0123456789abcdef0123456`. No
words.

## What is stored in plaintext

The `sessions` table stores the one in-flight template in plaintext
(column `template`) so that `seed-hunter run` with no flags can
recover and resume the session automatically. That's the single
concession to keep the resume UX zero-friction.

In walk mode the template column is empty (the walker generates a fresh
mnemonic from the cursor on every iteration), but the `cursor` column
stores the live walker position as a comma-separated string.

## If plaintext template persistence is unacceptable

You have three options:

1. **Pass `--template "..."` explicitly on every run.** Point `--db` at
   an in-memory or short-lived path, and never keep the database file
   around.
2. **Wipe after every session** by running `./seed-hunter reset --yes`.
   It truncates both tables and removes the plaintext.
3. **Don't use `seed-hunter`** for anything sensitive. It's an
   educational tool.

The plaintext templates `seed-hunter` stores are auto-generated demo
seeds that already get printed to stdout on the first run. They were
never funded. If you point `--template` at a real funded mnemonic, that
is on you, and you shouldn't be doing it anyway. This is an educational
tool, not an attack tool.

## Defence in depth

The SQLite database file is created with default OS permissions
(typically `0644`). If you run `seed-hunter` on a multi-user host, set
`--db` to a path inside a directory that only your user can read.
