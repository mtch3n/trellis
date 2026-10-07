---
name: verifying-before-done
description: Use before saying work is done, fixed, passing or ready - before a final report, a commit, a PR, or moving a card to done. Runs one verify script whose probes decide - the whole suite, spec and test consistency, approved tests unchanged, no leftover debug logs, draft migrations - and requires every blocker fixed first.
---

# Verifying before done

```bash
python3 <this skill's directory>/../../scripts/verify.py
```

- It runs the suite from `.sdd/config.json` (`"test"`), `spec_check`, the
  approved-test check, the debug-log check and the migration-draft check.
- A result is cached by the working tree's hash. Rerunning without a change
  costs nothing, and other agents reuse it.
- Every problem goes to the open-items queue with a severity:
  - **blocker**: a probe contradicts the work. Fix it, and run verify again.
    Do not say done while one stands.
  - **decide**: a choice for the user. Leave it in the queue.
  - **note**: worth knowing. Leave it in the queue.
- With no blocker and a change, the shadow runs in the background: one model
  run proposes problems a script could not catch, a second checks each against
  the code. Do not wait for it. When the Stop hook lists a confirmed shadow
  finding, check it against the code yourself, then either fix it and run
  `python3 <this skill's directory>/../../scripts/open_items.py resolve <id> --how fix --note "<what the fix changed>"`,
  or run `... resolve <id> --how dismiss --note "<why it is not a problem>"`.
  The user sees how each one ended.

Every claim needs its probe, run now, not remembered:

| Claim | Proved by |
|---|---|
| Tests pass | verify's `test` probe on this tree, not a narrower command |
| The cases are covered | verify's `spec` probe: no `untested` for an approved story |
| An approved case still holds | verify's `approved-tests` probe |
| The bug is fixed | `repro.py run <id>` green, and its regression test named with the case ID |
| The migration is safe | the `migration_checks` of `release.py apply`; before release, only a draft |
| No debug leftovers | verify's `debug-tags` probe |

A claim with no probe is not a fact: say it is unverified. If you say the
work is done or the tests pass with no passing verify for this tree, the Stop
hook sends you back to run it. Report the verify
command and its result as it printed it.

If there is no `.sdd/config.json` with a `"test"` command, verify says so as a
decide item. Offer the user one: `{"test": "<the project's test command>"}`.

## Next

Report done. If decide or note items are waiting, tell the user they can go
through them with `/sdd:triaging-open-items`.
