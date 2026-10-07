---
name: team-workflow
description: Use only when the user chose team at the case approval, or explicitly asks for several agents to build an approved spec. Needs Trellis - cases become cards, workers claim them and run only their own tests, one worker owns the migrations, and the coordinator verifies once at integration.
---

# Team workflow

Needs Trellis: `python3 <this skill's directory>/../../scripts/store.py` must
answer `store: trellis`. If it does not, stop and offer solo. Operate the board
as the `trellis` skill says, and handle contested claims as
`trellis:coordinating` says.

## Coordinator

1. Group the approved cases so each group can be built without another group's
   unfinished code. One card per group: `trellis card new --title "PIN-C1..C3: <what>"`,
   with the case IDs, the files the group may touch, and `blocked_by` for any
   group it waits on.
2. Give every migration draft to one card. No other worker writes a migration.
3. Dispatch one worker per unblocked card, each in a worktree of its own, with
   the card ref and this skill's worker section.
4. When every card is done, integrate, then continue with
   `/sdd:verifying-before-done`. Blockers go back to the card whose files they name.

## Worker

- Work in a worktree on a branch of your own, and start the build there:
  `python3 <this skill's directory>/../../scripts/build.py start <KEY>`, with
  the story's key. It refuses in the main checkout.
- Claim the card: `trellis card claim <ref>`, and keep it with `trellis card renew`.
- Build its cases as `/sdd:solo-building-from-cases` says, touching only the
  card's files.
- Run only the card's tests (select them by case ID, for example
  `go test -run 'PIN_C[1-3]'`). The whole suite runs once, at integration.
- Log progress and evidence with `trellis card comment <ref> --body "..."`,
  then move the card to done.

## Next

After integration, the coordinator continues with `/sdd:verifying-before-done`.
