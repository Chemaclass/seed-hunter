# The math, or: why your seed phrase is safer than your house

A standard 12-word BIP-39 mnemonic is one of:

```
2048¹²  =  5,444,517,870,735,015,415,413,993,718,908,291,383,296
        ≈  5.4 × 10³⁹  combinations
```

That's a `5` followed by **39 zeros**. People hear "12 words from a list of
2048" and their brain thinks "ok that's like guessing a long password,
maybe doable with a really fast computer". This document is here to fix
that intuition.

## Try to picture 5.4 × 10³⁹

You can't. Nobody can. Here is what's actually being asked of you:

| Quantity                                                  | Order of magnitude |
| --------------------------------------------------------- | ------------------ |
| Grains of sand on every beach on Earth                    | ~7.5 × 10¹⁸        |
| Stars in the **observable universe**                      | ~10²⁴              |
| Atoms in **a human body**                                 | ~7 × 10²⁷          |
| **Total SHA-256 hashes Bitcoin has ever computed**, ever  | ~10²⁹              |
| Atoms in **planet Earth**                                 | ~10⁵⁰              |
| Atoms in the observable universe                          | ~10⁸⁰              |
| **BIP-39 12-word combinations**                           | **~5 × 10³⁹**       |

The keyspace of a single seed phrase is **~10¹⁰ × the entire planet
Earth's lifetime hash output of every Bitcoin miner that has ever
existed**. It's larger than the number of seconds since the Big Bang
**raised to the second power**. It's larger than the number of grains of
sand in every beach on Earth, **multiplied by the number of stars in the
observable universe**.

If you took every atom in your body and turned each one into a separate
universe, and every atom in every one of those universes into another
universe, you would *still* be roughly 5 orders of magnitude short of
counting BIP-39 phrases.

## The patient burglar

Imagine the world's most patient burglar. They will check **one trillion
seed phrases per second** (10¹² candidates/s) — about a billion times
faster than this educational tool. They started the moment the Big Bang
happened, 13.8 billion years ago, and they have been working
uninterruptedly ever since. No bathroom breaks, no holidays, no Y2K.

How far have they got?

```
13.8 × 10⁹ years × 365.25 × 24 × 3600  ≈  4.4 × 10¹⁷ seconds
4.4 × 10¹⁷ s × 10¹² candidates/s        ≈  4.4 × 10²⁹ candidates checked
```

That's `4.4 × 10²⁹` out of `5.4 × 10³⁹`. They have completed:

```
  4.4 × 10²⁹
  ──────────  ≈  8 × 10⁻¹¹
  5.4 × 10³⁹
```

…**0.000_000_008%** of the search. After 13.8 billion years. At a trillion
guesses per second.

They will need another **~10²² × the current age of the universe** to
finish. The Sun will go red giant, swallow the Earth, and burn out before
our burglar gets to even 1% of 1% of 1% of the way through.

## "But what about quantum computers?"

Grover's algorithm gives a quadratic speedup on unstructured search, so
the effective security drops from `2¹³²` to roughly `2⁶⁶`. That sounds
worse, but `2⁶⁶ ≈ 7 × 10¹⁹` is still **galaxies of ridiculous**:

- A **perfect quantum brute-forcer** doing 1 billion Grover iterations per
  second would need ~2300 years to finish a single seed.
- We do not have such a quantum computer. We have machines that, on a
  good day, can factor 21. We have not, in fact, broken Bitcoin.
- Real-world quantum is also gated on coherence time, error correction,
  and physical-qubit overhead. Current best estimates put a Grover-class
  attack on a 256-bit key at *millions* of physical qubits. The largest
  publicly demonstrated systems have ~1000.

You will see the headline "QUANTUM COMPUTER BREAKS BITCOIN" exactly when
gravity stops working.

## "But what about a superintelligent AI 10,000× smarter than today?"

Make it 10¹⁰ × smarter. Make it 10¹⁰⁰ × smarter. The AI still has to
*physically check candidates*, and physical computation has a hard floor
called **Landauer's limit**: erasing one bit of information costs at
least `kT ln 2` joules of energy. At room temperature that's ~3 × 10⁻²¹
joules per bit-flip.

To merely **count** through 2¹³² candidates (no derivation, no SHA-256,
no PBKDF2 — just incrementing a counter) would dissipate at least

```
2¹³² × 3 × 10⁻²¹ J ≈ 1.6 × 10¹⁹ J
```

The total annual energy output of the Sun is about `1.2 × 10³⁴ J`. The
counter alone is fine on solar power. But now do the **actual** work: for
each candidate, run PBKDF2 (2048 rounds of HMAC-SHA512), derive an HD
key, hash to an address, query the blockchain. That's ~2¹⁸ bit
operations per candidate, conservatively. The total energy floor becomes:

```
2¹³² × 2¹⁸ × 3 × 10⁻²¹ J ≈ 4 × 10²⁴ J
```

You need **half the Sun's total annual energy output**, every year, for
**every key**. And that's just to thermodynamically *afford* the
computation, not to actually win — winning requires you to actually find
the right one, which on average takes half the keyspace.

A brute-force attack on a single BIP-39 seed is forbidden not by
"computers are slow" but by **the laws of thermodynamics**. No amount of
intelligence — biological, artificial, alien, divine — gets around `kT
ln 2`.

> *"There's no algorithmic shortcut. There's no clever AI. There's no
> quantum miracle. There's just the second law of thermodynamics, and the
> second law doesn't care how smart you are."*

## And we haven't even started yet

The real keyspace is **even bigger** than `2¹³²` because BIP-39 has two
escape valves built into it:

- **BIP-39 passphrase ("25th word")** — adds an arbitrary user
  passphrase hashed with PBKDF2 (2048 rounds of HMAC-SHA512). A
  reasonable passphrase adds another ~2¹²⁸ effective bits, taking the
  cost from "absurd" to **"absurd squared"**. Now you need to brute-force
  a `5.4 × 10³⁹` seed AND an additional `3.4 × 10³⁸` passphrase
  combinations. The dollar cost of attacking such a wallet exceeds the
  value of every asset that has ever existed, all at the same time.

- **Multisig (e.g. 2-of-3)** — every co-signer holds an independent
  BIP-39 seed. Brute-forcing one is impossible. Brute-forcing two
  simultaneously is **impossible × impossible**, a number large enough
  that the universe runs out of digits to write it. With 3-of-5 multisig
  the math becomes literally meaningless: you would need to break **three
  separate seeds at once**, each individually more secure than every atom
  in the universe.

This is why "memorize twelve words" is the only key-management story
Bitcoin needs. **The numbers do the work for you.** You are not
protecting your seed against a fast computer. You are protecting it
against the heat death of the universe, and the universe is on your
side.

## So what's `seed-hunter` actually for?

It's a **demo**. It exists so you can watch a real BIP-39 brute-forcer
running on real hardware against the real Bitcoin network and see, in
real time, how much progress it makes:

- **Sweep mode** (the default opening act) tries each word position in
  isolation: 12 × 2048 = **24,576 attempts**. That's
  `2048¹² / 24576 ≈ 2.2 × 10³⁵` times smaller than the actual keyspace.
  It finishes in minutes and gives you a satisfying "I tried something
  hard and it completed" feeling, before reality sets in.
- **Walk mode** (the auto-transition that runs forever) tries every
  combination by walking a 12-digit base-2048 odometer. The dashboard
  shows you the live cursor and the absolute count out of `5.4e+39`. You
  will not see the leftmost digit advance even once in **the lifetime of
  the universe** at any rate, on any hardware, ever.

After a few minutes of watching the cursor crawl, the impossibility moves
from the abstract ("39 zeros, ok") to the tactile ("oh, I'm watching it,
and it is *not happening*"). That's the entire purpose of this project.
The dashboard's `ETA full key` line is always honest about your current
throughput, and it always reads in the order of `10²⁹` to `10³⁹` years.

You can throw all the cores in the universe at it. You will not finish.
That's not a limitation of the tool. That's BIP-39 working as designed.

## Further reading (and watching)

If you'd rather *see* this kind of math than read it, watch
[**3Blue1Brown — How secure is 256 bit security?**](https://www.youtube.com/watch?v=S9JGmA5_unY).
It walks through the same intuition with much better animations than any
text document could pull off, with concrete "what if every computer ever
built worked together" thought experiments. The video is about a 256-bit
keyspace specifically (the size of an ECDSA private key), but the
intuition applies directly to BIP-39's 132-bit seed entropy: both are
large enough that the universe runs out before the search does, and the
gap between "huge" and "actually unbreakable" is much smaller than most
people think.

After watching the video and then watching `seed-hunter`'s walk-mode
cursor crawl for a few minutes, the lesson lands twice: once in your
head, once in your gut.
