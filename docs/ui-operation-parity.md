# Web UI operation parity

Date: 2026-09-16
Status: work list, not started. Nothing here is being implemented until the
user asks.
Source: session trellis-5b checked every item against the code, and 26 of the
claims were checked a second time independently. Session trellis-2f's web UI
work (uncommitted on `feat/memory-groundwork`) is cross-referenced in
[Overlap with work in progress](#overlap-with-work-in-progress).

## Goal

People use the web UI as their way into Trellis, so it has to expose every
operation Trellis supports, except the ones that are agent or process mechanics
(listed under [Out of scope](#out-of-scope)).

Many items below need a new or changed HTTP route, not only frontend work. The
**Backend** column says which.

- **Route:** needs a new or changed route in `internal/ui/server.go`.
- **Core:** needs new logic in `internal/core`, and usually the CLI as well.
- **FE:** frontend only; the route already exists.

Line numbers were accurate when the list was compiled. Uncommitted edits to
`server.go` have moved some of them since.

## D. Decisions: the question that started this

**Is there a "decisions" type?** Yes. `decision` is one of six knowledge types:
`note`, `decision`, `finding`, `research`, `runbook` and `reference`. They are
the templates in `internal/core/templates/`.

- The type is chosen at creation with `--template`.
- It is stored as the frontmatter `type` and as `knowledge.doc_type`, and the
  JSON returns it as `type` (`internal/core/knowledge.go:35`).
- The live TRELLIS vault has five entries: one decision and four findings.

**Is there a separate folder for past decisions?** No. Entries are flat files
at `<root>/projects/<KEY>/knowledge/<slug>.md` (kbDir, `knowledge.go:121-131`),
and `Slugify` never produces `/`.

The knowledge-paths spec (`docs/superpowers/specs/2026-09-16-knowledge-paths-design.md`)
is not yet planned. Around line 166 it says `type` is already a field and
folders must not encode it again. **Do not build a `decisions/` directory.**

Agents already filter by type with `knowledge ls --type decision` and
`recall --type decision`. The HTTP list handlers take no query parameters
(`server.go:765-806`), so the UI can filter on the client for now.

| ID | Item | Backend | Notes |
|---|---|---|---|
| D1 | A "Decisions" virtual folder: let the vault tree group by `type`. Offer a toggle between grouping by type and by folder, defaulting to type while the vault is flat | FE | The TUI already groups by type (`internal/cli/tui_workspace.go:281-291`). See [overlap](#overlap-with-work-in-progress): the tree now builds folders from scope and slug, and grouping by type slots in as a second grouping |
| D2 | Type filter chips, one per type | FE | Client-side over the loaded list |
| D3 | A "New entry" button with a type picker, so "New decision" starts from the decision template | Route | Depends on K1 |

## B. Bugs (verified)

| ID | Bug | Backend | Detail |
|---|---|---|---|
| B1 | Saving a vault entry silently drops the title and summary. **Fixed and merged (7b0dbc4).** PATCH now takes optional `title`, `summary` and `body` plus a required `version`, and calls `EditKnowledgeFields`. A missing version is 400, a stale one 409, an empty title 400 (`missing_title`). The web editor already sends all four fields and will not submit an empty title | Route | KnowledgePage sends `{title, summary, body, version}`, but `handleKnowledgeEdit` (`server.go:829-848`) calls `EditKnowledge` with the body only. Fix: call `core.EditKnowledgeFields` with Title, Summary and Body (`knowledge.go:474`). **This affects the in-place vault editor built this session: the title and summary fields look editable, but their changes are lost.** trellis-2f is also changing `EditKnowledgeFields` to require `if_version` and to return 409 `conflict`; the page already sends `version` and now shows a 409 as "Changed elsewhere" |
| B2 | No label or tag UI anywhere. **Fixed for the card, not the tile.** The card's facts column now has Labels and Tags: labels are picked from the project's list, tags are free words, both apply as they change, and a new card carries them into its creation. Board tiles still do not show them, because `handleBoardCards` would have to select them and three other worktrees hold that file; it stays under C2 | FE | Card create sends only title, body, priority and column (`BoardPage.tsx:246-252`). The API already accepts `labels` and `tags` on POST (`server.go:931-934`), and `add_labels`, `remove_labels`, `add_tags` and `remove_tags` on PATCH (`server.go:704-713`) |
| B3 | Board navigation always goes to `boards[0]`. **Fixed.** The shell picks the board in the URL, else the project's default, else the first, and a board switcher sits beside the project switcher whenever a project has more than one. `boardInfo` now carries `is_default` | FE | `AppShell.tsx:91` and `:139`. There is no board switcher, so other boards can be reached only by typing the URL |
| B4 | The Attachments section can never appear. **Fixed by trellis-2f and merged:** entries list their artifacts and `GET /api/p/{key}/artifacts/{name}` serves the bytes | Route, Core | KnowledgePage reads `entry.artifacts` (`:340`), but `core.Knowledge` has no artifacts field and no route serves artifact bytes. Depends on the knowledge-artifacts spec, which is approved for implementation and in progress in trellis-2f's worktree. The frontend side (`ArtifactList`, `ArtifactPreview`) is already built against that contract |
| B5 | Web writes are attributed to `daemon:<pid>` unless `TRELLIS_AGENT` is set. **Fixed.** The UI server holds a second Core that writes as `webActor()`: `TRELLIS_AGENT` if set, else `human:<os-username>`, else `human:web`. Every mutating handler uses it; reads and the daemon's own background work keep the daemon's identity. `Core.WithActor` copies rather than mutating, so concurrent requests cannot trample each other. `GET /api/me` says who that is, so the browser can tell a lease it holds ("Held by you", and the card stays editable) from one an agent holds. **This changes the actor recorded in the event log for web writes** | Core | `internal/cli/daemon.go:95-99`. So notes, claims and steals made in the UI carry an identity that changes on every restart. `ReleaseCard` requires owner == actor (`lease.go:221-235`), so a lease taken in the UI cannot be released after a restart. Recommend a stable human actor for web writes. This also affects this session's UI writes: moves, priority changes, card and project delete, and taking a lease |

## C. Cards: core support exists, but no HTTP route and no UI

| ID | Item | Backend | Detail |
|---|---|---|---|
| C1 | Add a note | Route | `CreateNote` (`note.go:24`) is always permitted, even on cards you do not own. The UI shows notes read-only today |
| C2 | Labels and tags: show them on tiles and in the card detail, and add or remove them while editing | Route | PATCH already supports changes. `handleBoardCards` (`server.go:557`) does not select labels or tags, so the list must include them |
| C3 | Archive, restore, and a "show archived" view | Route | `ArchiveCard` and `UnarchiveCard` (`archive.go`). The board query filters on `archived_at IS NULL` |
| C4 | Blocked-by: show blockers, and add or remove them | Route | `Blockers`, `BlockCard`, `UnblockCard` (`blocker.go`) |
| C5 | Claim a free card and release your own lease | Route | `ClaimCard(steal=false)` and `ReleaseCard`. Only `/steal` exists today, with a fixed 30-minute TTL (`server.go:735-763`). Expose the TTL. Depends on B5 for release to work across restarts |
| C6 | Link a card to an entry, and show the link on both sides | Route | `LinkCardToDoc` and `Backlinks` (`doc_relations.go`) |
| C7 | Board filters: label, priority, archived | FE, Route | Mirror the `card ls` flags. Archived depends on C3 |
| C8 | Artifacts on cards: list, add, link, remove | Route | `CreateArtifact`, `ListArtifacts`, `LinkArtifactToCard`, `DeleteArtifact` (`artifact.go`). An upload route needs an exception, because non-GET `/api` requests must be `application/json` (`security.go:59-64`). Do after the artifacts backend lands |
| C9 | (low) Bulk import, matching `card import` (a JSON array) | Route | |

## K. Knowledge

| ID | Item | Backend | Detail |
|---|---|---|---|
| K1 | Create an entry | Route | `POST .../b/{board}/knowledge` exists but is never called. Its request struct (`server.go:723-729`) lacks `private`, `tags`, `labels` and `provenance`; add them. Coordinate with trellis-2f: its templates plan (TRELLIS-35) will require `sources` on decision and finding entries, and the form must follow |
| K2 | Delete an entry | Route | `DeleteKnowledge` (`knowledge.go:592`). No route exists |
| K3 | Change type, private, tags, labels or board after creation | Core, Route | **No core support.** `KnowledgeEdit` has only Title, Summary, Body and IfVersion (`knowledge.go:75-80`); today the only way is to edit the frontmatter by hand. Needs core, CLI and API work. Without it, an entry saved as a note cannot be moved into Decisions |
| K4 | Filters: type and provenance, and cold entries | FE, Route | `KnowledgeFilter` already has DocTypes and Provenances (`knowledge.go:421`). Cold entries need `ColdKnowledge` (`health.go:83`) exposed |
| K5 | Pin and unpin, with recap and optional board scope; a pins list with a stale marker | Route | `pin.go` |
| K6 | Show backlinks from cards on the entry page | Route | `Backlinks` |
| K7 | A vault health view | Route | `Lint` (stubs, broken anchors, orphans), `Health` counts, `Dupes` clusters. The graph explorer already lists unlinked entries and stubs, computed on the client |
| K8 | Global vault lifecycle: the nominations queue, escalate, demote, verify | Route | `pin.go`. The CLI deliberately requires a human for these, and its error tells humans to escalate from the browser with `trellis ui` (`requireHuman`, `internal/cli/knowledge.go:507-530`), so the UI is the intended surface. Mirror the CLI: retype the slug to confirm, and require a reason. `nominate` is agent-only; leave it out |
| K9 | (low) Uptake analytics, read-only | Route | `RecallUptake` |
| K10 | Version history for entries and cards: the list of versions, and a diff between two | FE | **The backend is built on trellis-2f's branch, not yet merged.** `GET /api/p/{key}/knowledge/{slug}/history` and `.../diff?from=N&to=M`, and the same pair for cards, whose items also carry `actor`. Omitting both ends means previous against latest; a version no longer kept is 404. Entries keep up to 100 versions in a hidden `.<filename>/` directory beside the file, cards in a `card_revision` table, and `trellis config set history.keep <n>` changes the limit. The diff is plain text and must be rendered as text. Card version numbers have gaps, because a move also bumps the version. `HistoryList` and `DiffView` already exist and should show it |

## P. Boards, columns, labels

| ID | Item | Backend | Detail |
|---|---|---|---|
| P1 | Boards: switcher (B3), create, rename, delete (with a force option), set default | Route | Create already has a POST that is never called (`server.go:65`). The rest is in `board.go` |
| P2 | Columns: add (with after and done options), rename, reorder, delete (with move-cards-to) | Route | `column.go:108-189` |
| P3 | Labels: list, create (a description is required), delete, merge | Route | List already has a GET, and merge a POST, that are never called. The rest is in `label.go` |

## S. Search and graph

| ID | Item | Backend | Detail |
|---|---|---|---|
| S1 | Search options: method, project scope, label, limit | FE, Route | Method (fts, vector or hybrid) has no parameter in the handler. Project scope is hard-coded to `AllProjects=true` (`server.go:347`). Label is already supported by the handler, but SearchPage sends only `q`. Limit |
| S2 | Graph walk with depth, relation type and reverse, including cards and `blocked_by` | Route | `GET graph` exists but passes nil and false for relation type and reverse (`server.go:875`), and the UI never calls it. The vault graph today is built on the client from wikilinks |

## M. Admin: one System or Settings area, mostly read-only

| ID | Item | Backend | Detail |
|---|---|---|---|
| M1 | Agents, and what each one holds | Route | `ListAgents` |
| M2 | Config: list, get, set, unset | Route | |
| M3 | Doctor diagnostics, and the version with an "update available" notice | Route | Show the `trellis update` command. Do not replace the binary from the browser |
| M4 | Vector index: status, rebuild, prune, reindex, compact | Route | |
| M5 | Maintenance: prune and compact; backup, and backup prune | Route | |

## Out of scope

These are agent or process mechanics and stay out of the UI: `card next`,
`card renew`, `agent register`, `agent remind`, `recall` (including
`--record`), `knowledge nominate`, `daemon install`/`uninstall`/`start`/`stop`/`restart`,
`init`, `tui`, `ui`, and `update install`.

## Suggested order

1. **P0:** B1 (fixed, merged), B2 (fixed for the card; tiles remain under C2), B3 (fixed), B5 (fixed).
2. **P1:** D1-D3, C1-C6, K1-K3, K5, K8, P1-P3.
3. **P2:** K4, K6, K7, K10 (once its backend merges), S1, S2, C7, M1, and C8 once the artifacts backend lands.
4. **P3:** M2-M5, K9, C9.

## Overlap with work in progress

This session's web UI work is uncommitted on `feat/memory-groundwork`. None of
it conflicts with the list above, but several items build on it or change
meaning because of it.

**Already present**
- Delete a card: `DELETE /api/p/{key}/b/{board}/cards/{card}`, from the ⋯ menu
  on the card.
- Delete a project: `DELETE /api/p/{key}`, with the key retyped to confirm.
- Change a card's status and priority in place. Priority is a PATCH without
  `if_version`, since only title and body replacements require it.
- Card lists sorted by priority; card `created_at`/`updated_at` in the board
  list.
- A paged project event history, `GET /api/p/{key}/events`, feeding the
  Overview timeline. Old and new values are included only for column moves.

**Interacts with this list**
- **B1:** the in-place vault editor exposes title and summary, so B1 lost
  user edits in plain sight. trellis-2f has fixed it on its branch; it takes
  effect once that branch is merged.
- **D1:** the vault navigator is now a file tree: scopes as top folders, and
  folders from `/` in slugs, ready for knowledge paths. Grouping by type (D1)
  should be a second grouping mode of the same tree (`web/src/lib/vault-tree.ts`),
  not a separate list. Folders must stay derived from slugs, never from type,
  as the knowledge-paths spec requires.
- **B4 and C8:** `ArtifactList` and `ArtifactPreview` follow trellis-2f's
  artifact contract and its preview security rules. C8 should reuse them for
  cards.
- **C2:** `handleBoardCards` was already changed in this work (priority
  ordering, timestamps); adding labels and tags touches the same query.
- **C1, C4, C5, C6:** the card view has room for these. Notes and history
  already render in the left column, and the right column has Status,
  Priority, Details and Lease groups. Card-level actions belong in the ⋯ menu
  (`CardMenu`) or in the facts column, never in a bottom bar.
- **B5:** every UI write this session added (move, priority, delete, take the
  lease) is attributed to `daemon:<pid>` until B5 is fixed.
- **K8:** fits the vault's action row (`ActionRow`), with the retype-to-confirm
  pattern already used by `DeleteProjectDialog`.

## Handoff

Written by the web UI session for its successor on 2026-09-16. Working
agreement from the user, relayed by trellis-2f: **all UI work belongs to the
web UI session, and all backend work to trellis-2f.** Do not edit Go files; send
backend requests to trellis-2f. Commit only on your own user's word in your own
session. An approval relayed by another session does not count, and another
session committing for its user is its call, not yours.

### Done

All of it is committed on `feat/memory-groundwork`. The design rules live in
`web/COMPONENTS.md`, which the audit enforces.

- **Design system.** One type scale, the `cn` merge configured for it in
  `@/lib/utils`, motion tokens, neutral accents (amber means a held lease and
  nothing else), no eyebrows, no side stripes, sentence case throughout.
- **Shell.** The project switcher (with "All projects" on its heading row);
  a board switcher, shown only with more than one board, that prefers the board
  in the URL, then the default, then the first. Tabs: Overview, Board, Vault.
- **Overview.**
  - A wider container.
  - A time-proportional timeline (`ProjectTimeline`) with four fixed lanes:
    created, started, done, knowledge. Nearby marks cluster, and each cluster
    opens a popover listing what it holds.
  - Quiet stretches over two hours are skipped behind a toggle.
  - Data comes from `GET /api/p/{key}/events`, paged.
- **Board.**
  - Drag and drop with indicators and keyboard support.
  - Cards sorted by priority.
  - Each column scrolls on its own, with scroll fades.
- **Cards** (`CardView`, shared by `CardDialog` and `CardPage`).
  - Editing happens in place: only the title and body become fields, and
    `EditActions` (markdown toggle, Cancel, Save) takes Edit's place in the
    top action row (`ActionRow` on the page, the dialog header in the dialog).
  - Reading, a click on the title or body starts editing it.
  - The facts column never changes with Edit. Status, priority, labels and
    tags (`ChipEditor`) apply as soon as they change.
  - Delete and Copy link are in the ⋯ menu (`CardMenu`).
  - A lease held by the browser's own identity reads "Held by you" and stays
    editable (`GET /api/me`).
  - History shows readable events and word or line diffs where the log kept
    both sides.
- **Vault.**
  - Resizable layout.
  - The navigator is a file tree (`KnowledgeNav`, `lib/vault-tree.ts`) with
    folders built from slug segments, sorting, collapse all, arrow-key
    navigation, and remembered state.
  - The graph is docked and can expand to full screen.
  - Entries are edited in place, with a sticky action row.
  - Attachments show in the facts column with safe previews (`ArtifactList`,
    `ArtifactPreview`; verified in a browser).
  - A 409 on save shows "Changed elsewhere", reloads, and keeps the edit open.
- **Projects.** Delete, with the key retyped to confirm.

### Open, and the backend each item needs

Items marked **merged** have their backend on the branch now. Everything else
needs a request to trellis-2f first. The tables above hold the full list; these
come first.

- **K10 Revision history (merged).**
  - `GET /api/p/{key}/knowledge/{slug}/history` returns
    `[{version, timestamp}]`, newest first.
  - `GET .../knowledge/{slug}/diff?from=N&to=M` returns
    `{from, to, diff}`; the diff is unified text, so render it as text.
  - Omitting both ends compares previous with latest. A version no longer
    kept returns 404.
  - Cards have the same pair at `/api/p/{key}/cards/{card}/history|diff`;
    their history items also carry `actor`, and a card revision renders as
    `# <title>\n\n<body>`.
  - Card version numbers have gaps, because a move bumps the version too.
  - Build it into `HistoryList` and `DiffView` rather than a new view.
- **K1 and D3 Create an entry (merged).**
  - `POST /api/p/{key}/b/{board}/knowledge` takes `title`, `summary`, `body`,
    `template` (note, decision, finding, research, runbook, reference),
    `sources: string[]` and `set: {field: value}`.
  - `decision` and `finding` without sources return 400
    `template_violation`; show the message as it is.
  - Internal sources must resolve: `[[slug]]`, `/KEY/cards/KEY-12`,
    `/KEY/knowledge/slug`, `/GLOBAL/knowledge/slug`, `/KEY/artifacts/name`.
  - Entries now carry `sources`, so show them on the entry page too.
- **Timeline data (landing with trellis-2f's EventFeed change).** `/events`
  gains `title` and `type` and drops `read` events, so use `title` directly
  instead of looking titles up through the entry list.
- **Knowledge paths (merged in core).**
  - Slugs may contain `/`, and the tree already nests them.
  - **Unverified trap:** the routes are single-segment (`/knowledge/{slug}`
    in Go, and `:slug` in `App.tsx`), and the navigator links with
    `encodeURIComponent(slug)`.
  - Check that an entry with a `/` in its slug opens, saves, and shows
    history. If not, ask trellis-2f for `{slug...}` routes and switch the app
    route to a splat.
- **D1 and D2.** A type grouping mode for the vault tree, and type filter
  chips. Both are frontend only.
- **B2 remainder, labels on board tiles.** Needs `handleBoardCards` to select
  labels and tags (backend). The label picker is also empty until a project
  defines labels, which is P3 (label create, delete, merge; list and merge
  routes exist).
- **C1 to C9, K2 to K9, P1 to P3, S1, S2, M1 to M5.** See the tables. Most
  need routes.
  - The smallest wins are C1 (add a note), C5 (claim a free card and release
    your own lease), and K2 (delete an entry).
  - C5 matters most now that web writes have a stable identity: today the UI
    can take a lease but not release it.

### Build, test, verify

From `web/`:

```bash
pnpm exec tsc -p tsconfig.app.json --noEmit
node scripts/ui-audit.js
pnpm exec oxlint src                     # four page-load effects warn; they are known and left alone
pnpm run build                           # web/dist is embedded in the binary; rebuild on every frontend change
```

**Dev server against the running daemon.** Read only, unless you mean to
write to the user's data.

```bash
~/.local/bin/trellis daemon status       # prints the url with ?token=
TRELLIS_DAEMON=http://127.0.0.1:7788 TRELLIS_TOKEN=<token> pnpm exec vite --port 5199 --strictPort
# open http://localhost:5199, not 127.0.0.1: vite binds localhost only
```

**Anything that writes: use a scratch home, never a copy of the real one.**
TRELLIS-36 is still open: knowledge rows store absolute paths, so a copied
home writes the real vault.

```bash
mkdir -p /tmp/h /tmp/r && cd /tmp/r
TRELLIS_HOME=/tmp/h trellis init --key VERIFY
TRELLIS_HOME=/tmp/h trellis board new --name "second board"
TRELLIS_HOME=/tmp/h trellis label new ux --description "Look and feel"
env -u TRELLIS_AGENT TRELLIS_HOME=/tmp/h trellis ui --port 7799   # unset it, or web writes use the agent id
```

**Browser checks.** Use the `agent-browser` CLI (found under
`~/.cache/pnpm/dlx/*/pkg/node_modules/.bin/agent-browser`).

- Set the viewport, and `localStorage.theme = 'light'`.
- `network route '<glob>' --body '<json>'` mocks a response.
- Screenshot every state you change.

**For the user to see a change.** The token changes on every restart, so give
them the new URL.

```bash
go build -o ~/.local/bin/trellis ./cmd/trellis && systemctl --user restart trellis
```

### Traps

- **Shadcn installs.**
  - `pnpm dlx shadcn@latest add <x>` writes `import { cn } from "cn"`;
    change it to `@/lib/utils`.
  - Pipe `yes n |` into it so it does not overwrite existing components.
- **The UI audit.**
  - No arbitrary values except viewport units.
  - No `z-index` in pages: put it in a wrapper, as `ActionRow` does.
  - No raw `<button>` or `<label>`.
  - An icon inside `Button` needs `data-icon`.
  - Every component in a wrappers file, even an unexported one, needs a
    `COMPONENTS.md` row.
  - Imports from `@/lib` are allowed.
- **`edit-surface` and `edit-hint`** own their own margin and padding. Wrap
  them; never add spacing utilities to the same element.
- **Sticky elements.** Chrome sticks them inside the scroll container's
  padding. A scroll fade dims sticky edges: switch the start fade off with a
  class that sets `--scroll-fade-s-size: 0px`. The positioned ProseMirror
  editor paints over a sticky header that has no `z-index`.
- **Base UI dialog.** An `initialFocus` on content taller than the dialog
  scrolls it. Focus the scroller itself.
- **Card versions.** A move or a priority change bumps the version, so refresh
  the open card (`refreshDetail`) before a title or body save. A priority-only
  PATCH needs no `if_version`; a title or body replacement does.
- **Test shells.** `TRELLIS_AGENT` is set in agent shells; unset it when you
  test the human identity. `pkill -f` and `pgrep -f` patterns match your own
  shell (exit 144), so find processes by port with `ss -lptn 'sport = :5199'`.
- **agent-browser selectors.** `find role button --name X` matches partial
  names. `[aria-current=page]` also matches the shell's active tab; scope it,
  for example `nav[aria-label=Vault] …`.
- **Other sessions' commits.** They have committed the whole shared tree more
  than once. Stage by explicit path, and check `git log -- <file>` to see
  where your files landed.
