---
name: writing-spec
description: Use before building a feature, story or change that needs agreement first - the user asks to plan, spec, design, scope or "grill" something, or the work touches data, schema, an API, or several parts at once. Grows one spec per story through batched question rounds, each question with a recommended answer, and records the settled decisions. Uses Trellis as the store when it is installed. Not for a one-line fix or a spike.
---

# Writing the spec

The tests are the ground the work stands on, and the tests come from the spec.
This skill settles the decisions; `/sdd:writing-cases` turns them into cases.

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

## Next

When the frontier is empty, continue with `/sdd:writing-cases`.
