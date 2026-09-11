# Turn a plan into cards

Use this workflow when the user asks to populate a board from a plan. Read the
plan and existing board first, so already tracked work is not imported again.

Map each actionable work item to a card. Preserve its scope, acceptance criteria,
and required validation in `body`; use a concise action-oriented `title`. Keep
independent tasks independent: `blocked_by` represents a real prerequisite, not
merely the order in which the plan lists tasks.

Use unique batch-local `id` values for dependencies between new cards. Existing
card references are also accepted. Check the board's columns and label vocabulary
before supplying those fields. Preserve specified priorities; omit unspecified
values rather than inventing urgency.

```bash
trellis card import <<'PLAN'
[
  {
    "id": "schema",
    "title": "Add the status migration",
    "body": "Preserve existing rows. Verify migration on a populated test database."
  },
  {
    "id": "consumer",
    "title": "Read the new status field",
    "body": "Handle existing rows and verify the consumer integration test.",
    "blocked_by": ["schema"]
  }
]
PLAN
```

Supported fields: `id`, `title`, `body`, `column`, `priority`, `labels`, `tags`,
`blocked_by`. Only `title` is required. Priorities are `urgent`, `high`, `normal`,
and `low`. `id` is an import-local handle, not the resulting card reference.

The import is one transaction: all cards and dependencies land, or none. Inspect
the returned cards to map local IDs to real references. If the command's outcome
is uncertain, inspect the board before retrying: importing the same payload again
can create duplicates. File input is supported with `trellis card import @plan.json`.
