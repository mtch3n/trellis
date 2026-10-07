---
name: solo-building-from-cases
description: Use when a story's cases are approved and one agent builds them - the user chose solo at the approval, or asks to implement an approved spec. Builds each case test-first with the case ID in the test name, never edits an approved case's test to make it pass, and writes migrations as drafts.
---

# Building from the cases, solo

Scripts live in `<this skill's directory>/../../scripts/` (`<scripts>` below).

## Before the first case

1. Work in a worktree on a branch of its own: `EnterWorktree`, or
   `git worktree add <path> -b <branch>` and work there. The main checkout stays
   free for other sessions.
2. Start the build there: `python3 <scripts>/build.py start <KEY>`, with the
   story's key (`PIN`). It refuses outside a worktree, and `red.py` refuses a
   case whose story was not started on the branch it runs on.

## For each case, in order

1. Write its test, with the case ID on the line that defines it (`def test_PIN_C2`,
   `func TestPin_PIN_C2`, `test('PIN-C2 …', …)`). An ID only in a comment is not a test.
2. Record it failing: `python3 <scripts>/red.py PIN-C2 "<command that runs this test>"`.
   The command must name the case ID, and the output must show that case failing:
   a passing run, another case failing, or a run where no test ran is refused. A
   test never seen failing has not proved it can catch anything; verify blocks
   an approved case with no red record. A guard case, whose test passes before
   any code exists (something must stay silent or must not happen), is
   recorded after its code: break the code it guards for a moment, record the
   red run, and put the code back.
3. Write the smallest code that passes it.
4. Run the tests you touched. The whole suite runs once, in verify.
5. Commit. From its first commit after approval, a case's test is locked:
   verify reports any later change as a blocker.

Never edit an approved case's test to make it pass. If the case is wrong, stop
and tell the user: that is a decision change.

## While building

- Write the whole thing. A placeholder where the work should be (`// ...`,
  `TODO: implement`, `panic("not implemented")`) is blocked when written; a
  deliberate one carries `sdd: allow-placeholder` on its line.
- Do not stop to ask whether to continue while cases are left: keep going. Ask
  only what `writing-spec` would ask (intent or scope, an external contract,
  hard to reverse, a weak recommendation); decide the rest and say so.
- Comment only a reason the code cannot show, such as a workaround or an
  outside constraint. Decisions live in the spec, not in comments.
- A schema change is a draft: `<migrations dir>/draft/<slug>.<ext>`, never a
  numbered file. Numbers are assigned at release (`/sdd:releasing-migrations`).
- A test failing for a reason you do not understand: switch to
  `/sdd:systematic-debugging` rather than guessing.

## Next

When every case passes, continue with `/sdd:verifying-before-done`.
