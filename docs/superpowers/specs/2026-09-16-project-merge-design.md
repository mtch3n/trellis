# Project merge

Date: 2026-09-16
Status: design, planned in `docs/superpowers/plans/2026-09-16-project-merge.md`
Layer 3 of 3. It ships after `2026-09-16-pin-only-projects-design.md` and
`2026-09-16-virtual-paths-design.md`.

## Problem

With pins, several directories can name one project, but nothing combines two
projects that already exist. Three cases need it:

- A monorepo whose packages were pinned separately and now want one namespace.
- A duplicate created by the old git-based resolution, before pins.
- A project whose key fails the key grammar, which no pin can name.

```bash
trellis project merge API --into MONO            # print the plan; change nothing
trellis project merge API --into MONO --apply    # back up, then merge
```

After an applied merge, SRC (`API` here) no longer exists and DST (`MONO`)
holds everything SRC owned.

## What moves, and how

| Item | Rule |
|---|---|
| Boards | Move and stay separate boards. A slug collision gives the moved board the slug `<src-key-lower>-<slug>` (then `-2`, `-3`…). A name collision gives it the name `<name> (<SRC>)`. No moved board is DST's default. |
| Columns | Follow their board. |
| Cards | Move. **Each keeps its ref**: `API-12` stays `API-12`. `seq` is renumbered after DST's highest, because `seq` is only DST's allocator. |
| Card labels and tags | Map by name. A label or tag DST lacks moves over with its id intact. A name DST already has is folded into DST's row. |
| Knowledge | Moves when its slug is free in DST. Identical file bytes under the same slug collapse to DST's entry. Different content under the same slug is a conflict. |
| Knowledge labels and tags | Map by name, like card labels. A document's frontmatter names labels, so every label SRC uses must exist in DST afterwards, or the next refresh of that document fails with `label_not_found` (`internal/core/doc_relations.go:55`). |
| Vault entries SRC escalated | Stay in the vault, and their origin becomes DST. The address `/GLOBAL/knowledge/<slug>` does not change. |
| Artifacts | Move when the name is free. The same name with the same `content_hash` collapses into DST's artifact, with the links re-pointed. A different hash is a conflict. |
| Pins and nominations | Follow their document and board ids unchanged. |
| Project config | DST's overrides stay. SRC's overrides are dropped, and the plan lists every one that differs from or is missing in DST. |
| Events | Stay. They are keyed by entity id (`internal/core/event.go`), so they follow the moved rows. |
| Vectors | Derived. SRC's `vec_*` tables are dropped, its vector files go to the backup with its leftover directory, and DST reconciles after commit. |

### Conflicts

- **By default a conflict stops the merge.** Nothing changes, and the plan
  lists each conflict with its slug or name and both content hashes.
- `--rename-conflicts` renames SRC's side instead:
  - `<slug>-<src-key-lower>` for a document;
  - `<stem>-<src-key-lower><ext>` for an artifact;
  - then `-2`, `-3`… if needed.
- A renamed document is rewritten where it is referenced (see below).
- A conflict involving a **vault** entry is never renamed automatically, even
  with the flag. Renaming it would change a `/GLOBAL/...` address that every
  project may cite. The fix is a human decision: demote it, or edit one side.
- **File conditions are conflicts too, and the plan checks them.** A SRC file
  that is missing, or an untracked file already sitting at a destination, is
  listed with a `reason`. The plan must see what the apply would trip over.
- **Collapsing keeps nomination evidence.** Where one actor nominated both
  entries, DST's nomination keeps its row and gains SRC's reason.

### Refusals

A missing SRC or DST is an error, `project_not_found` (or `project_merged`
for a key an earlier merge retired).

The plan reports a refusal in `refused`, and `--apply` then fails with
`merge_refused` without changing anything, when:

- SRC equals DST;
- DST's key fails the key grammar. Merging into a key no pin can name defeats
  the purpose;
- SRC has a card an agent holds right now. This is the same check as
  `DeleteProject` (`internal/core/project.go:153`).

## Card refs survive

A card's ref becomes stored data instead of being computed from its project's
key.

Migration `0015` adds `card.ref`, backfills it from `project.key || '-' || seq`,
and adds `CREATE UNIQUE INDEX card_ref ON card(ref)`. It is an `ADD COLUMN` plus
an index, so no table rebuild is needed. The index is global: a ref's prefix is
a key, keys are unique, and a merged key stays reserved (below), so two cards
can never share a ref.

- **New cards:** `createCard` writes `ref` as the project's key plus the
  allocated `seq`.
- **Every place a ref is built from key and seq reads the column instead:**
  - `internal/core/card.go:83`
  - `internal/core/ids.go:49`, for messages only
  - `internal/core/graph.go:91`
  - `internal/cli/board_show.go:169` and `:194`
  - `internal/ui/server.go:311` and `:567`
  - the SQL in `internal/core/search.go:153`, `internal/core/recall.go:181`,
    `internal/core/doc_relations.go:123` and `internal/core/blocker.go:88`
- **Lookup:**
  - `KEY-N` means `ref = 'KEY-N'`.
  - A bare `N` means `ref = '<current key>-N'`.
  - A UUID means `id`.

  Layer 2's rule that `KEY-N` names project KEY becomes "`KEY-N` names the
  project that holds the card with that ref". For an unmerged project that is
  the same thing. For a merged one it is the project the card moved to.
- **Text already written stays valid:** commit messages, notes, `blocked_by`
  link text (`internal/core/blocker.go:45`) and event values all store refs.
  None needs rewriting.

## The merged key is reserved

Migration `0015` also adds:

```sql
CREATE TABLE merged_project (
    key       TEXT PRIMARY KEY,
    into_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    merged_at INTEGER NOT NULL
);
```

- **Recording a merge:** an applied merge inserts SRC's row and re-points every
  earlier row whose `into_id` was SRC. A chain of merges therefore always points
  at the survivor.
- **Where the reservation shows:**
  - `ProjectByKey(SRC)`, `--project SRC`, `TRELLIS_PROJECT=SRC`, a pin naming
    `/SRC`, and an address under `/SRC/` all fail with `project_merged`. The
    error names DST and gives the replacement address.
  - A card ref such as `SRC-12` resolves normally. It names a card, and the card
    still exists.
  - `project new SRC` and `init --key SRC` are refused.
- **If DST is later deleted,** the cascade frees the reservation. The refs that
  carried SRC's prefix were deleted with DST.

## References are rewritten once

Relative references need no change: a moved document keeps its slug, unless it
was renamed. Absolute references change:

- **Wikilinks** `[[/SRC/knowledge/x]]`, in any project's documents, become
  `[[/DST/knowledge/x]]`, or `[[/DST/knowledge/x-src]]` if x was renamed. The
  file is rewritten and the `link.to_raw` rows are updated.
- **In SRC's own documents,** a relative `[[x]]` that pointed at a renamed x
  becomes `[[x-src]]`, so it still names the same entry.
- **`trellis link` targets** (`rel = 'documents'`) holding `/SRC/knowledge/...`
  get their `to_raw` updated.
- **A moved SRC document's `artifacts:` list**, naming an artifact of its own
  that was renamed on conflict, is rewritten to the artifact's new name. Left
  alone, the old name would resolve, once DST exists, to whatever DST already
  has under it — a different file, never the one the document meant.
- **`sources:` frontmatter**, in any project's documents, has each item that
  is a plain absolute address under `/SRC/` rewritten the same way a wikilink
  is: `/SRC/knowledge/x` becomes `/DST/knowledge/x`, or DST's collapsed
  entry's address; `/SRC/artifacts/shot.png` becomes `/DST/artifacts/...`,
  renamed the same way a renamed artifact's name is; `/SRC/cards/API-12`
  becomes `/DST/cards/API-12` — a card's ref never changes. A URL, prose, a
  `path:lines` pointer, and a `[[wikilink]]`-shaped item are left alone: only
  a plain address is one of these three things.
- **Stubs:** after the moves, every stub whose source is now in DST, or whose
  target is an address under `/DST/`, is re-resolved. A DST document that cited
  `[[x]]` before x existed there now resolves.

**Documents to rewrite are found by reading the files**, not the link rows.
The rows lag behind an edit made outside Trellis until that document is next
read.

**One rewrite function serves both cases.** It walks the wikilinks the parser
finds and replaces only those, so code spans and fences stay untouched, exactly
as parsing skips them. It lives beside `ParseWikilinks`.

## Pins are rewritten

The merge searches for pins under a **scan root**: the nearest ancestor of the
working directory that contains `.git`, or the working directory itself.

- **What the scan skips:** `.git` directories, and any nested directory that
  holds its own `.git`, because a nested repository has its own pins and its own
  commits. It does not follow symlinks.
- **How a pin naming SRC is rewritten:**
  - `/SRC` becomes `/DST/boards/<new slug of SRC's default board>`. A session in
    that directory still opens the same board.
  - `/SRC/boards/x` becomes `/DST/boards/<x's new slug>`.
  - A pin naming a board SRC does not have is left alone and reported.
- **When:** pins are rewritten after the database commit, with the replacing
  form of `writeAtomic`. They live outside the storage root, where no
  transaction reaches. If a rewrite fails, the merge still stands, the failure
  is reported, and that pin then fails with `project_merged` and a hint.
- **What the output says:** it lists every rewritten pin and reminds the caller
  to commit them. Pins outside the scan root are not searched. The plan says so.

## Atomicity and backup

**The backup is written first**, under
`<root>/backups/merge-<SRC>-into-<DST>-<utc>/`:

- `trellis.db`, written with `VACUUM INTO` (`Core.Backup`). `VACUUM` cannot run
  inside a transaction.
- `files/`, a copy of every file the merge will move or rewrite, laid out as
  they sit under the storage root.

**Next, SRC's `vec_*` tables are dropped**, through a core hook
(`SetDropDerived`) that the retrieval service implements.
- These tables live in `trellis.db`, and their names are derived from the
  project's vector-file path (`internal/vector/index.go:151`). Nothing drops
  them today, `DeleteProject` included.
- Dropping first is safe. sqlite-vec's `Destroy` only closes a handle, and the
  vectors live in `vectors.db`, so a refused or failed merge loses nothing:
  `vector.New` recreates the tables on next use.
- `DeleteProject` drops them the same way.

**Then one transaction does everything in the database.** File operations are
staged inside it:

- **Moves** publish with a hard link, which fails if anything is at the
  destination, even a file that appeared after a check. Then they remove the
  source. `writeAtomic` publishes the same way. The undo is recorded as soon as
  the link exists.
- **Rewrites** use `writeAtomic` with replace. The old bytes are recorded for
  undo *before* the write, because `writeAtomic` can replace a file and then
  fail to sync its directory.

**Between the backup and the transaction, nothing is locked.**
- The backup records a hash for every file it copied.
- The apply refuses (`merge_changed`, exit 4, nothing changed) to move or
  rewrite a file that the backup does not hold, or holds with a different hash.
- The database copy is a snapshot taken just before the apply. A change
  another process commits in that window is merged, but is not in the copy.

If the transaction fails, every staged operation is undone, newest first.

**After the commit:**

- SRC's project directory is moved into the backup as `leftover/<SRC>`. By then
  it holds only collapsed files and derived vector files. Nothing is deleted.
- Pins are rewritten.
- DST's knowledge-changed hook runs, so its vectors reconcile.

These steps are best effort. Each failure is reported in `warnings` rather than
returned, because the merge has already committed.

## Plan output

Without `--apply`, the merge computes everything inside a transaction that is
rolled back, and reports:

```
{
  "src": "API", "dst": "MONO", "ready": true,
  "boards":    [{"name", "slug", "new_name", "new_slug"}],
  "cards":     {"moved": 30, "first_seq": 58},
  "knowledge": {"moved": 12, "collapsed": ["runbook"], "renamed": [], "conflicts": []},
  "artifacts": {"moved": 3, "collapsed": [], "renamed": [], "conflicts": []},
  "labels":    {"moved": ["infra"], "folded": ["bug"]},
  "config_dropped": [{"key", "src", "dst"}],
  "documents_rewritten": ["/OTHER/knowledge/overview"],
  "pins": {"scan_root": "...", "rewrite": [{"path", "from", "to"}], "left": []},
  "refused": ""
}
```

- `ready` is false when there are conflicts or a refusal. The plan itself exits
  0 either way.
- `--apply` exits 4 with `merge_conflicts`, changing nothing, when the plan
  would not be ready.
- An applied merge returns the same shape plus `backup` and `warnings`.

Computing the plan inside a rolled-back transaction means the plan and the
apply run one code path; the plan is never a separate estimate. File operations
are skipped in plan mode. The plan reports what they would be.

## Follow-ups this closes

- `trellis doctor`'s `project keys` check changes its fix to
  `trellis project merge <KEY> --into <VALID-KEY>`.
- `DeleteProject` drops its `vec_*` tables before it deletes.

## Non-goals

- An undo command. The backup is the undo.
- Merging into `GLOBAL`, or merging only part of a project.
- Searching for pins outside the scan root.
- Renumbering card refs.
- Keeping SRC's config overrides.

## Testing

- **Everything moves:**
  - A merge of two populated projects: every board, column, card, note, label,
    tag, knowledge row, pin, nomination and artifact is in DST.
  - Files sit under DST's directories, and `path` columns match them.
  - SRC's directory is gone.
- **Card refs:**
  - `SRC-12` still opens the same card and names DST as its project.
  - New DST cards continue DST's own numbering. `ref` is unique.
  - A bare `12` in DST means `DST-12`.
  - Migration `0015` backfills refs that are identical to the computed ones.
- **Collisions:**
  - A board slug collision and a board name collision each get the documented
    rename.
  - An identical document collapses, and links to it are re-pointed.
  - A differing document stops the merge, leaving the database and files
    unchanged.
  - With `--rename-conflicts`, the document is renamed, and every absolute and
    relative reference to it is rewritten.
  - A vault conflict stops the merge even with the flag.
- **References:**
  - `[[/SRC/knowledge/x]]` in a third project's document is rewritten, in the
    file and in `link.to_raw`.
  - A DST stub that SRC's entry satisfies is resolved.
  - A code span containing `[[/SRC/knowledge/x]]` is untouched.
- **Labels:**
  - A label only SRC has moves, and a shared name folds into DST's label.
  - Documents whose frontmatter names SRC labels refresh without
    `label_not_found`.
- **Reservation:**
  - `--project SRC`, a `/SRC` pin and `/SRC/...` addresses fail with
    `project_merged`, naming DST.
  - `project new SRC` is refused.
  - Merging DST into a third project re-points SRC's reservation.
  - Deleting DST frees it.
- **Pins:**
  - `/SRC` and `/SRC/boards/x` under the scan root are rewritten.
  - A nested repository's pins are skipped.
  - A pin naming a missing board is left and reported.
- **Atomicity:**
  - A failure injected after files were moved restores every file and every
    rewritten body, and leaves the database unchanged.
  - The backup holds the database and a copy of every touched file.
- **Plan mode:** the plan reports exactly what apply then does, and plan mode
  changes nothing, in the database or on disk.
- **Refusals:**
  - Refused: a held lease, SRC equal to DST, a missing project, and a DST that
    fails the key grammar.
  - A SRC that fails the key grammar merges.
- **Vectors:** SRC's `vec_*` tables are gone after a merge, and after a
  `DeleteProject`.
