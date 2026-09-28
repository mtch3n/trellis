---
name: writing-cases
description: Use when a story's decisions are settled and need turning into testable cases - after /sdd:writing-spec, or when the user asks for cases, acceptance criteria or a test plan for a spec. Drills each decision down to expected and unexpected cases, checks the spec with a script, and asks in one message for the only approval the work needs, together with the choice of building solo or as a team.
---

# Writing the cases

Scripts live in `<this skill's directory>/../../scripts/`. Find the store with
`python3 <scripts>/store.py`, as `/sdd:writing-spec` does.

## 1. Drill down, decision by decision

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

## 2. Write the cases

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

## 3. Check the spec

```bash
python3 <scripts>/spec_check.py specs/<story>              # files
python3 <scripts>/spec_check.py --trellis specs/<story>    # Trellis
```

Before approval, `untested` is expected. Fix every `uncovered`, `collision`,
`bad_ref`, `duplicate` and `no_unexpected` first, or tell the user why one stands.

## 4. One message: approve, and solo or team

Recommend solo or team from facts: how many case groups are independent,
whether migrations are involved, whether the store is Trellis. Team needs
Trellis: without it, do not offer team and say that it needs Trellis.

```
Cases (N: a expected, b unexpected)
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
