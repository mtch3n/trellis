# Knowledge paths

Date: 2026-09-16
Status: design, not yet planned
Ships after: `2026-09-16-knowledge-disclosure-policy-design.md`

## Problem

The knowledge vault is flat. Every document lives directly in
`<root>/projects/<KEY>/knowledge/` or the reserved `<root>/global/knowledge/`.
`Slugify` collapses every `[^a-z0-9]+` run to `-` (`internal/core/markdown.go:76`),
so `/` cannot survive into a slug; `path` is computed at create as
`filepath.Join(dir, slug+".md")` (`internal/core/knowledge.go:190`); the schema
has `UNIQUE (project_id, slug)` and no parent column.

That was fine when knowledge meant a handful of distilled findings. It does not
hold now that knowledge means every markdown document a project keeps.

## Why directories rather than more facets

The honest version of the argument, after two independent reviews.

Agents reach for paths without being told. A `--tag` filter has a discovery
problem: you cannot filter by a tag whose existence you do not know. A directory
listing is its own vocabulary. This project's own measurement is that hooks fire
in 100% of sessions and skills fire in at most 15% — agents do not reliably use
affordances they are merely told about, and they do reliably use paths.

Two corrections to that argument, both of which survived into this design:

- **Part of the tag discovery problem is self-inflicted.** `KnowledgeFilter`
  (`internal/core/knowledge.go:403`) has `BoardID`, `DocTypes` and `Provenances`
  but no tags, so tags are effectively write-only. The comparison is unfair to an
  unfinished alternative. This design fixes that in the same pass, independent of
  whether paths were a good idea.
- **A directory is a single hierarchy and knowledge is multi-axis.** The agent
  that creates a document picks the axis; every later agent has to guess it. A
  wrong tag costs one hit; a wrong directory is a miss. Two mitigations below —
  retrieval stays flat, and bare-leaf resolution — exist precisely for this, and
  without them paths are a net loss.

One claim from review is rejected: that tags are a derived-only structure living
in a junction table. `Frontmatter.Tags` is `yaml:"tags,omitempty"`
(`internal/core/markdown.go:24`) and round-trips through the file. Tags survive
`rm trellis.db` exactly as directories do. Paths win on being visible without
parsing anything, which is a real but smaller advantage.

## Path is optional

```
knowledge new --title "Rollback runbook" --in deployment
  → deployment/rollback-runbook
knowledge new --title "Recall ranking"
  → recall-ranking
```

Root-level documents stay legal. Nothing forces a document to answer "where does
this belong" before it can be written. This is the one piece of OKF philosophy
that applies directly: minimally opinionated, tolerate incompleteness.

`uniqueSlug` operates on the full path, so `deployment/rollback` and
`docs/rollback` coexist without suffixes. Today those would become `rollback` and
`rollback-2`.

## Reference syntax: `/` is path, `:` is project

```
[[deployment/rollback]]          path in the current project
[[GLOBAL:integrations-layout]]   the global vault
```

`ParseWikilinks` currently reads the first `/` segment as a project key
(`internal/core/markdown.go:101`), so `[[deployment/rollback]]` would parse as
project `DEPLOYMENT`. Path separators and cross-project references collide
head-on, and the separator wins because it is the common case.

Six sites construct or split a reference on `/`:
`markdown.go:101`, `markdown.go:165`, `doc_relations.go:142`, `lint.go:61`,
`knowledge.go:379` (`key + "/" + doc.Slug`), and the `p.key || '/' || k.slug`
expression in the recall query.

The qualifier is barely load-bearing. `resolveDocRef`
(`internal/core/doc_relations.go:84`) returns nil for a qualified reference to
any project other than `GLOBAL` — "another project: a stub, deliberately
unresolvable". In practice the prefix only ever means `GLOBAL`. Eight wikilinks
exist in the entire vault and none uses the qualified form, so the old syntax is
removed outright rather than supported alongside the new one.

## Validation

Slug becomes path-shaped, and it is agent-supplied input feeding `writeAtomic`.

- Split on `/`, `Slugify` each segment, rejoin. `Slugify` already lowercases, so
  the case-insensitive collisions that would appear on macOS and Windows are
  handled.
- Reject empty segments, `.` and `..`.
- `filepath.Clean`, then confirm the resolved absolute path still has the
  project's `kbDir` as a prefix. Reject absolute inputs.
- **Reject Windows reserved device names** per segment: `con`, `prn`, `aux`,
  `nul`, `com1`–`com9`, `lpt1`–`lpt9`. `Slugify` produces these happily, and a
  reserved name stays reserved with an extension — `con.md` cannot be opened on
  Windows. This bug exists today in the flat vault, where a document titled "CON"
  already breaks the Windows CI job; directories multiply the surface. Fixed here
  because this is the pass that touches slug construction.
- **Length ceilings**, enforced at write rather than linted:

  | Rule | Value |
  |---|---|
  | One segment | ≤ 96 characters |
  | Full relative path | ≤ 180 characters |

  `NAME_MAX` is 255 on all three platforms and is not the binding constraint;
  Windows `MAX_PATH` = 260 is. The base
  `C:\Users\<name>\.trellis\projects\<KEY>\knowledge\` measures 52 characters for
  this machine, but the username and project key are variable, so 80 is reserved
  conservatively, leaving 180.

  96 per segment is chosen against real data: the longest slug in the live vault
  is 65 characters. A 64-character ceiling would reject a document that already
  exists.

  These two are enforced rather than reported because portability is already a
  hard commitment — `CLAUDE.md` requires Linux, macOS and Windows CI to pass — and
  because the failure surfaces on a different machine from the one that caused
  it. A 250-character path written on Linux succeeds there and breaks when the
  vault reaches Windows. Rejecting at write costs nothing to fix.

- **A generated slug is truncated; an explicit path is rejected.** A slug derived
  from a long title is truncated to the segment ceiling — the full title is
  preserved in frontmatter and the slug is only an address, so a long title must
  not be unwritable. A path supplied through `--in` is caller input and fails
  loudly instead. `Slugify` does not truncate today; nothing in the live vault
  reaches the ceiling.

Two call sites flatten a path by applying `Slugify` to a whole reference and
must use the per-segment form instead: `loadDoc` on lookup (`knowledge.go:303`)
and `ParseReference` (`markdown.go:168`), which is the inverse of
`ParseWikilinks` and reads a stored reference back out of the database.

## Depth

Depth itself has no enforced limit. The length ceilings above bound it
implicitly, and they bound it in the right place: with short directory names the
180-character budget allows roughly eleven levels, while directory names
following this vault's sentence-derived slug convention (measured leaf slugs run
32–65 characters) exhaust it at depth 3.

Go's `os` package applies the `\\?\` prefix to long absolute paths and probably
survives past 260 on its own, but git on Windows has `core.longpaths` off by
default, and markdown files are the source of truth — humans and editors touch
them directly. Go surviving alone is not enough, which is why the ceiling is
enforced rather than left to the runtime.

Three lint findings, all reported and never blocking, matching how the project
handles dangling links:

- depth ≥ 3
- a **directory** segment longer than 30 characters
- near-duplicate directory names, such as `deploy/` beside `deployment/`

The second is a budget warning well before the hard 96-character ceiling:
directories should be short so leaf slugs can be descriptive.

The first is a design smell. `deployment/aws/runbooks/rollback` encodes
`{domain: deployment, platform: aws, type: runbook}`, and `type` is already a
field while `aws` should be a tag. Depth is where using directories to
re-implement existing facets becomes visible. Two levels is the recommended
practice.

## Bare-leaf resolution

The other half of the directory decision. Without it, paths are pure cost: the
creating agent picks a directory and every later agent must guess it.

```
knowledge show deployment/rollback
knowledge show rollback
```

Resolution order:

1. Exact match on the full slug.
2. No exact match and the input contains no `/` — match the leaf:
   `WHERE slug = ? OR slug LIKE '%/' || ?`.
3. One result: open it.
4. More than one: list the candidates with full paths and stop. Never guess —
   silently opening one of two documents named `rollback` is worse than failing,
   because the caller cannot tell it got the wrong one. The candidate list also
   teaches the directory structure.
5. None: the existing not-found error, which already suggests `knowledge ls`.

This is not a compatibility shim. Bare leaf is a permanent first-class
addressing mode, the way `git checkout main` does not require `refs/heads/`.

Ambiguity is created by this design — `deployment/rollback` and `docs/rollback`
are both legal where the flat vault would have suffixed one — so the two belong
to the same decision.

| Command | Bare leaf | On ambiguity |
|---|---|---|
| `show` | yes | list candidates, stop |
| `mv`, `rm` | yes | list candidates, stop |
| `[[wikilink]]` | yes | stays unresolved; `lint` reports `ambiguous_link` |

Links never error, so the wikilink case degrades to a stub and lint names it.
Consistent with dangling links already being stubs rather than errors.

Known cost: a leading-wildcard `LIKE` cannot use the slug index. Irrelevant at
current vault sizes. If it ever matters, the fix is a generated `leaf` column
with its own index — not done now.

## Moving

`knowledge mv <ref> <new-path>` moves the file, rewrites the slug, updates the
row, and is the honest admission that this design makes a document's address
mutable.

Inbound wikilinks are rewritten in the same transaction. The link graph already
knows who points at the document; leaving them dangling would mean every move
silently erodes the graph, and lint would only report the damage afterwards.
Lint still catches whatever rewriting misses.

`promote` and `demote` move a file between the project vault and the global
vault (`internal/core/pin.go:212`). Both preserve the subpath.

## Retrieval stays flat

No `--path` filter on `search` or `recall`, and no glob syntax.

Search exists to find things when the caller does not know where they are. A
path filter requires knowing the prefix, which is the thing search is supposed to
supply, and it turns a wrong guess into a silent miss rather than a lower-ranked
hit.

**Path is an output of search, not an input to it.** Every hit's ref carries the
full path, so searching teaches the directory structure instead of requiring it
up front. That inverts the discovery problem rather than relocating it.

`--label` already exists on `search` and is the right shape by contrast: a label
is a property of the document, not its location. Properties filter; locations do
not.

A glob is the idiom for when the filesystem is the only index. FTS5, the vector
index and the link graph are already better at that job, and a second query
language would have to be maintained alongside them.

If path scoping ever proves necessary, the correct shape is a ranking boost
("working in deployment, prefer those"), never a filter — a boost cannot hide a
relevant document. Not in this design.

## Carried in the same pass

- `Tags []string` on `KnowledgeFilter`, and `--tag` on `knowledge ls`. Roughly
  fifteen lines, and it removes the unfair comparison described above.
- `knowledge ls` renders a tree; `knowledge ls <dir>` scopes to a subtree.
- The session-start hook's command list (`plugin/hooks/trellis_hook.py:95`)
  currently advertises `search <text>` and `knowledge show <slug>` but not
  `recall`. Recall fuses BM25, vector and link-graph rankings and is the better
  retrieval, and the hook is the only channel that reaches every session. One
  string.

## Non-goals

- No reserved `index.md`. OKF reserves it because its consumers have no query
  engine; Trellis has SQLite, FTS5, a vector index and a link graph. A stored
  listing would be a second source of truth that drifts.
- No fixed top-level vocabulary. Directories are free-form; lint reports
  near-duplicate directory names (`deploy/` beside `deployment/`) without
  blocking.
- No cap on file or directory counts. The scarce resource is context, not disk,
  and it is already bounded where it is spent: `MaxInjectedPins = 5` and the
  recall limit. A vault quota's failure mode is an agent that cannot write down
  what it just learned.
- No directory-inherited disclosure defaults. See the non-goals in the
  disclosure spec.

## Testing

- Path validation: traversal attempts, absolute inputs, empty and `.`/`..`
  segments, and each Windows reserved device name, all rejected.
- Length: a 97-character `--in` segment and a 181-character explicit path are
  both rejected; a title long enough to generate an over-ceiling slug is
  truncated instead, with the untruncated title intact in frontmatter.
- The longest slug currently in the vault (65 characters) still round-trips, so
  the ceiling does not break existing documents.
- Round-trip: create at a path, drop the database, reindex from files, confirm
  the slug and path are unchanged.
- Bare leaf: unique leaf opens; two documents sharing a leaf list both and open
  neither; a wikilink to an ambiguous leaf stays a stub and reports
  `ambiguous_link`.
- `mv`: file moves, row updates, inbound wikilinks rewritten, and a link that
  could not be rewritten shows up in lint.
- `promote`/`demote`: subpath preserved in both directions.
- Reference syntax: `[[a/b]]` resolves as a path, `[[GLOBAL:x]]` resolves to the
  global vault, and the old `[[KEY/x]]` form no longer parses as a qualifier.
- Retrieval: a document under `deployment/` is returned by a search that names
  neither the directory nor any part of it.
- Cross-platform: the path suite runs on Windows CI, which is where the reserved
  names and length limits actually bite.
