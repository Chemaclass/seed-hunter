# Privacy

`seed-hunter` makes a deliberate trade-off: the high-volume per-attempt
data is hash-only, but the much smaller per-session metadata stores the
in-flight template in plaintext to keep the resume UX zero-friction.

## What is **not** stored in plaintext

The high-volume `attempts` table **never** stores plaintext mnemonics.
Each of the (potentially millions of) candidate rows records only a
SHA-256 hex fingerprint in the `mnemonic_hash` column. Verify it for
yourself:

```sh
sqlite3 seed-hunter.db "select id, word_index, mnemonic_hash from attempts limit 5"
```

You'll see only `mnemonic_hash` values like
`9f2eab10cafebabedeadbeef0123456789abcdef0123456789abcdef0123456` — no
words.

## What **is** stored in plaintext

The much smaller `sessions` table stores the *one* in-flight template in
plaintext (column `template`), so that `seed-hunter run` with no flags
can recover and resume the session automatically. This is the single
concession we make to keep the resume UX zero-friction.

In walk mode the template column is empty (the walker generates a fresh
mnemonic from the cursor on every iteration), but the `cursor` column
stores the live walker position as a comma-separated string.

## If plaintext template persistence is unacceptable

You have three options:

1. **Pass `--template "..."` explicitly on every run.** Point `--db` at
   an in-memory or short-lived path, and never keep the database file
   around.
2. **Wipe after every session** by running `./seed-hunter reset --yes` —
   it truncates both tables and removes the plaintext.
3. **Don't use seed-hunter** for anything sensitive. It is, after all, an
   educational tool.

The plaintext templates `seed-hunter` stores are auto-generated demo
seeds that already get printed to stdout on the first run; they were
never funded. If you point `--template` at a real funded mnemonic, that
is on you, and you should not be doing it anyway because **this is an
educational tool, not an attack tool**.

## Defense in depth

The SQLite database file is created with default OS permissions
(typically `0644`). If you run `seed-hunter` on a multi-user host, set
`--db` to a path inside a directory that only your user can read.
