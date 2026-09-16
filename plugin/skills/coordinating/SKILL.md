---
name: coordinating
description: Use when a Trellis card is already held by another actor, when several agents or sessions work one board at the same time, or when deciding whether a quiet lease may be taken over. Covers reading holder state, when stealing is justified, and keeping parallel workers from overwriting each other. Commands are in trellis:trellis.
---

# Coordinating on one board

Applies when more than one actor can touch the same card. If you are the only
one working the board, claim and carry on — none of this is needed.

## Read the holder before acting

`trellis agent ls` shows each actor's handle and when it was last seen. Combine
that with the card's lease:

| Holder state | Do this |
|---|---|
| Active, reachable handle | Coordinate through the harness when messaging is authorized |
| Active, no handle | Leave a `card note` if useful, then take other work |
| Quiet, lease expiring soon | Take other work and revisit — do not race the clock |
| Quiet, long idle | Confirm takeover is appropriate, then `claim --steal --reason "..."` |
| Lease expired | Claim normally |

A lease that is merely quiet is not abandoned. Coding, tests, and reading files
do not renew a lease, so a working agent looks idle. Prefer waiting or taking
other work over stealing; the `--reason` is read by the displaced actor and
should say what made takeover necessary.

## If you are displaced

Check ownership after any long pause before writing. If another actor now holds
the card, do not assume your old claim stands. Record what you completed as a
note and coordinate, rather than continuing to write to work someone else owns.

## Parallel workers

Workers sharing one `TRELLIS_AGENT` must each use a distinct, stable `--as`
suffix on every ownership-sensitive command — `claim`, `next`, `release`,
`renew`, `note`, `edit`, `move`. A worker that uses its suffix inconsistently
will appear as two actors and can steal from itself.

Preserve the suffix when finishing or parking work, or ownership will not
release cleanly.

## Handing off

Park unfinished work with a note that a cold reader can act on — what is done,
what is left, the next action — then `card release`. A released card with no
note is worse than no card: it claims the work exists but not where it stands.
