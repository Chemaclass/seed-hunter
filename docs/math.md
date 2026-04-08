# The math

A 12-word BIP-39 mnemonic is one of `2048¹²` ≈ `5.4 × 10³⁹`
combinations. That's a 5 followed by 39 zeros. Your intuition will lie
to you about what this number means. This document is here to fix it.

## What 5.4 × 10³⁹ actually looks like

| Quantity                                       | Order of magnitude |
| ---------------------------------------------- | ------------------ |
| Grains of sand on every beach on Earth         | ~7.5 × 10¹⁸        |
| Stars in the observable universe               | ~10²⁴              |
| Atoms in a human body                          | ~7 × 10²⁷          |
| Total SHA-256 hashes Bitcoin has ever computed | ~10²⁹              |
| Atoms in planet Earth                          | ~10⁵⁰              |
| Atoms in the observable universe               | ~10⁸⁰              |
| **BIP-39 12-word combinations**                | **~5 × 10³⁹**      |

The keyspace of one seed phrase is roughly the lifetime hash output of
every Bitcoin miner that has ever existed, multiplied by ~10¹⁰. It's
larger than the number of atoms in your body raised to the second
power.

## The patient burglar

Imagine a burglar who checks one trillion seed phrases per second
(`10¹²` candidates per second, about a billion times faster than this
educational tool). They started the moment the Big Bang happened, 13.8
billion years ago, and they've been working uninterrupted ever since.
No breaks, no holidays, no Y2K.

How far have they got?

```
13.8 × 10⁹ years × 365.25 × 24 × 3600  =  4.4 × 10¹⁷ seconds
4.4 × 10¹⁷ s × 10¹² candidates/s        =  4.4 × 10²⁹ candidates checked
```

That's `4.4 × 10²⁹` out of `5.4 × 10³⁹`. They have completed about
`8 × 10⁻¹¹` of the search. **0.000_000_008%**, after 13.8 billion
years.

They will need another ~`10²² × the current age of the universe` to
finish. The Sun will go red giant and swallow the Earth before our
burglar gets to one percent of one percent of one percent.

## Quantum computers don't save you

Grover's algorithm gives a quadratic speedup on unstructured search.
Effective security drops from `2¹²⁸` to roughly `2⁶⁴`. That sounds
worse, but `2⁶⁴` ≈ `1.8 × 10¹⁹` is still well past anything you can
build:

- A perfect quantum brute-forcer doing one billion Grover iterations
  per second would need ~570 years to finish a single seed.
- We don't have such a quantum computer. We have machines that, on a
  good day, can factor 21.
- Real quantum is gated on coherence time, error correction, and
  physical-qubit overhead. Current best estimates put a Grover-class
  attack on a 256-bit key at millions of physical qubits. The largest
  publicly demonstrated systems have a few thousand.

You'll see the headline "QUANTUM COMPUTER BREAKS BITCOIN" exactly when
gravity stops working.

## A smarter AI doesn't save you either

Make the AI 10⁴ × smarter than today. Make it 10¹⁰⁰ × smarter. The AI
still has to physically check candidates, and physical computation has
a hard floor: **Landauer's limit**. Erasing one bit of information
costs at least `kT ln 2` joules. At room temperature that's about
`3 × 10⁻²¹` joules per bit-flip.

To merely *count* through `2¹²⁸` candidates (just incrementing a
counter, no derivation, no SHA-256, no PBKDF2) costs at least:

```
2¹²⁸ × 3 × 10⁻²¹ J ≈ 10¹⁸ J
```

The Sun's total annual energy output is about `1.2 × 10³⁴` J. The
counter alone is fine on solar power. But the actual work per
candidate (PBKDF2, EC key derivation, address hashing, blockchain
query) is closer to `2¹⁸` bit-operations. The total energy floor
becomes:

```
2¹²⁸ × 2¹⁸ × 3 × 10⁻²¹ J ≈ 4 × 10²³ J
```

That's about 3% of the Sun's annual output, every year, for one key.

A brute-force attack on a single BIP-39 seed isn't blocked by
"computers are slow". It's blocked by the second law of thermodynamics.
No amount of intelligence — biological, artificial, alien — gets
around `kT ln 2`.

## Bits of what?

You'll see different bit counts cited for BIP-39 and Bitcoin. They're
all real, and they refer to different things. Three numbers worth
keeping straight:

| Number       | What it is                                                                             | Applies to                                |
| ------------ | -------------------------------------------------------------------------------------- | ----------------------------------------- |
| **128**      | Real entropy in a 12-word mnemonic. The other 4 of the 132 bits are checksum, deterministic. | 12-word seeds                       |
| **256**      | Real entropy in a 24-word mnemonic. Also the size of every secp256k1 private key.    | 24-word seeds, raw secp256k1 keys         |
| 132 / 264    | Search space of the *full* mnemonic including the deterministic checksum bits.       | Naive brute-forcers that don't filter checksums |

A wallet's effective security is the smaller of (mnemonic entropy,
private key size):

- 12-word seed → `min(128, 256) = 128` bits → ~`3.4 × 10³⁸` possibilities
- 24-word seed → `min(256, 256) = 256` bits → ~`1.16 × 10⁷⁷` possibilities

A 12-word seed leaves 128 bits of the 256-bit private-key space
unused. BIP-32 derivation is deterministic, so guessing the seed is no
harder than guessing the entropy that produced it.

`seed-hunter` iterates 12-word seeds, so the directly-relevant number
is `2¹²⁸`, not `2²⁵⁶`. Both numbers are already past the thermodynamic
ceiling. Landauer's limit forbids the search at either size, the
patient burglar is stuck at `0.000_000_008%` in either case, and the
intuition transfers cleanly past about `2¹⁰⁰`.

> If 256-bit security is unbreakable, 128-bit security is also
> unbreakable. The gap between them is enormous in absolute terms, but
> both numbers are on the same side of the impossibility line.

## And it gets worse for the attacker

The math above assumes a vanilla 12-word seed. Bitcoin gives users two
escape valves that make it dramatically worse:

- **BIP-39 passphrase ("25th word").** Adds an arbitrary user
  passphrase, hashed with PBKDF2 (2048 rounds of HMAC-SHA512). A
  reasonable passphrase adds another ~`2¹²⁸` effective bits. The
  attacker now has to brute-force a `5.4 × 10³⁹` seed *and* a
  `3.4 × 10³⁸` passphrase. The dollar cost exceeds every asset that
  has ever existed.
- **Multisig (e.g. 2-of-3).** Each co-signer holds an independent
  BIP-39 seed. Brute-forcing one is impossible. Brute-forcing two is
  impossible squared. With 3-of-5, you'd need to break three separate
  seeds at once, each individually larger than the count of atoms in
  the universe.

This is why "memorise twelve words" is the only key-management story
Bitcoin needs. The numbers do the work.

## What `seed-hunter` actually does

`seed-hunter` is a demo. It exists so you can run a real BIP-39
brute-forcer against the real Bitcoin network and watch how little
progress it makes.

- **Sweep mode** (the opening act) tries each word position in
  isolation: 12 × 2048 = 24,576 attempts. That's `2048¹² / 24,576`
  ≈ `2.2 × 10³⁵` times smaller than the actual keyspace. It finishes
  in minutes.
- **Walk mode** (the auto-transition that runs forever) tries every
  combination by walking a 12-digit base-2048 odometer. The dashboard
  shows the live cursor and the absolute count out of `5.4e+39`. You
  will not see the leftmost digit advance once at any rate, on any
  hardware.

After a few minutes of watching the cursor crawl, the impossibility
moves from abstract to concrete. The dashboard's `ETA full key` line
is always honest about your current throughput, and it always reads in
the order of `10²⁹` to `10³⁹` years.

## Further reading

- 3Blue1Brown — [**How secure is 256 bit security?**](https://www.youtube.com/watch?v=S9JGmA5_unY).
  The canonical visual explainer for the same intuition. The video
  targets `2²⁵⁶` specifically — directly applicable to 24-word
  BIP-39 seeds and to the secp256k1 private key Bitcoin uses under the
  hood. For the 12-word seeds `seed-hunter` iterates, the relevant
  number is the smaller `2¹²⁸`, but as the table above shows, both are
  on the same side of the impossibility line.
- [Bitcoin Fundamentals](https://chemaclass.com/blog/bitcoin-fundamentals/)
  and [How Bitcoin Works](https://chemaclass.com/blog/how-bitcoin-works/)
  for the broader context.
