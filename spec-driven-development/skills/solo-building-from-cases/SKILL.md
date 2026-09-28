---
name: solo-building-from-cases
description: Use when a story's cases are approved and one agent builds them - the user chose solo at the approval, or asks to implement an approved spec. Builds each case test-first with the case ID in the test name, never edits an approved case's test to make it pass, and writes migrations as drafts.
---

# Building from the cases, solo

Scripts live in `<this skill's directory>/../../scripts/`.

## For each case, in order

1. Write its test, with the case ID in the test's name (`PIN_C2` or `PIN-C2`).
2. Run it and watch it fail. Quote the failing line. A test never seen failing
   has not proved it can catch anything.
3. Write the smallest code that passes it.
4. Run the tests you touched. The whole suite runs once, in verify.
5. Commit. From its first commit after approval, a case's test is locked:
   verify reports any later change as a blocker.

Never edit an approved case's test to make it pass. If the case is wrong, stop
and tell the user: that is a decision change.

## While building

- Comment only a reason the code cannot show, such as a workaround or an
  outside constraint. Decisions live in the spec, not in comments.
- A schema change is a draft: `<migrations dir>/draft/<slug>.<ext>`, never a
  numbered file. Numbers are assigned at release (`/sdd:releasing-migrations`).
- A test failing for a reason you do not understand: switch to
  `/sdd:systematic-debugging` rather than guessing.

## Next

When every case passes, continue with `/sdd:verifying-before-done`.
