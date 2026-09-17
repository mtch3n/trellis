---
name: when-to-use-trellis
description: Use when deciding whether work belongs on a Trellis board - starting a task with three or more steps, ending a session with work unfinished, about to clear or compact context mid-task, resuming a repository after a gap, or carrying a finding that the next session would otherwise re-derive. Decides whether to reach for Trellis and authorizes writing to the board. How to run the commands is in trellis:trellis.
---

# When to use Trellis

This skill decides *whether* the board is the right place for something.
`trellis:trellis` covers *how* to run the commands. Read that one once you have
decided to act.

Trellis earns its cost when state must survive something — a context clear, a
session boundary, a handoff to another agent. It is overhead for work that
begins and ends in one conversation. Most work is the second kind; say so and
move on rather than filing a card to look diligent.

## Write authorization

An agent may create and update cards and vault entries in the situations listed
below, without asking first. This is a standing authorization and it is the
point of this skill: a board no one may write to stays empty.

Nominating an entry for the global vault is also yours: `vault nominate` is the
argument that an entry belongs beyond this project, and making that argument is
the agent's job. Promoting, demoting and verifying a global entry are the
human's: the CLI refuses all three whenever `TRELLIS_AGENT` is set, and asks a
human for a terminal and the slug retyped.

The authorization does not extend to: claiming work the user did not ask you to
take, moving another actor's cards, archiving, or deleting. Those need the user.
Reading the board is always allowed and needs no authorization.

## Triggers

| Situation | Do this |
|---|---|
| Task has 3+ steps, or will span more than one sitting | One card per step; `card relate <card> blocked-by <card>` for real dependencies |
| Session ending with work unfinished | One card: what is done, what is left, the next action |
| About to `/clear` or `/compact` mid-task | Same as above, *before* clearing — after is too late |
| Returning to a repository after a gap | Read the brief first; `card show` anything open before starting |
| Finding the next session would re-derive | `vault new`; pin it only if not knowing it causes a wrong action |
| Another agent needs to pick this up | Card with enough context to start cold, then `card release` |
| User says "track this", "for later", "don't forget" | Take it literally — a card or a vault entry, now |

The clear-and-compact trigger is the one that pays for itself most often, and
the one most easily missed: the moment you notice the context is getting long is
the moment to write the card, not after.

## When not to use it

- A question answered in one reply.
- A single-file edit you will finish in this conversation.
- Exploration with no decision yet — wait until there is something worth
  recording. A card per idea turns the board into noise.
- Anything already captured elsewhere and still accurate. Do not copy a fact
  onto the board to feel thorough; duplicated state is worse than none.

## Cost

Writes are cheap; a card is one command. Injection is not — the board brief is
read at the start of every session, so a board crowded with stale cards taxes
every future session. Close what is done. A card that will never be picked up
should be archived, not left to accumulate.
