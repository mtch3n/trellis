# sdd — spec-driven development

Spec-first work that stands on tests. A separate plugin from `trellis`: it uses
Trellis as its store when the CLI is installed and the repository is a Trellis
project, and the repository's `specs/` directory otherwise.

```bash
claude plugin marketplace add mtch3n/trellis   # the marketplace that ships trellis
claude plugin install sdd@trellis
```

## Flow

```
writing-spec → writing-cases → (approve cases, pick solo or team)
                                  ├─ solo-building-from-cases ─┐
                                  └─ team-workflow (Trellis) ──┴→ verifying-before-done
systematic-debugging ───────────────────────────────────────────→ verifying-before-done
open-items queue → triaging-open-items (user only)
draft migrations → releasing-migrations (user only), before tagging
```

| Skill | Started by | Does |
|---|---|---|
| `/sdd:writing-spec` | user or agent | Batched question rounds with recommended answers; one spec per story |
| `/sdd:writing-cases` | handoff | Expected and unexpected cases per decision; the one approval, with the solo or team choice |
| `/sdd:solo-building-from-cases` | handoff | Test-first per case; approved tests stay fixed |
| `/sdd:team-workflow` | the user's choice | Cases as Trellis cards; workers run only their tests |
| `/sdd:systematic-debugging` | user or agent | A registered red loop first; three failed fixes stop |
| `/sdd:verifying-before-done` | handoff | One verify run per tree; blockers before done |
| `/sdd:triaging-open-items` | user only | Decide the waiting questions and findings |
| `/sdd:releasing-migrations` | user only | Merge draft migrations into numbered ones |

## Scripts

All in `scripts/`; every verdict comes from git, an exit code or a script,
never from a model.

| Script | Does |
|---|---|
| `store.py` | Says whether specs live in Trellis or `specs/` |
| `spec_check.py` | Cases without tests, tests naming unknown cases, colliding decisions |
| `verify.py` | Suite, spec, approved tests, debug logs, drafts; cached by tree hash |
| `open_items.py` | The queue: `list`, `resolve`, `compact` |
| `repro.py` | Debugging loops: `record`, `run`, `bisect` |
| `red.py` | Records a case's test failing before its code; verify requires one per approved case |
| `follow_through.py` | Placeholder patterns, asking-to-continue, and the work probes say is left |
| `release.py`, `release_check.py` | Merge drafts at release; fail while drafts remain |
| `shadow.py` | The observe-only model run; its lines reach only the user |

## Hooks

None runs the test suite.

| Event | Does |
|---|---|
| `SessionStart` | One line when open items wait; nothing otherwise |
| `PostToolUse` | After a write: migration facts, and placeholders where the work should be (`// ...`, `TODO: implement`, `panic("not implemented")`). After `AskUserQuestion`: queues unanswered questions |
| `Stop` | Queues the `Qn.` questions of the last reply and any declined `AskUserQuestion` in the transcript; sends back a reply that asks to continue while a blocker or an untested approved case is left; shows new shadow lines to the user |
| `SessionEnd` | The same transcript scan when a session ends with `/clear` or exit |

## The `.sdd/` folder

Everything the plugin keeps lives in `.sdd/` at the main worktree root, shared by
every worktree. `.sdd/.gitignore` keeps it all out of git except `config.json`:

```json
{
  "test": "go test ./...",
  "test_timeout": 600,
  "migrations": ["internal/store/migrations/*", "internal/store/migrate_[0-9][0-9][0-9][0-9].go"],
  "migration_checks": ["go test ./internal/store/..."]
}
```

- `test`: the suite verify runs. `test_timeout`: seconds, default 600.
- `red_since`: a commit. Cases whose test already existed there need no red
  record, for a repository that adopts sdd with tests already written.
- `migrations`: globs that share one version sequence. Without it, any
  `migrations/` or `migrate/` directory is its own sequence, and only an entry
  whose name starts with a standalone number (`0013_x.sql`, not Alembic's
  `27c6a30d7c24_x.py`) takes a version. Drafts go in `draft/` inside it.
- `migration_checks`: commands the release runs after writing numbered files;
  `migration_check_timeout` is seconds per command, default 1800. A failure or
  a timeout rolls the release back.

State files: `queue.jsonl` (events, append-only), `archive.jsonl` (the newest
200 resolved items plus every dismissed or accepted one), `verify/<tree>.json`,
`repro/<id>.json`, `red/<case>.json`, `shadow.jsonl`, and `release.json` while a release runs.

## Tests

`python3 -B -m unittest discover -s scripts/tests` from the repository root.
The Haiku evals (SDD-C9 to SDD-C11) cost money and run only with `SDD_EVAL=1`.
The spec is `specs/sdd/` in the TRELLIS vault.
