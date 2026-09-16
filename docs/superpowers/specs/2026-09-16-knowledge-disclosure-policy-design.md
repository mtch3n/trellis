# Knowledge disclosure policy

Date: 2026-09-16
Status: design, not yet planned
Revised: 2026-09-16, twice.
- After review 1: two exposure routes added, pointer shape corrected, one
  unimplementable test removed.
- After review 2: one of those routes withdrawn — it was asserted from a grep
  hit without reading the query's `WHERE` clause and does not exist. The
  disclosure lag on the two surfaces that actually reach a model is added in its
  place, and the reindex mechanism is dropped as unnecessary.

## Problem

`knowledge` means every markdown document a project keeps: meeting notes,
conference logs, deployment logs, environment documentation, credentials. Any
content is allowed. Trellis does not inspect, classify or judge what a body
holds.

What Trellis does owe the author is control over **where a body goes**. Today it
offers none. Five routes carry a body somewhere the author did not choose, all
verified against the code:

| # | Route | Site | What leaves |
|---|---|---|---|
| 1 | Vector index, retrieval path | `internal/retrieval/service.go:170` | `Content: doc.BodyMD`, chunked, POSTed to the configured embedder |
| 2 | Vector index, CLI path | `internal/cli/vector.go:72` | same composition, second entry point |
| 3 | Pin recap | `internal/core/pin.go:41` | falls back to `FirstParagraph(doc.BodyMD)`; the recap is injected at session start |
| 4 | Recall recap | `internal/core/recall.go:169` | `COALESCE(NULLIF(k.recap, ''), k.summary)` — its own fallback, independent of pin |
| 5 | Event log | `internal/core/knowledge.go:547` | `EditKnowledgeFields` writes the **entire edited body** into `event.new_value`, on every edit, permanently |

Route 5 was missed in the first draft. It fires on every ordinary edit with no
pin involved: a document that was never pinned and never embedded still has its
full body in the event log.

A sixth route was claimed in the second draft — that `internal/ui/server.go:535`
serves `event.new_value` over HTTP — and **it does not exist**. That query is the
card-detail handler and is scoped `WHERE entity_type = 'card'`; the global
activity feed (`server.go:249-259`) selects `field` but neither `old_value` nor
`new_value`. No HTTP surface serves a knowledge event's value. Route 5 still
needs closing, because it is a real local copy the purge must clear, but it is
not published anywhere today.

The embedder may be remote. `VectorSearchConfig.Provider` is `command`, `local`
or `http` (`internal/config/config.go:88`), but `local` only defaults to
`127.0.0.1:11434` when `Endpoint` is empty (`internal/vector/index.go:215`) — a
configured endpoint can point anywhere.

Writing a document must not mean broadcasting it.

## Scope

The author declares; Trellis honours the declaration mechanically.

There is no content inspection, no credential scanning, no heuristic
classification, and no advice about what belongs in the vault. A body containing
production passwords is an ordinary document that happens to be marked private.
If content inspection is ever wanted it is a plugin concern, and no extension
point is built for it here.

One thing this design cannot do, stated plainly because the mechanism cannot be
made to do it: `knowledge show` discloses. For an AI caller, its output goes
into model context. Trellis has no signal separating an agent opening a document
on its own initiative from a human asking for it, so the flag shapes automatic
surfaces only. It is egress control, not access control.

## The flag

One frontmatter key, one mirrored column:

```yaml
private: true
```

Two levels, because the only mechanically meaningful line is whether a body
leaves its file. Graded schemes (`public`/`internal`/`confidential`/`secret`)
borrow a vocabulary nobody applies consistently, and none of the extra tiers
would change what the code does.

`private` rather than `restricted`: nothing about storage is restricted, and the
name should not suggest otherwise. It marks a body as not-for-automatic-
distribution.

`private` **must live in frontmatter**, not only in SQLite. The project
invariant is that anything derived is rebuildable from the files; if the flag
were a column alone, a rebuilt row would read the file back as an ordinary entry.

The column is a mirror, populated on read like every other knowledge column. An
absent key means `false`. An unrecognised value is a parse error rather than a
silent `false` — the one place this design refuses to tolerate incompleteness,
because the failure is unrecoverable. The field is typed `bool`, so
`SplitFrontmatter` already returns `bad_frontmatter` for a non-boolean.

There is no index on the column. Nothing may query it in SQL — see below — so an
index would never be used.

### The mirror is never trusted over the file

Any code selecting or disclosing on the strength of this flag must refresh from
the file first and decide on the refreshed value.

`ListSearchKnowledge` (`internal/core/search.go:233`) selects and *then* calls
`refreshFromFile` per row, so a `WHERE private = 0` clause there is wrong in both
directions: a document just marked private is still selected, and one just
un-marked can never be selected again.

The same trap catches the two surfaces that feed a model, and it catches them
where it matters most:

- `Recall` reaches FTS through `SyncKnowledgeSearch`, which refreshes the search
  index without updating knowledge rows, and then gates on `k.private` in SQL.
- `Pins` (`internal/core/pin.go:109`) is pure SQL over `k.recap`.

So after a hand-edit sets the flag, recall would still return the old summary and
the session brief would still inject the old recap, until some unrelated command
happened to refresh that row. Both surfaces are bounded — recall returns a
handful of hits, pins are curated and few — so both refresh the documents they
are about to disclose and decide afterwards. A lag here is not acceptable,
because these are precisely the paths that reach a model unasked.

## Semantics

| Surface | normal | private |
|---|---|---|
| Local FTS5 | indexed | indexed |
| `search` / `recall` hits | returned | returned |
| Vector index | body, chunked | **not indexed at all** |
| Recap | may fall back to summary, then first paragraph | **none; a supplied recap is discarded** |
| Session injection | recap | **pointer: ref + title** |
| Event log content | body and summary recorded in `new_value` | **field name only, `new_value` empty** |
| `knowledge show` | body | body |

Presence is not what is being protected; automatic transmission is. A private
document that never surfaces is one nobody can act on, and env and deployment
documents are exactly the ones an agent most needs to know exist.

The pointer is **ref + title**, not ref + title + path. An earlier draft said
path; paths do not exist until the knowledge-paths design ships, and the ref is
already the address. Once slugs become path-shaped the pointer carries the path
for free, with no change here.

A private document has **no recap**, written or derived. An earlier revision
allowed an explicit one, but recall and the pin list blank the recap of every
private hit, so a stored recap was never shown. It only sat in `knowledge.recap`
and the `pinned` event, ready to be injected the moment the flag was cleared.
Pinning a private document therefore succeeds with or without `--recap`: a
supplied recap is discarded, `recap` and `recap_hash` stay NULL, and the
`pinned` event records no value.

Local FTS5 keeps indexing private documents, and this is safe because
`rebuildKnowledgeFTS` (`internal/core/knowledge.go:632`) reads files directly
while `Search` returns neither snippet nor summary — a hit carries an identifier,
not content.

### Why the vector index is excluded outright

An earlier draft embedded `title + summary` instead of the body, and a second
option gated on whether the embedder is local. Both are rejected.

Deciding "is this endpoint really on this machine" is a judgement call, and
`provider: local` with a custom endpoint can be remote. That judgement does not
belong on an egress boundary. Excluding private documents from the vector index
removes the question. They keep FTS5, which is local.

Every call site that builds, counts or prunes a vector index must read the same
corpus. Six read it today and they disagree: `ListSearchKnowledge` covers
`project_id = ? OR global = 1` while `ListKnowledge(KnowledgeFilter{})` covers
`project_id = ?` alone, so the CLI rebuild and the daemon reconcile already
produce different indexes and `vector prune` already deletes vectors that
`Reconcile` just wrote. Closing one path while five others query the table
directly is not a boundary.

Unifying them also removes the need for any separate vector-eviction step.
`Reconcile` builds both its upsert set and its prune keep-list from
`ListSearchKnowledge` (`internal/retrieval/service.go:163-180`), so a document
dropped from the corpus is pruned by the same pass that would have re-embedded
it. Nothing has to go and delete it.

## Reclassification

Marking an existing document private must purge what already escaped. A
private document never acquires these copies (it has no recap, and its pins and
edits record no value), so each one was made while the document was ordinary.
Three local copies exist:

1. `knowledge.recap` — the column, plus `recap_hash`
2. `event.new_value` for `pinned` — `PinKnowledge` writes an ordinary
   document's recap text into the event log (`pin.go` → `event.go:11`)
3. `event.new_value` for `edited` — `EditKnowledgeFields` writes the full body
   there (`knowledge.go:547`). This is the largest copy and the one nobody looks
   for, because an audit log is not where you expect to find content.

A pin is not a copy. Its row is (id, knowledge_id, board_id, created_at) and
holds no text, so the purge leaves it alone. Once the recap is gone a surviving
pin injects the pointer, which keeps telling the agent the document exists.
Deleting it would also break an immediate unpin: the purge runs inside the
caller's transaction, the unpin would find no row, and its error would roll the
purge back.

The purge is self-healing. Any caller that fails after it rolls it back
together with the mirror update, so the next successful read sees the same
transition and purges again. Nothing is disclosed in between, because recall
and the pin list decide from the file.

The vector index is a fourth copy and needs no explicit step, for the reason
above: exclusion from the corpus is eviction. A second draft of this spec added a
mechanism to push a reindex after the transition, on the belief that
`refreshFromFile` never triggers one. That belief rested on a misread —
`knowledge.go:280` is `LoadKnowledge`, which already calls
`notifyKnowledgeChanged` unconditionally after every successful load, while
`EscalateKnowledge` does not notify at all. The mechanism is dropped.

What remains is a bounded lag, recorded below rather than engineered away.

Anything already sent to a remote embedder cannot be recalled. Reclassification
cleans up locally and makes no wider claim.

A mirror that disagrees with its file is treated as a transition, so restoring
an older database purges and records a `privatised` event for any document whose
file says private and whose restored row does not. That is the safe direction and
it is intended.

## Accepted limitations

Stated here so they are not discovered later as surprises.

- **`knowledge show` discloses.** See Scope.
- **Local FTS5 permits inference.** A caller can confirm content by matching
  queries against it. Accepted inside the trusted-local-caller boundary; it is
  not a defence against anything with shell access.
- **Already-embedded content is gone.** Reclassification purges local copies
  only.
- **Old vectors linger until the next reconcile.** A document reclassified by a
  hand-edit leaves the corpus immediately, so it is never re-embedded, but its
  existing chunks are deleted only when a reconcile next runs — which any
  subsequent knowledge load or edit in that project triggers. Local, bounded, and
  not worth a dedicated eviction path.
- **The flag is author-declared and therefore forgettable.** Nothing detects an
  unmarked document that should have been marked, by design.

## Non-goals

- Graded classification levels.
- Content inspection of any kind — credential scanning, entropy checks, pattern
  matching. Trellis does not judge what a body contains. A plugin may; core does
  not, and no seam is built for one until a plugin actually exists.
- Restrictions on what may be stored. Any markdown content is valid.
- Redaction of bodies at read time. A heuristic redactor invites reliance on a
  guarantee it cannot make. Pointer-shaped injection is deterministic; redaction
  is not.
- Changing what the event log records for **ordinary** documents. Storing whole
  bodies in an audit log is questionable generally, but narrowing it for every
  document is a separate decision about audit fidelity. This design changes it
  only where an author has asked.
- Directory-inherited defaults. A path prefix is a natural place to hang a
  default, but that is a third feature with its own storage question, and it
  fails closed only inside correctly classified directories — a misplaced file
  still fails open, and moving a file out of a private directory would silently
  downgrade it. Revisit once a forgotten flag is an observed problem, and only if
  a moved document never loses its flag implicitly.
- OKF export. The philosophy applies — one required field, tolerate
  incompleteness, format not platform — but the format is not a deliverable.

## Testing

- Each of the five exposure routes, asserted closed for a private document and
  open for a normal one.
- Frontmatter is authoritative: create a document, set `private: true` by editing
  the file directly, and confirm the next read reflects it and purges. Then clear
  the flag in the file and confirm the document returns to the vector corpus —
  this is the direction a SQL-side filter would break.
- Mirror drift: set the column to disagree with the file and confirm the file
  wins on the next read. This covers a database restored from an older backup.
- **Reclassification with no intervening read.** Edit the file to set the flag,
  then make `recall` the very first operation, and separately make `Pins` the
  very first operation. Both must reflect the new state. A test that calls
  `LoadKnowledge` first, or that creates the document already private, cannot see
  this failure and is not a test of it.
- Reclassification: pin a document **and** edit its body while it is ordinary,
  confirm the recap reaches `knowledge.recap` and the `pinned` event and the body
  reaches the `edited` event, each checked on its own, then mark it private and
  confirm all of them are purged.
- No recap: pinning a private document with no recap and with an explicit one
  both succeed, and neither stores anything in `knowledge.recap`, `recap_hash` or
  the `pinned` event. The document injects a pointer and never a summary or body
  excerpt, through both the pin path and the recall path.
- A purge rolled back by a failing caller discloses nothing, and the next
  successful read purges again.
- An unrecognised `private` value fails the parse rather than defaulting.

There is deliberately no "drop the database and rebuild from files" test. The
first draft claimed one. No such path exists — the only `INSERT` into `knowledge`
is in `CreateKnowledge`, and `RebuildKnowledgeSearch` rebuilds FTS from existing
rows rather than reconstructing rows from files. The invariant that files are
authoritative is enforced here by the refresh-on-read path, which is what the
mirror-drift test covers. Reconstructing a vault from files alone is a real gap,
and a separate one.
