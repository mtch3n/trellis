---
name: writing-knowledge
description: Use when deciding whether a finding is worth recording and where it belongs - after debugging something non-obvious, when a measurement or decision would otherwise be re-derived, when an approach failed and the reason matters, or when the user says to write something down. Covers what each template asks for and when a card comment or project memory is the better home instead.
---

# Writing knowledge

Two questions, in order: does this deserve to be written down at all, and where
does it go. Most candidates fail the first. Commands are in `trellis:trellis`.

## Does it deserve an entry

Write when a future session would otherwise repeat meaningful work. That is the
whole test.

Do not write: a restatement of what the code plainly says, a summary of what you
just did, anything already recorded and still accurate, or a fact you have not
verified. An entry that is wrong is worse than no entry, because the next
session will trust it.

## Where it goes

| The thing | Home | Why |
|---|---|---|
| Durable fact about *this* repo — a decision, a trap, a measurement | The project vault | Searchable, linkable, survives a clear |
| Progress, evidence, what is left on a specific task | A card comment | Belongs to the work, not the vault |
| How the user wants to work, across every project | Project memory / CLAUDE.md | Already the working store; do not migrate it |
| Fact useful in several repos | The project vault, then `vault nominate` | You nominate; a human promotes |
| Still a hypothesis | Nowhere yet | Wait until it is verified |

Project memory already holds hundreds of working entries. Do not copy them onto
the board and do not start a parallel vault for the same class of fact. Split
knowledge is worse than either store alone — check the incumbent first, and add
to it when the finding belongs there.

## What each template asks for

`trellis template ls` is the live list; `template show <name>` gives
one template's rules and skeleton. What ships:

| Template | Must include |
|---|---|
| `decision` | The context, the options considered, the decision, its consequences — and at least one source |
| `finding` | The fact, the evidence, its scope — and at least one source |
| `research` | The question, the method, the findings, the conclusion |
| `runbook` | When to use it, preconditions, steps, verification, rollback |
| `glossary` | One `## Terms` table: Term, Means, Not. `trellis:keeping-glossary` writes it |
| `reference` | Nothing fixed; it only warns |

`decision`, `finding` and `glossary` refuse a write that does not meet them, so
choose the template that fits what you actually have. An entry created without
`--template` has no shape to keep, which is the right choice for a note that
belongs to none of the six.

Prefer evidence and constraints over a transcription of structure. Separate what
you observed from what you infer. Include source paths, commands, versions, and
dates wherever they affect whether the entry still applies — architectural
invariants change too.

Write the summary yourself and make it specific. "Notes on caching" tells a
future session nothing; "Vector index rebuilds on every write above 10k rows"
tells it whether to open the entry.

## Linking

Write `[[wikilinks]]` to related entries as you go, including ones that do not
exist yet. An unresolved link is a stub, not an error — it marks the gap, and
`vault lint` reports it later. This is how a vault accumulates without anyone
planning it.

A bare `[[slug]]` means this project, and it may name a directory:
`[[ops/db/rollback]]`. Its last segment alone, `[[rollback]]`, also finds it
while that leaf is unique; lint reports `ambiguous_link` once it is not. To link
another project's entry or a global one, write its address:
`[[/OTHER/vault/runbook]]`, `[[/GLOBAL/vault/conventions]]`. The form
`[[KEY/slug]]` is not a cross-project link — it reads as a directory in this
project, and lint reports it as a stub.

## Pinning

Pin only when *not* knowing the fact causes a wrong action. Pinned recaps are
injected into every session's brief and compete for a hard budget, so a pin that
is merely interesting taxes every future session.

Write the recap yourself, and keep it to the consequence: what the next session
would get wrong without it. Editing an entry marks its recap stale — refresh it
or unpin it rather than leaving a confidently wrong summary in the brief.
