# Revision history for knowledge and cards

Date: 2026-09-16
Status: approved 2026-09-16. Versions enter history by copy, not by move (the
user asked for the better of the two; see below).

## Problem

Nothing in Trellis can answer "what changed?"

- A knowledge edit records the new body in `event.new_value` and nothing about
  the old one. For a private entry it records nothing at all.
- A card edit records `("body", "", "")` — the fact of an edit, no content
  (`internal/core/card.go`).
- The web UI wants a version diff, and has asked for a contract.

The user's direction: keep old versions and diff them, **without git**, for
knowledge and for cards; use a library for the diff; keep 100 revisions by
default behind an exposed setting; and extend housekeeping to prune old things.

## Knowledge: revisions are files next to the entry

```
knowledge/
  standup.md
  .standup.md/
    7.md
    8.md
    9.md
```

Each entry has a hidden directory named `.<filename>` beside it, holding one
file per retained version, named by the entry's version number. A revision file
is the entry's complete file at that version, frontmatter included — tags, the
private flag and the artifact list are part of what changed.

- **One hidden directory per entry, not one hidden file per version.** At the
  default of 100 revisions, flat dotfiles would put a hundred hidden files per
  entry into the vault directory; this puts one.
- **Hidden on Unix-like systems.** On Windows a leading dot does not hide
  anything, but nothing depends on it being hidden.
- **Never mistaken for an entry.** Trellis never discovers entries by scanning
  directories — every entry is a database row. `Slugify` turns every character
  outside `a–z0–9` into `-` and trims `-`, and the knowledge-paths design
  slugifies every directory segment, so no entry or directory name can start
  with a dot and collide with a revision directory. Anything that lists
  directories (the paths design's tree view, its resemblance check) skips
  dot-directories.
- **Revision files are written with `writeAtomic`**, like every other file.

Revisions are original data, not derived data. Like the entry file, they are
records, and nothing needs to rebuild them.

### How a version enters history: copy, not move

The user asked for the old version to be **moved** aside when a new one is
written. That works for writes that go through Trellis. It loses a version when
a file is edited directly: if an agent writes version 8 through Trellis, and a
person then saves version 9 in an editor, version 8 is overwritten before
Trellis sees anything, so there is nothing left to move. Version 8 is the one
most worth diffing against.

So this design **copies each version in when Trellis sees it**, rather than
moving the previous one out when it is replaced:

| Moment | What is copied into `.<filename>/` |
|---|---|
| `CreateKnowledge` | the new file, as version 1 |
| before any Trellis write replaces the file | the current file, if its version is not already there |
| after any Trellis write | the new file |
| `refreshFromFile` detects a change made outside Trellis | the new file |

The second row covers entries that existed before this feature: their first
Trellis write captures the version it is about to replace.

The latest version therefore exists twice: as the entry and as its newest
revision. That is the whole cost. The retention count is unchanged.

What is still lost: an entry edited directly **before** this feature
ships, and before Trellis writes it again. Its pre-edit version was never seen.
This is a one-time gap at upgrade.

Two rules keep history free of repeats:

- A revision whose version is already there is not written again, so repeated
  reads and retries write nothing.
- A revision whose content is byte-identical to the newest retained revision is
  not written. An entry's version normally moves only when its file changes,
  but `refreshFromFile` also bumps it when the `private` mirror has drifted
  from the file, and that bump changes no content.

A failure to write a revision fails the operation that caused it. For a write
path, that rolls the write back, as `EditKnowledgeFields` already does for
other failures. For the refresh path, the next read retries. A silently missing
revision would be a gap nobody notices.

### Concurrent writers

Two writers at the same moment must not both succeed on the same version: the
first to finish wins, and the other gets `conflict`. This design relies on the
rules the knowledge write path follows (fixed alongside the artifact work):

- Every Trellis write runs inside one `Core.Tx`, which holds SQLite's write lock
  from `BEGIN` across processes. Revision files are written inside that same
  transaction, so two writers cannot pick the same version number.
- `knowledge edit` requires `--if-version`, as `card edit` already does.
- The entry file is replaced only if it still holds the bytes the edit was
  based on. A direct edit that lands during a Trellis write turns the write
  into a `conflict`; it is never overwritten. The direct edit is then captured
  by the next read, like any other.
- Undoing a failed write happens before the lock is released.

### Moving and deleting entries

- **Deleting an entry** removes its revision directory, staged with the entry
  through `stageRemoval`, so a failed transaction restores both.
- **`escalate` and `demote`** (`EscalateKnowledge`, `DemoteKnowledge`) move the
  revision directory with the entry, and undo that move on failure as they undo
  the entry's own.
- **The knowledge-paths design's `knowledge mv`** moves it too.

## Cards: revisions are rows

Cards are stored in the database, not in files, so their revisions are too:

```sql
CREATE TABLE card_revision (
    card_id    TEXT    NOT NULL REFERENCES card(id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    body_md    TEXT    NOT NULL,
    actor      TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (card_id, version)
);
```

A revision holds the card's title and body — the text a diff is about. Column
moves, priority, labels and leases are field changes that the event log already
records, and they create no revision.

A card's `version` also goes up on moves, lease changes and archiving
(`card.go`, `lease.go`, `reorder.go`, `archive.go`), so revision numbers are the
card's version at the time and have gaps. Capture happens at these moments:

| Moment | Row written |
|---|---|
| `CreateCard` | the new card |
| `EditCard` changes the title or body, and the card has no revision yet | the card as it was, before the edit — this covers cards that predate the feature |
| `EditCard` changes the title or body | the card after the edit |

Every card write goes through Trellis, so the direct-edit gap that shapes the
knowledge design does not exist here. Each insert is `INSERT OR IGNORE`.

Deleting a card deletes its revisions through the foreign key.

## Retention

One setting, in the existing config file and managed through the existing
`trellis config get | set | unset`:

```yaml
history:
  keep: 100
```

- `keep` is the number of revisions retained **per entry and per card**.
  Default 100.
- After a revision is added, the oldest are removed until at most `keep`
  remain, ordered by version number. For an entry, "removed" means the files
  are deleted; for a card, the rows are.
- `keep: 0` turns capture off. Existing revisions stay until pruned.
- A negative value is a configuration error, not a synonym for unlimited.

Lowering `keep` does not delete anything by itself. The excess goes at the next
revision of each item, or at once with `maintenance prune --revisions`.

## Diffs

A diff compares two retained versions and returns a unified diff. It uses an
existing, maintained diff library, not a hand-written algorithm.
`github.com/pmezard/go-difflib` is already in the module graph as an indirect
dependency, but it is archived. The implementation plan picks a maintained
unified-diff library, checks its documentation, and adds it following the
repository's toolchain conventions.

- For an entry, the two revision files are compared whole.
- For a card, each revision is rendered as `# <title>\n\n<body>` and the two
  renderings are compared.

### CLI

```
trellis knowledge history <slug>
trellis knowledge diff <slug> [--from N] [--to M]
trellis card history <card>
trellis card diff <card> [--from N] [--to M]
```

- `history` lists the retained versions, newest first, with timestamps. It also
  lists actors for cards; knowledge revision files do not record one.
- `diff` defaults to the previous retained version against the latest. A
  version that is not retained is an error naming the retained range.
- JSON output carries the two version numbers and the diff text.

### Web API — the contract for the frontend

```
GET /api/p/{key}/knowledge/{slug}/history
GET /api/p/{key}/knowledge/{slug}/diff?from=N&to=M
GET /api/p/{key}/cards/{card}/history
GET /api/p/{key}/cards/{card}/diff?from=N&to=M
```

- `history` returns `[{"version": 9, "timestamp": 1789...}, ...]`, newest first.
  Card entries also carry `"actor"`.
- `diff` returns `{"from": 8, "to": 9, "diff": "--- ...\n+++ ...\n@@ ..."}`.
  Omitting `from` and `to` means previous against latest.
- Both sit under `/api/`, behind `protectedHandler`'s host, origin and token
  checks.
- The diff is plain text in JSON. The frontend renders it as text; it is never
  HTML.

This replaces the earlier proposal to store old and new values in
`event.old_value` and `event.new_value`. That would have put a full body copy
into an append-only table on every edit, with no retention. The event log keeps
recording the fact of an edit and nothing more.

## Housekeeping

`trellis maintenance prune` gains two selectors beside its existing `--events`
and `--invocations`:

| Flag | Removes |
|---|---|
| `--revisions` | every revision beyond `history.keep`, across all entries and cards — for use after lowering the limit |
| `--orphan-history` | revision directories whose entry file no longer exists, which is what deleting a file outside Trellis leaves behind |

`--before` stays required with `--events` and `--invocations`, which prune by
age. The two new selectors prune by count and by existence, and take no
`--before`.

`trellis knowledge health`, which only detects and never changes anything,
reports the revision count and any orphaned revision directories, each with the
`maintenance prune` flag that acts on it.

A further candidate the user may want is `--orphan-artifacts`: files in a
project's artifact directory that no artifact row names. It is not in this
design unless asked for.

## Disclosure

Revision files are local files with the same standing as the entry they belong
to. They are not embedded, indexed or injected, so the disclosure design's
purge on reclassification does not touch them. A private entry keeps its
history. The history and diff endpoints disclose on explicit request, which is
the same standing as `knowledge show`.

The event log's content rules are unchanged.

## Accepted limitations

- **A direct edit made before this feature ships loses the version before it**,
  if Trellis never wrote the entry after the feature shipped. See above.
- **Knowledge revisions do not record who made them.** The entry file has no
  actor field, and a direct edit has no Trellis actor at all.
- **Stored paths are absolute** (TRELLIS-36). Revision directories are found
  relative to the entry's stored path, so they inherit that issue until it is
  fixed.

## Non-goals

- Restoring a revision. For an entry, restoring means rewriting its whole file,
  frontmatter included. Restoring a pre-private version would clear the
  private flag and re-expose the body without anyone deciding to. It needs its
  own design.
- Revisions of artifact files.
- Age-based retention.
- Revisions of pins, recaps, links or any other database-only field. Only an
  entry's file and a card's title and body are revisioned.
- Git.

## Order

This touches `CreateKnowledge`, `EditKnowledgeFields`, `refreshFromFile`,
`DeleteKnowledge`, `promote`/`demote`, `CreateCard`, `EditCard`, the config
file and a migration. The artifact work now running changes
`EditKnowledgeFields`, and TRELLIS-35 changes `CreateKnowledge`. Proposed
order: artifacts, then this — the frontend is waiting on its contract — then
TRELLIS-35, then knowledge paths.

## Testing

- Create writes version 1 into `.<filename>/`.
- A Trellis edit leaves both the replaced and the new version in history.
- An entry that predates the feature gets its current version captured by its
  first Trellis write.
- **A direct edit after a Trellis write keeps the Trellis version.** Write
  version 8 through Trellis, overwrite the file directly, read. Both 8 and 9
  are retained. This is the case that motivates copying over moving.
- Repeated reads write nothing new.
- A version bump from private-mirror drift, with no file change, writes no
  revision.
- Moving a card writes no revision; its next body edit writes one, numbered
  with the card's version at that time.
- With `keep: 3`, a fourth revision removes the oldest, for an entry and for a
  card.
- `keep: 0` captures nothing; a negative value is rejected.
- Deleting an entry removes its revision directory; a failed delete restores
  both.
- `promote`, `demote` and `mv` move the revision directory.
- A card's create and edits produce rows; deleting the card removes them.
- A failure to write a revision fails the edit and leaves the entry file
  unchanged.
- `diff` defaults to previous against latest, errors on an unretained version,
  and renders a card as `# title` plus body.
- `maintenance prune --revisions` trims to `keep` everywhere;
  `--orphan-history` removes directories whose entry file is gone, and nothing
  else.
- `knowledge health` reports both counts.
- The history and diff endpoints return the documented shapes and require the
  session token.
- Windows: a leading-dot directory is created, written, moved and removed
  correctly.
