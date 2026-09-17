---
name: coordinating
description: Use when another actor has claimed a Trellis card, when several agents or sessions work one board at the same time, or when deciding whether a quiet claim may be stolen. Covers reading the claimant's state, when stealing is justified, and keeping parallel workers from overwriting each other. Commands are in trellis:trellis.
---

# Coordinating on one board

Applies when more than one actor can touch the same card. If you are the only
one working the board, claim and carry on — none of this is needed.

## Read the claim before acting

`trellis agent ls` shows each registered actor's handle and when it was last
seen. Combine that with the card's claim:

| Claimant state | Do this |
|---|---|
| Active, reachable handle | Coordinate through the harness when messaging is authorized |
| Active, no handle | Leave a `card comment` if useful, then take other work |
| Quiet, claim expiring soon | Take other work and revisit — do not race the clock |
| Quiet, long idle | Confirm takeover is appropriate, then `claim --steal --reason "..."` |
| Claim expired | Claim normally |

A claim that is merely quiet is not abandoned. Only `card renew` and the
claimant's own `card edit` push the expiry out — a comment does not, and neither
does coding, running tests or reading files — so a working agent looks idle, and
one that only comments looks idle too. Prefer waiting or taking other work over
stealing; the `--reason` is read by the displaced actor and should say what made
takeover necessary.

Trellis tells the two cases apart for you. A write refused with `contention`
means another actor's claim is live: its message names the claimant, and the
JSON error's `detail` carries the claimant's agent record (`claimed_by`, with
its handle and last-seen time) and a `recommended_action`. One
refused with `not_yours` means nobody holds the card, or the claim has already
expired, so claiming it is enough.

## If you are displaced

Check the claim after any long pause before writing. If another actor has
claimed the card, do not assume your old claim stands. Record what you completed
as a comment and coordinate, rather than continuing to write to work someone
else has claimed.

## Parallel workers

Workers sharing one `TRELLIS_AGENT` must each use a distinct, stable `--as`
suffix on every command that records or checks a claimant — `claim`, `next`,
`release`, `renew`, `comment`, `edit`, `move`. A worker that uses its suffix
inconsistently will appear as two actors and can steal from itself.

Preserve the suffix when finishing or parking work, or the claim will not
release cleanly.

## Handing off

Park unfinished work with a comment that a cold reader can act on — what is done,
what is left, the next action — then `card release`. A released card with no
comment is worse than no card: it says the work exists but not where it stands.
