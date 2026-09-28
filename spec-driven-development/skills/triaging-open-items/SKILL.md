---
name: triaging-open-items
description: The user goes through the questions and findings waiting in the open-items queue - unanswered questions and verify findings, oldest and most severe first - and decides each one.
disable-model-invocation: true
---

# Triaging open items

The queue holds questions the user was asked but never answered, and findings
verify recorded. The user decides each item; you recommend.

Scripts live in `<this skill's directory>/../../scripts/`.

## 1. Read the queue

```bash
python3 <scripts>/open_items.py list --json
```

Reading already closes what is settled: a question a spec decision answers
(`Answers: <id>`), and a finding verify no longer reproduces. Each open item has
a state:

- **open**: waiting.
- **drifted**: a file it names changed since it was opened. Check whether it
  still holds before recommending anything.
- **aged**: older than 14 days with nothing changed. Still valid.

## 2. One batch

Blockers first, then decide, then notes, then questions; drifted and aged items
grouped at the end. One line each, with a recommendation:

```
I1. [blocker] <text> (opened <date>) — recommended: fix, <one line why>
I2. [question] <text> (drifted: <file> changed) — recommended: dismiss, <why>
```

Actions: **fix** (do it now, then verify), **card** (make it Trellis work),
**decision** (write a spec decision ending `Answers: <id>`), **accept** (it
stands as it is), **dismiss** (not a problem). "All as recommended" is a
complete answer.

## 3. Apply the answers

- fix: do the work, then `python3 <scripts>/verify.py`.
- card: `trellis card new --title "..." --body "..."`, then resolve with `--how card`.
- decision, accept, dismiss:
  `python3 <scripts>/open_items.py resolve <id> --how <action> --note "<why>"`.
  Accepted and dismissed items never come back.

## Finish

`python3 <scripts>/open_items.py compact`, then report what was decided.
