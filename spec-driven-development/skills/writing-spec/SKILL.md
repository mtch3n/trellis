---
name: writing-spec
description: Use before building a feature, story or change that needs agreement first - the user asks to plan, spec, design, scope or "grill" something, or asks for cases, acceptance criteria or a test plan, or the work touches data, schema, an API, or several parts at once. Grows one spec per story through batched question rounds with recommended answers, drills each decision down to expected and unexpected cases, and asks in one message for the only approval, with the choice of building solo or as a team. Uses Trellis as the store when it is installed. Not for a one-line fix or a spike.
---

# Writing the spec

The tests are the ground the work stands on, and the tests come from the spec.
This skill settles the decisions, turns them into cases, and gets the cases approved.

Prose decays: text nobody re-reads goes stale while still looking true. So the
spec holds only what something re-reads: decisions later work must respect, and
cases that tests execute. Everything else stays in the conversation.

Scripts live in `<this skill's directory>/../../scripts/`.

## 0. Route the request

Say which, in one line, before anything else:

- **No spec needed**: a fix or change with no decision in it. Say so and leave
  this flow; just do the work.
- **Extend**: it belongs to an existing story. Grow that spec.
- **New**: a story of its own.
- **Split**: several stories. Name them, and start with the one the others need.

## 1. Find the store and the story

Run `python3 <scripts>/store.py` and use its answer. Do not decide the store yourself.

- **`store: trellis`**: the spec lives in the Trellis vault under `specs/<story>/`,
  one entry per segment. Pass bodies on stdin so no scratch file lands in the
  repository: `trellis vault new --in specs/<story> --title "..." --body - <<'EOF'`,
  and change one with `trellis vault edit <entry> --body - --if-version <n>`.
  Run `trellis search "<topic>"` for related specs. Operate the CLI as the
  `trellis` skill says.
- **`store: files`**: the spec lives at `specs/<story>/*.md`, or `specs/<story>.md`
  while it is one segment.

If a spec for this story exists, grow it in place. Never start a second spec
for the same story. History is in git or in Trellis revisions, not in the text.

## 2. Pick the entry mode

Say which, in one line, so the user can override it.

- **Small**: one behaviour, or an addition to an existing story. One question
  round at most.
- **Big**: a new story, several parts, or data, schema or API changes. Work
  the rounds below until nothing is open.

## 3. Question rounds

Map the work as a tree of decisions. The **frontier** is every decision whose
prerequisites are settled. Each round, ask the whole frontier in one message,
in exactly this form (a hook reads it, so unanswered questions are not lost):

```
Q1. <question> — recommended: <your answer, one line why>
Q2. ...
```

- A question that depends on an open question waits for a later round. Never
  hold a whole round for one fact; ask what does not depend on it.
- Facts are yours to find: read the code, run a command, send a sub-agent.
  Never ask the user something you can look up.
- Decisions are the user's. Recommend, then wait. "All as recommended" is a
  complete answer: write every recommendation as a decision.
- Stop when the frontier is empty.

After each round, write what was settled into the spec before the next round.
If the answered question is in the open-items queue
(`python3 <scripts>/open_items.py list`), end the decision with `Answers: <id>`.

## 4. The spec's shape

One story, split into segments once it passes about 150 lines or spans areas:
an index segment linking the others (`[[specs/<story>/<area>]]` in Trellis, a
relative link in files), and one segment per area. Pick a short uppercase story
key (`PIN`); every ID starts with it.

```markdown
# <Story or area>

Status: cases pending approval

Intent: <two to four lines: what it is for, and what it is not for>

## Decisions
- **PIN-D1** <the decision>. Why: <one line>. Governs: <what it constrains, comma-separated>.
- **PIN-D4** <new decision>. Why: <...>. Governs: <...>. Supersedes: PIN-D2.

## Cases
| ID | Covers | Kind | Case |
|---|---|---|---|

## Open
- <question not yet settled>
```

- **Governs** names what a decision constrains: a column (`cards.ref`), an
  endpoint (`api:/cards`), a file, a behaviour. Two active decisions governing
  the same thing collide unless one supersedes the other.
- When a decision changes, add one that supersedes it; do not rewrite the old one.
- **Open** holds questions only. No narrative, no progress log, no plan file.

When the frontier is empty, turn the decisions into cases.

## 5. Drill down, decision by decision

For every active decision, ask:

- **Expected**: when the decision holds, what does the user see? Write it as
  Given / when / then.
- **Unexpected**: walk this list, every item, every decision:
  - bad input
  - missing data (the thing does not exist, or is empty)
  - permissions (not allowed, or claimed by someone else)
  - concurrency (two actors at once)
  - interruption and retry (it stops halfway, then runs again)
  - for a schema change: upgrade from the current schema, rerun, old data

  Each item becomes a case, or you say in one line why it does not apply.
  A decision with no unexpected case at all ends its bullet with
  `Unexpected: n/a — <reason>`.

Pick the key examples, not every combination. More than four cases per
decision is a sign to merge them: spec_check reports it as `crowded`.

## 6. Write the cases

Under `## Cases` in the segment they belong to. **Covers** names the decisions
a case exercises:

```markdown
| ID | Covers | Kind | Case |
|---|---|---|---|
| PIN-C1 | PIN-D1 | expected | Given a card, when it is pinned, then it sorts first |
| PIN-C2 | PIN-D1 | unexpected | Given a card id that does not exist, when it is pinned, then exit 3 and nothing changes |
```

- Kind is `expected` (normal use) or `unexpected` (anything from the list above).
- Prefer the seams tests already use; a case needing a new seam says so.
- Do not copy test names into the spec. A test is found by the case ID in its name.

## 7. Check the spec

```bash
python3 <scripts>/spec_check.py specs/<story>              # files
python3 <scripts>/spec_check.py --trellis specs/<story>    # Trellis
```

Before approval, `untested` is expected. Fix every `uncovered`, `collision`,
`bad_ref`, `duplicate` and `no_unexpected` first, or tell the user why one stands.

## 8. One message: approve, and solo or team

Recommend solo or team from facts: how many case groups are independent,
whether migrations are involved, whether the store is Trellis. Team needs
Trellis: without it, do not offer team and say that it needs Trellis.

```
Cases (N: a expected, b unexpected; per decision: PIN-D1 3, PIN-D2 2)
  PIN-C1 PIN-D1 expected    <one line>
  ...
Approve these cases?
Build — recommended: solo (<reason from the facts>)
  solo: I build them in order, test-first
  team: cases become Trellis cards worked by several agents
```

This is the only approval before code. When the user approves, write the
status line under the title, with today's date and the short HEAD commit:
`Status: cases approved <date> at <commit>`. From then on, a case changes only
with the user: a wrong case is a decision change.

## Next

On approval, continue with `/sdd:solo-building-from-cases` for solo, or
`/sdd:team-workflow` for team.
