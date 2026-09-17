# Virtual path addressing

Date: 2026-09-16
Status: design, planned in `docs/superpowers/plans/2026-09-16-virtual-paths.md`
Layer 2 of 3. It ships after `2026-09-16-pin-only-projects-design.md` and before
`2026-09-16-project-merge-design.md`.
It supersedes the reference-syntax section of
`2026-09-16-knowledge-paths-design.md`; see "Relation to knowledge paths".

## Problem

Trellis names things four ways, and the names do not round-trip. Every item
below was verified against the code.

- **A search hit cannot be opened.** `search` and `recall` emit a document as
  `KEY/slug` (`internal/core/search.go:173`, `internal/core/recall.go:165`).
  `knowledge show` passes its argument through `Slugify`
  (`internal/core/knowledge.go:304`), which turns `KEY/slug` into `key-slug`,
  so the command reports `knowledge_not_found`. `graph`, `knowledge edit` and
  `knowledge pin` fail the same way.
- **A document ref can be read as a card.** `graph TRELLIS/pain-point-analysis-sept-2026`
  fails `loadDoc`. It then falls through to `ParseCardRef`, which splits at the
  last hyphen and reads the argument as card 2026 of the current project
  (`internal/core/ids.go:30`).
- **A qualified card ref is silently re-scoped.** `card show OTHER-12` run in
  project `KEY` shows `KEY-12`. `loadCard` matches on `seq` and the current
  project and ignores the key (`internal/core/card.go:109`).
- **Cross-project wikilinks can never resolve.** `resolveDocRef` returns a stub
  for any key but the current one and `GLOBAL`
  (`internal/core/doc_relations.go:90`).
- **`[[a/b]]` means "project A, slug b"** (`internal/core/markdown.go:122`).
  That collides with document paths as soon as those exist.
- **Artifacts can only be named by UUID.** `artifact rm` and `artifact link`
  take the id and nothing else.
- **The same document gets two refs.** Backlinks build `p.key || '/' || k.slug`
  even when the source is a global document
  (`internal/core/doc_relations.go:117`). Everything else calls that document
  `GLOBAL/slug`.

## Addresses

Every object has one canonical address:

```
/KEY                          a project
/KEY/boards/<slug>            a board, by slug
/KEY/cards/<REF>              a card, by its full ref: /TRELLIS/cards/TRELLIS-21
/KEY/knowledge/<slug>         a knowledge entry that KEY owns and has not escalated
/GLOBAL/knowledge/<slug>      a global vault entry
/KEY/artifacts/<name>         an artifact, by file name
```

A knowledge address may carry an anchor, `/KEY/knowledge/<slug>#<heading>`,
wherever an anchor is meaningful: in wikilinks and in `trellis link`.

The card segment is the full ref, not the bare number. After a merge, a project
holds cards whose refs have another prefix (see the merge spec), and the path
has to name them without renumbering.

### Grammar

- **Key:** as in the pin spec, `^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`, matched
  case-insensitively. `GLOBAL` is valid only with `knowledge`.
- **Board slug:** as in the pin spec.
- **Card ref:** `<prefix>-<digits>`, case-insensitive, stored upper-case. The
  prefix is looser than a project key, `^[^/#\s-][^/#\s]*-[0-9]+$` after
  upper-casing: it only has to be one segment with no `/`, `#` or whitespace.
  Two kinds of card carry a prefix that is not a valid key today. A card keeps
  its ref when its project is merged into another. And keys created before
  the key grammar existed, such as `MY_APP`, produce refs like `MY_APP-1`.
  After a merge, `/MONO/cards/MY_APP-1` must parse. The project key in the
  first segment of an address stays strict.
- **Knowledge slug:** one segment in the shape `Slugify` produces,
  `[a-z0-9]+(-[a-z0-9]+)*`. Knowledge paths later allow `/` inside this part;
  the parser treats everything after `/knowledge/` as the name, so that change
  only relaxes validation.
- **Artifact name:** one segment. It is non-empty, not `.` or `..`, and holds no
  path separator or NUL byte.

`internal/vpath` gains:

- `Parse(s string) (Path, error)`, for any absolute address;
- `SplitAnchor(s string) (target, anchor string)`;
- the constructors `KnowledgePath`, `GlobalKnowledgePath`, `CardPath` and
  `ArtifactPath`;
- the collection constants `CollectionCards`, `CollectionKnowledge` and
  `CollectionArtifacts`.

`ParsePin` is unchanged; a pin still holds only a project or a board.

## Arguments

The noun commands stay. A path names an object and a command names an
operation; claim, lease, note, block and `--if-version` have no file-verb
equivalent. There is no generic `ls`, `cat` or `mv`.

**Every argument or flag that takes a reference also takes an absolute
address.** A relative argument means what it means today, relative to that
command's collection in the current project. Rules per collection:

| Collection | Absolute | Relative |
|---|---|---|
| card | `/KEY/cards/KEY-N` | `KEY-N` names project KEY (see below); `N` means the current project's `N`; a UUID is looked up in the current project |
| knowledge | `/KEY/knowledge/s` names KEY's own, non-escalated entry; `/GLOBAL/knowledge/s` names the vault entry | `s` checks the current project first, then the vault. This is today's `loadDoc` order |
| board | `/KEY/boards/<slug>` | a board name, exactly as today |
| artifact | `/KEY/artifacts/<name>` | a name in the current project, or a UUID |
| project (`--project`, `TRELLIS_PROJECT`) | `/KEY` | `KEY` |

**`KEY-N` is a qualified card ref.** It names project KEY, exactly as
`/KEY/cards/KEY-N` does. The old behavior, which silently re-scoped it to the
current project, is removed.

**An absolute argument names its own project.** The command acts in that
project even when no pin applies, and `--project` becomes unnecessary. This
holds for flags that take a reference as much as for positional arguments:
`card block --by`, `knowledge new --board`, `knowledge pin --board`, and
`artifact add`, `link` and `ls --card`.

- An absolute argument beats the pin and `TRELLIS_PROJECT`. Both are ambient.
- If `--project` names a different project, the command fails with
  `project_conflict`. The caller stated two targets.
- If two references in one command name different projects, the command
  fails with `project_conflict`.
- A relative reference means the current project. When one resolves and
  another reference names a different project, the command fails with
  `project_conflict` rather than reading the relative one somewhere else:
  `card block 2 --by /OTHER/cards/OTHER-1` in a pinned directory must not
  quietly block OTHER-2. Where no current project resolves, the named project
  is the only candidate, and the relative reference is read there.
- A `/GLOBAL/knowledge/...` argument needs no project. `knowledge show`,
  `edit`, `demote`, `verify` and `graph` run without one, and consult nothing
  ambient: a malformed or stale pin, or a `TRELLIS_PROJECT` naming nothing,
  does not block them. `pin`, `nominate`, `escalate` and `rm` still resolve
  the current project, because they act on it.
- `init --board` is not a reference. It names a board of the project being
  pinned, by name.

**An absolute address in the wrong collection is a usage error**,
`wrong_collection`, and names the command that takes it. `card show
/KEY/knowledge/x` says to use `trellis knowledge show`.

**Core enforces the same boundary.** Every core function that takes a project
id and a reference refuses an absolute reference to a different project with
`wrong_project` (`/GLOBAL` is allowed where the operation allows it). This keeps
the project-scoped HTTP routes honest without a second rule set.

For cards, `CardRef` carries the project an address names apart from the ref's
prefix, so `/OTHER/cards/KEY-12` in project KEY is refused and never reduced
to `KEY-12`. Core refuses a card ref whose address project or prefix differs
from the project it acts in. That check lives in one place, which the merge
layer replaces once a prefix no longer has to match its project. Callers above
core, such as the terminal workspace, check only an address's project and
leave prefixes to core.

**Cross-project operations:**
- `trellis link <card> <doc>` may link a card to another project's document.
  Link rows carry no foreign key, and wikilinks now cross projects too.
- `card block` stays within one project. A cross-project blocker is refused
  with `cross_project_block`.

## Output

- A knowledge entry's `ref` becomes its canonical address everywhere. Card and
  document refs stay distinguishable at a glance.
  - Sites that change:
    - `docView`
    - the `CASE ... END || '/' || k.slug` expressions in
      `internal/core/search.go`, `internal/core/recall.go`,
      `internal/retrieval/service.go` and `internal/cli/search.go`
    - `Backlinks`, which also gets the missing `GLOBAL` case
    - `Traverse`
    - project events in `internal/ui/server.go`
  - One shared SQL fragment builds the address.
  - `internal/core/dupes.go:45` recovers a slug by cutting at the first `/`.
    It now uses `vpath.Parse`.
- **A card's `ref` stays `KEY-N`.** It is fully qualified, every card command
  accepts it, and it is what commit messages and notes already use. The brief
  stays as short as it is.
- **Artifacts gain a `ref`**, `/KEY/artifacts/<name>`. `artifact ls` prints it
  in place of the UUID, and graph nodes for artifacts use it.
- **Fix hints and teaching text follow:**
  - the hook's command list says `knowledge show <ref>`;
  - the recall integration says to open a hit with the ref it printed;
  - the core skill's examples use real refs.

## Wikilinks

```
[[slug]]                          relative: the current project, then the vault
[[slug#heading]]
[[/OTHER/knowledge/slug]]         another project's entry
[[/GLOBAL/knowledge/slug]]        the vault
```

**Parsing** (`ParseWikilinks`, `ParseReference`):
- A target starting with `/` goes through `vpath.Parse`.
- A relative target is slugified as a whole, so `[[a/b]]` becomes slug `a-b`
  until knowledge paths give it a directory.
- The `KEY/slug` and `GLOBAL/slug` forms are removed. Written today, they now
  parse as relative slugs and show up as stubs in `knowledge lint`.
- An absolute target in another collection (`[[/KEY/cards/KEY-1]]`) is kept
  as a stub. Lint reports it as `wrong_collection`.

**Resolution** (`resolveDocRef`):
- A relative target resolves in the current project, then in the vault. The
  vault fallback is new; the project's own entry still wins.
- `/KEY/knowledge/s` resolves in project KEY, **including when KEY is another
  project**.
- `/GLOBAL/knowledge/s` resolves in the vault.
- A target whose project or entry does not exist is a stub, as the
  dangling-link invariant requires. `resolveDocStubs` already re-resolves every
  stub, in every project, when an entry is created, so a stub is backfilled
  once its target appears. That holds even if the target's project is created
  later.
  Escalation and demotion run the same pass, because they change an entry's
  address.

**Disclosure.** Resolving a cross-project link discloses nothing new:
`--project OTHER` already reads that project.
- Recall stays scoped to the current project plus the vault. Its link boost
  reads only edges between candidates (`internal/core/recall.go:298`), so it
  never follows a link out of that scope.
- `graph` does follow links across projects. It is an explicit request for one
  entity's neighborhood, in the same class as reading the other project by
  name.

**Stored raw text.** There is no migration for it. `link.to_raw` holds what the
author wrote, and wikilink rows are rebuilt from the body on the next sync. The
author's vault has no qualified wikilinks today, and lint surfaces any written
later in the old form.

## Uniqueness the addresses need

An address must name exactly one object. Migration `0014` adds:

- `CREATE UNIQUE INDEX knowledge_global_slug ON knowledge(slug) WHERE global = 1`.
  - Two projects can escalate the same slug today, and the second escalation
    silently overwrites the first file in the vault directory
    (`internal/core/pin.go:260`, `moveFile` over an existing path).
  - `EscalateKnowledge` checks first and refuses with `global_slug_taken`.
- `CREATE UNIQUE INDEX artifact_name ON artifact(project_id, name)`.
  - `CreateArtifact` picks a free name by probing only the filesystem
    (`internal/core/artifact.go:88`). It checks the table as well.

**If existing rows violate either index, the migration fails** with SQLite's
unique-constraint error, and nothing changes.
There is nothing to guess: two vault entries with one slug already share a file
path, and the fix is a human decision.

## Web

`web/src` parses and builds document refs in four places:

- `lib/knowledge-graph.ts:66-72` and `:115`
- `pages/OverviewPage.tsx:102`
- `components/wrappers/ProjectTimeline.tsx:289`
- `lib/timeline-marks.ts:32`

All four move to the new form through one small parser in `web/src/lib`.
Routes do not change. `web/dist` is rebuilt, as CLAUDE.md requires.

The web tree has uncommitted work in progress. This layer's web changes are
made on top of it only after it is committed.

## Relation to knowledge paths

`2026-09-16-knowledge-paths-design.md` chose `[[GLOBAL:slug]]` for the vault
and `/` for directories. This spec replaces that choice: cross-project and
vault references are absolute paths, and `/` inside a relative reference is left
free for directories.

That spec's "Reference syntax" section is amended to point here. Its path
validation, `--in`, `mv` and tree listing are unaffected. When it ships, it
relaxes the knowledge-slug grammar above to allow directory segments.

## Non-goals

- Generic file verbs (`ls`, `cat`, `mv`, `rm`) over the tree.
- Changing web routes.
- A card path in the brief or in commit messages. `KEY-N` stays the form people
  and agents type.
- Letting project-level commands (knowledge, label, search) run without a
  resolvable board. Today they fail with `no_default_board` in a project that
  has several boards and no default; that is a separate change.
- Migrating raw link text.

## Testing

- **`vpath.Parse`:**
  - Every collection, in both cases, with anchors split off.
  - `GLOBAL` accepted only with `knowledge`.
  - Rejected: bad key, bad slug, bad card ref, an empty name, and `..`.
- **Arguments:**
  - An absolute card, document, board or artifact address works with no pin.
  - It beats the pin and `TRELLIS_PROJECT`.
  - It conflicts with a different `--project`.
  - An address in the wrong collection names the right command.
  - `OTHER-12` acts in OTHER, never in the current project.
  - `N` and a UUID stay local.
- **Output:**
  - `search`, `recall`, `knowledge show`, backlinks, `graph`, `lint` and
    project events all emit the canonical document address, including for a
    global document.
  - Each emitted ref opens with the matching `show` command. This is the
    round-trip the problem statement says is broken today.
- **Wikilinks:**
  - Relative and anchored links resolve as before, and a relative link falls
    back to the vault.
  - A missing heading is reported for a target in another project or the
    vault, not only for this project's entries.
  - `[[/OTHER/knowledge/x]]` resolves across projects.
  - A link to a missing project is a stub and is backfilled when that
    project's entry is created.
  - The removed `KEY/x` form becomes a relative stub, and lint reports it.
- **Core boundary:** an absolute ref naming another project returns
  `wrong_project` from core.
- **Migration `0014`:** clean data migrates. A seeded duplicate vault slug, or a
  duplicate artifact name, fails the migration with the offending rows named.
- **Escalation:** escalating a slug that the vault already holds is refused, and
  the existing file is untouched.
- **Artifacts:** `artifact rm` and `artifact link` accept a name and an address.
  A second artifact with the same file name gets a suffix even when the first
  file was removed from disk but its row remains.
- **Web:** the ref parser is tested with the same cases as `vpath.Parse`, using
  Node's built-in `node --test` (`web/scripts/vpath.test.mjs` imports the `.ts`
  module through Node's type stripping; no new dependency), and
  `pnpm run build` succeeds.
