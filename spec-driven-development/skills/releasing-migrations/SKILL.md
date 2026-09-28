---
name: releasing-migrations
description: The user merges the draft migrations written during development into numbered migrations, once, right before tagging a release, and runs the migration checks.
disable-model-invocation: true
---

# Releasing migrations

During development every schema change is a draft in `<migrations dir>/draft/`,
with no version number, so parallel work never collides. At release the drafts
become numbered migrations, all at once.

Scripts live in `<this skill's directory>/../../scripts/`.

## 1. Plan

```bash
python3 <scripts>/release.py plan
```

It lists each numbered file it would write and the drafts it comes from. SQL
drafts in one directory merge into one file, in the order they were first
committed; any other draft takes a number of its own.

Show the plan to the user. The plan marks every SQL draft that changes data
(an UPDATE, INSERT, DELETE or MERGE): recommend it stay its own numbered
migration, and ask.

## 2. Apply

```bash
python3 <scripts>/release.py apply --name <what this release changes>
```

It writes the numbered files, then runs each `migration_checks` command from
`.sdd/config.json` in order: fresh install, upgrade from the last release,
rerun, integrity. If one fails, it removes what it wrote and keeps the drafts;
report the failure. If none is configured, tell the user nothing was checked.

## 3. Confirm

```bash
python3 <scripts>/release_check.py
```

It exits 1 while any draft remains. The user tags the release after it passes.

## Finish

Report the files written and the checks run. The user tags the release.
