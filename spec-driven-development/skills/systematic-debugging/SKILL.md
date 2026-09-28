---
name: systematic-debugging
description: Use when a bug is reported or a test fails unexpectedly, before proposing any fix - "debug this", "why is this failing", "it's broken", a regression, a flaky test, or a slow path. Registers a loop that goes red on the bug by script first, ranks falsifiable hypotheses, counts failed fixes by script and stops at three, bisects when a good version exists, and turns the fixed bug into a case.
---

# Systematic debugging

Scripts live in `<this skill's directory>/../../scripts/`. Redact secrets in
anything you show: write `<REDACTED>` in their place.

## 1. A loop that goes red

Everything else consumes this. Find one command that drives the bug's code
path and asserts the user's exact symptom: a failing test at the seam that
reaches it, a curl against a running server, a CLI run diffed against a known
good output, a replayed payload, a throwaway harness. Make it fast and
deterministic; for a flaky bug, raise the reproduction rate until it is
debuggable.

```bash
python3 <scripts>/repro.py record "<command>"
```

It is refused unless the command fails now. No registered loop, no hypotheses.
If you cannot build one, say so, list what you tried, and ask the user for an
environment, a captured artifact, or permission to instrument.

## 2. Reproduce and minimise

Cut inputs, callers, config and steps one at a time, rerunning the loop after
each cut, until every remaining part is needed for it to fail.

## 3. Hypotheses

List three to five, ranked, each falsifiable: "if X is the cause, changing Y
makes the bug disappear". Show the list to the user, but do not wait on it.

If a known good version exists, bisect instead of guessing:
`python3 <scripts>/repro.py bisect <id> <good-ref>`.

## 4. Instrument

Each probe tests one prediction. Tag every debug log `[DEBUG-` plus four hex
digits and `]`, the same tag throughout; verify blocks while any remains.

## 5. Fix

Rerun the loop after every attempted fix: `python3 <scripts>/repro.py run <id>`.
A red run on a changed tree counts as a failed fix. At three it prints STOP:
question the design instead of trying a fourth fix, and ask the user.

When it is green, add the bug to its story's spec as a new unexpected case, and
name the regression test with that case's ID. Remove every tagged log.
State the hypothesis that proved right in the commit message.

## Next

Continue with `/sdd:verifying-before-done`.
