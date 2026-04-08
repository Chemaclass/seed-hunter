# The math

A standard 12-word BIP-39 mnemonic is one of:

```
2048¹²  =  5,444,517,870,735,015,415,413,993,718,908,291,383,296
        ≈  5.4 × 10³⁹
```

…distinct combinations. To put that in perspective:

| Quantity                                       | Order of magnitude |
| ---------------------------------------------- | ------------------ |
| BIP-39 12-word combinations                    | ~5 × 10³⁹          |
| Atoms in a human body                          | ~7 × 10²⁷          |
| Seconds since the Big Bang                     | ~4 × 10¹⁷          |
| Total Bitcoin SHA-256 hashes ever computed     | ~10²⁹ (and rising) |
| Atoms in the observable universe               | ~10⁸⁰              |

So even if every Bitcoin miner that ever lived had been computing nothing
but BIP-39 candidates since the Big Bang, they would still be many orders
of magnitude short of finishing the search.

`seed-hunter` makes that visceral by printing a live `ETA full key` figure
in years on the dashboard. At the polite default rate of `--rate 2 req/s`,
the projected ETA is on the order of **10³⁹ years**. Crank `--workers 10`
and `--rate 100` and you'll see it drop to **~10²⁹ years** — meaningless
progress against the same impossible denominator.

## And we haven't even started yet

The real keyspace is even bigger:

- **BIP-39 passphrase ("25th word")** — adds an arbitrary user passphrase
  hashed with PBKDF2 (2048 rounds, HMAC-SHA512). For a strong passphrase
  this multiplies the keyspace by another ≈ 2¹²⁸, taking the cost from
  "absurd" to "absurd × 10³⁸".
- **Multisig (e.g. 2-of-3)** — every co-signer holds an independent BIP-39
  seed. Brute-forcing one is impossible; brute-forcing two simultaneously
  is impossible *squared*.

This is why "remember a phrase" is the only key-management story Bitcoin
needs. The numbers do the work.

## What `seed-hunter` actually covers

- **Sweep mode** (the default first phase) tries each word position in
  isolation: 12 × 2048 = **24,576 attempts**. That's `2048¹² / 24576 ≈
  2.2 × 10³⁵` times smaller than the actual keyspace. It finishes in
  minutes on a laptop and is good for a quick demo.
- **Walk mode** (the auto-transition that runs forever) tries every
  combination by walking a 12-digit base-2048 odometer. The dashboard
  shows the live cursor and the absolute count against `5.4e+39`. You will
  not see the leftmost digit advance even once in the lifetime of the
  universe.

In other words: even with the most ambitious mode the tool offers, you are
provably in the rounding error of the rounding error of the search.
