# Web UI operation parity

Date: 2026-09-16. Rechecked against `wip/vocabulary` on 2026-09-17, after the
vocabulary rename.
Status: in progress, not a record and no longer "not started". Rows marked
**Done** or **Fixed** below have landed; the rest is still the work list. A
Settings area — General, Templates, Maintenance and Logs, with PATCH writing
`config.yaml` — is built on both sides and ships in this release; the
integrator relays that from trellis-8d's branch.

**Unverified here: trellis-8d's uncommitted work.** The recheck read the Go
server and the `web/` code committed on this branch, and nothing else.
trellis-8d holds uncommitted frontend work, so a row that looks undone here may
already be built. Ask trellis-8d before starting one, and do not mark a row
Done on the strength of the backend alone. Where a row says "unverified from
this branch", that is what it means.

Source: session trellis-5b checked every item against the code, and 26 of them
were checked a second time independently. Session trellis-2f's web UI
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

**Is there a "decisions" template?** Yes. `decision` is one of six shipped
templates: `decision`, `finding`, `glossary`, `reference`, `research` and
`runbook`, in `internal/core/templates/`. There is no `note` template; an entry
may have no template at all.

- The template is chosen at creation with `--template`.
- It is stored as the frontmatter `template` and as the column
  `entry.template`, and the JSON returns it as `template`
  (`internal/core/entry.go`).
- The live TRELLIS vault has five entries: one decision and four of the
  `finding` template.

**Is there a separate directory for past decisions?** Not one named after the
template. Entries live at `<root>/projects/<KEY>/vault/<slug>.md` (`vaultDir`,
`internal/core/entry.go`), and a slug may hold directories, so an entry can sit
in `ops/db/`.

The vault-paths design (`docs/superpowers/specs/2026-09-16-knowledge-paths-design.md`,
now merged) says the template is already a field and directories must not
encode it again. **Do not build a `decisions/` directory.**

Agents already filter by template with `vault ls --template decision` and
`recall --template decision`. The HTTP list handlers take no query parameters,
so the UI can filter on the client for now.

| ID | Item | Backend | Notes |
|---|---|---|---|
| D1 | A "Decisions" virtual directory: let the vault tree group by `template`. Offer a toggle between grouping by template and by directory | FE | The TUI already groups by template (`internal/cli/tui_workspace.go`). See [overlap](#overlap-with-work-in-progress): the tree now builds directories from scope and slug, and grouping by template slots in as a second grouping |
| D2 | Template filter chips, one per template | FE | Client-side over the loaded list |
| D3 | A "New entry" button with a template picker, so "New decision" starts from the decision template. **Done:** see [Creating entries and switching templates](#creating-entries-and-switching-templates-done-k1-d3) | Route | Depends on K1 |

## B. Bugs (verified)

| ID | Bug | Backend | Detail |
|---|---|---|---|
| B1 | Saving a vault entry silently drops the title and summary. **Fixed and merged (7b0dbc4).** PATCH now takes optional `title`, `summary` and `body` plus a required `version`, and calls `EditEntryFields`. A missing version is 400, a stale one 409, an empty title 400 (`missing_title`). The web editor already sends all four fields and will not submit an empty title | Route | KnowledgePage sends `{title, summary, body, version}`, but `handleEntryEdit` called `EditEntry` with the body only. Fix: call `core.EditEntryFields` with Title, Summary and Body (`internal/core/entry.go`). **This affects the in-place vault editor built this session: the title and summary fields look editable, but their changes are lost.** trellis-2f is also changing `EditEntryFields` to require `if_version` and to return 409 `conflict`; the page already sends `version` and now shows a 409 as "Changed elsewhere" |
| B2 | No label or tag UI anywhere. **Fixed for the card, not the tile.** The card's facts column now has Labels and Tags: labels are picked from the project's list, tags are free words, both apply as they change, and a new card carries them into its creation. Board tiles: `handleBoardCards` now returns them (C2); whether the tiles render them is unverified from this branch | FE | Card create sends only title, body, priority and column (`BoardPage.tsx:246-252`). The API already accepts `labels` and `tags` on POST (`server.go:931-934`), and `add_labels`, `remove_labels`, `add_tags` and `remove_tags` on PATCH (`server.go:704-713`) |
| B3 | Board navigation always goes to `boards[0]`. **Fixed.** The shell picks the board in the URL, else the project's default, else the first, and a board switcher sits beside the project switcher whenever a project has more than one. `boardInfo` now carries `is_default` | FE | `AppShell.tsx:91` and `:139`. There is no board switcher, so other boards can be reached only by typing the URL |
| B4 | The Artifacts section can never appear. **Fixed by trellis-2f and merged:** entries list their artifacts and `GET /api/p/{key}/artifacts/{name}` serves the bytes | Route, Core | KnowledgePage reads `entry.artifacts` (`:340`), but `core.Entry` has no artifacts field and no route serves artifact bytes. Depends on the artifacts spec, which is approved for implementation and in progress in trellis-2f's worktree. The frontend side (`ArtifactList`, `ArtifactPreview`) is already built against that contract |
| B5 | Web writes are attributed to `daemon:<pid>` unless `TRELLIS_AGENT` is set. **Fixed.** The UI server holds a second Core that writes as `webActor()`: `TRELLIS_AGENT` if set, else `human:<os-username>`, else `human:web`. Every mutating handler uses it; reads and the daemon's own background work keep the daemon's identity. `Core.WithActor` copies rather than mutating, so concurrent requests cannot trample each other. `GET /api/me` says who that is, so the browser can tell a claim of its own ("Claimed by you", and the card stays editable) from an agent's. **This changes the actor recorded in the event log for web writes** | Core | `internal/cli/daemon.go`. So comments, claims and steals made in the UI carry an identity that changes on every restart. `ReleaseCard` requires the claimant to be the actor (`internal/core/claim.go`), so a claim taken in the UI cannot be released after a restart. Recommend a stable human actor for web writes. This also affects this session's UI writes: moves, priority changes, card and project delete, and stealing a claim |

## C. Cards: core support exists, but no HTTP route and no UI

| ID | Item | Backend | Detail |
|---|---|---|---|
| C1 | Add a comment. **Done:** see [Card comments: done](#card-comments-done); verified on this branch (`POST …/cards/{card}/comments`, called from `BoardPage` and `CardPage`) | Route | `CreateComment` (`internal/core/comment.go`) is always permitted, even on a card another actor has claimed |
| C2 | Labels and tags: show them on tiles and in the card detail, and add or remove them while editing | FE | **Backend landed:** PATCH supports changes, and `handleBoardCards` now returns `labels` and `tags` on every tile (see [Contracts landed since](#contracts-landed-since-f36fa23-from-trellis-2f)). The card detail has them (B2). Tiles: unverified from this branch |
| C3 | Archive, restore, and a "show archived" view | Route | `ArchiveCard` and `RestoreCard` (`archive.go`). The board query filters on `archived_at IS NULL` |
| C4 | Blocked-by: show blockers, and add or remove them. **Done** as one kind of card relation: see [Card relations: done](#card-relations-done); verified on this branch (`POST` and `DELETE …/cards/{card}/relations`) | Route | `Blockers`, `BlockCard`, `UnblockCard` (`blocker.go`) |
| C5 | Claim a free card and release your own claim | FE | **Backend landed:** `POST …/cards/{card}/claim` (with `ttl_minutes`) and `POST …/cards/{card}/release` exist beside `/steal` (see [Contracts landed since](#contracts-landed-since-f36fa23-from-trellis-2f)). The committed UI on this branch calls only `/steal`; whether trellis-8d has wired the other two is unverified from this branch. B5 is fixed, so release works across restarts |
| C6 | Link a card to an entry, and show the link on both sides | Route | `LinkCardToEntry` and `Backlinks` (`entry_relations.go`) |
| C7 | Board filters: label, priority, archived | FE, Route | Mirror the `card ls` flags. Archived depends on C3 |
| C8 | Artifacts on cards: list, add, link, remove | Route | `CreateArtifact`, `ListArtifacts`, `LinkArtifactToCard`, `DeleteArtifact` (`artifact.go`). An upload route needs an exception, because non-GET `/api` requests must be `application/json` (`security.go:59-64`). Do after the artifacts backend lands |
| C9 | (low) Bulk import, matching `card import` (a JSON array) | Route | |

## K. Vault

| ID | Item | Backend | Detail |
|---|---|---|---|
| K1 | Create an entry. **Done**, with directory and template: see [Creating entries and switching templates](#creating-entries-and-switching-templates-done-k1-d3) | Route | `POST …/b/{board}/vault` is called by `NewEntryDialog`. Its request (`entryRequest`) now carries `template`, `private`, `sources`, `set` and `directory`, and still lacks `tags`, `labels` and `provenance` |
| K2 | Delete an entry | FE | **Backend landed:** `DELETE …/b/{board}/vault/{slug}` calls `DeleteEntry` (`internal/core/entry.go`) and returns 204. No committed UI on this branch calls it; unverified from this branch whether trellis-8d has |
| K3 | Change template, private, tags, labels, sources or board after creation | FE; Core for board only | **Core, the CLI and the route now have everything but board.** `EntryEdit` (`internal/core/entry.go`) carries Title, Summary, Body, Artifacts, Sources, Template, Private, Tags, Labels and Set; `vault edit` exposes `--template --private --tag --label --source --set`; and `handleEntryEdit` passes Template, Private, Tags, Labels, Sources and Set straight through PATCH. What is left is the UI for those, plus core work for **board alone** — `EntryEdit` has no Board field and `vault edit` no `--board`, so an entry's board association is still frontmatter-only. How much of the UI already exists is trellis-8d's to say; unverified from this branch |
| K4 | Filters: template and provenance, and cold entries | FE, Route | `EntryFilter` already has Templates and Provenances (`internal/core/entry.go`). Cold entries need `ColdEntries` (`health.go`) exposed |
| K5 | Pin and unpin, with recap and optional board scope; a pins list with a stale marker | Route | `pin.go` |
| K6 | Show backlinks from cards on the entry page | Route | `Backlinks` |
| K7 | A vault health view | Route | `Lint` diagnostics, `Health` counts, `Duplicates` clusters. The graph explorer already lists orphans and stubs, computed on the client |
| K8 | Global vault lifecycle: the nominations queue, promote, demote, verify | Route | `promote.go` and `nomination.go`. The CLI deliberately requires a human for these three, and refuses them whenever `TRELLIS_AGENT` is set, pointing a human at an interactive terminal or `trellis ui` (`requireHuman`, `internal/cli/vault.go`), so the UI is the intended surface. Mirror the CLI: retype the slug to confirm, and require a reason. `nominate` is the agent's argument; leave it out |
| K9 | (low) Uptake analytics, read-only | Route | `RecallUptake` |
| K10 | Version history for entries and cards: the list of versions, and a diff between two | FE | **Backend merged.** No committed UI on this branch calls these routes; unverified from this branch whether trellis-8d has. `GET /api/p/{key}/vault/{slug}/history` and `.../diff?from=N&to=M`, and the same pair for cards, whose items also carry `actor`. Omitting both ends means previous against latest; a version no longer kept is 404. Entries keep up to 100 versions in a hidden `.<filename>/` directory beside the file, cards in a `card_revision` table, and `trellis config set history.keep <n>` changes the limit. The diff is plain text and must be rendered as text. Card version numbers have gaps, because a move also bumps the version. `DiffView` already exists; the view is to be called History |

## P. Boards, columns, labels

| ID | Item | Backend | Detail |
|---|---|---|---|
| P1 | Boards: switcher (B3), create, rename, delete (with a force option), set default | Route | Create already has a POST that is never called (`server.go:65`). The rest is in `board.go` |
| P2 | Columns: add (with after and done options), rename, reorder, delete (with move-cards-to) | Route | `column.go:108-189` |
| P3 | Labels: list, create (a description is required), delete, merge | FE | **All four routes exist:** `GET`, `POST` and `POST …/labels/merge` on `/api/p/{key}/labels`, and `DELETE …/labels/{name}` (see [Contracts landed since](#contracts-landed-since-f36fa23-from-trellis-2f)). The committed UI on this branch lists labels for the card's picker and calls none of the other three; unverified from this branch whether trellis-8d has |

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

The Settings area trellis-8d reports built covers M2 (General) and M5
(Maintenance), and adds Templates and Logs, which this table never listed.
Unverified from this branch: treat the rows as open until trellis-8d confirms
which it closes.

## Out of scope

These are agent or process mechanics and stay out of the UI: `card next`,
`card renew`, `agent register`, `agent remind`, `recall` (including
`--record`), `vault nominate`, `daemon install`/`uninstall`/`start`/`stop`/`restart`,
`init`, `tui`, `ui`, and `update install`.

## Suggested order

1. **P0:** B1 (fixed, merged), B2 (fixed for the card; tiles remain under C2), B3 (fixed), B5 (fixed).
2. **P1:** D1-D3 (D3 done), C1-C6 (C1 and C4 done), K1-K3 (K1 done), K5, K8, P1-P3.
3. **P2:** K4, K6, K7, K10 (backend merged), S1, S2, C7, M1, and C8 (its core has landed; it still needs routes).
4. **P3:** M2-M5, K9, C9. The Settings area is reported to cover M2 and M5; unverified from this branch.

## Overlap with work in progress

This session's web UI work is uncommitted on `feat/memory-groundwork`. None of
it conflicts with the list above, but several items build on it or change
meaning because of it.

**Already present**
- Delete a card: `DELETE /api/p/{key}/b/{board}/cards/{card}`, from the ⋯ menu
  on the card.
- Delete a project: `DELETE /api/p/{key}`, with the key retyped to confirm.
- Change a card's column and priority in place. Priority is a PATCH without
  `if_version`, since only title and body replacements require it.
- Card lists sorted by priority; card `created_at`/`updated_at` in the board
  list.
- A paged project event history, `GET /api/p/{key}/events`, feeding the
  Overview timeline. Old and new values are included only for column moves.

**Interacts with this list**
- **B1:** the in-place vault editor exposes title and summary, so B1 lost
  user edits in plain sight. trellis-2f has fixed it on its branch; it takes
  effect once that branch is merged.
- **D1:** the vault navigator is now a file tree: scopes as top directories,
  and directories from `/` in slugs, ready for vault paths. Grouping by
  template (D1) should be a second grouping mode of the same tree
  (`web/src/lib/vault-tree.ts`), not a separate list. Directories must stay
  derived from slugs, never from the template, as the vault-paths design
  requires.
- **B4 and C8:** `ArtifactList` and `ArtifactPreview` follow trellis-2f's
  artifact contract and its preview security rules. C8 should reuse them for
  cards.
- **C2:** `handleBoardCards` was already changed in this work (priority
  ordering, timestamps); adding labels and tags touches the same query.
- **C1, C4, C5, C6:** the card view has room for these. The timeline already
  renders in the left column, and the right column has Column, Priority,
  Details and Claim groups. Card-level actions belong in the ⋯ menu
  (`CardMenu`) or in the facts column, never in a bottom bar.
- **B5:** every UI write this session added (move, priority, delete, steal the
  claim) is attributed to `daemon:<pid>` until B5 is fixed.
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
  `@/lib/utils`, motion tokens, neutral accents (amber means a live claim and
  nothing else), no eyebrows, no side stripes, sentence case throughout.
- **Shell.** The project switcher (with "All projects" on its heading row);
  a board switcher, shown only with more than one board, that prefers the board
  in the URL, then the default, then the first. Tabs: Overview, Board, Vault.
- **Overview.**
  - A wider container.
  - A time-proportional timeline (`ProjectTimeline`) with four fixed lanes:
    created, started, done, entries. Nearby marks cluster, and each cluster
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
  - The facts column never changes with Edit. Column, priority, labels and
    tags (`ChipEditor`) apply as soon as they change.
  - Delete and Copy link are in the ⋯ menu (`CardMenu`).
  - A claim the browser's own identity holds reads "Claimed by you" and stays
    editable (`GET /api/me`).
  - History shows readable events and word or line diffs where the log kept
    both sides.
- **Vault.**
  - Resizable layout.
  - The navigator is a file tree (`KnowledgeNav`, `lib/vault-tree.ts`) with
    directories built from slug segments, sorting, collapse all, arrow-key
    navigation, and remembered state.
  - The graph is docked and can expand to full screen.
  - Entries are edited in place, with a sticky action row.
  - Artifacts show in the facts column with safe previews (`ArtifactList`,
    `ArtifactPreview`; verified in a browser).
  - A 409 on save shows "Changed elsewhere", reloads, and keeps the edit open.
- **Projects.** Delete, with the key retyped to confirm.

### Open, and the backend each item needs

Items marked **merged** have their backend on the branch now. Everything else
needs a request to trellis-2f first. The tables above hold the full list; these
come first.

- **K10 Revision history (merged).**
  - `GET /api/p/{key}/vault/{slug}/history` returns
    `[{version, timestamp}]`, newest first.
  - `GET .../vault/{slug}/diff?from=N&to=M` returns
    `{from, to, diff}`; the diff is unified text, so render it as text.
  - Omitting both ends compares previous with latest. A version no longer
    kept returns 404.
  - Cards have the same pair at `/api/p/{key}/cards/{card}/history|diff`;
    their history items also carry `actor`, and a card revision renders as
    `# <title>\n\n<body>`.
  - Card version numbers have gaps, because a move bumps the version too.
  - Build it as its own History view, reusing `DiffView`. History means
    revisions; the card's merged comments-and-events list is its Timeline.
- **Timeline data (landing with trellis-2f's event log change).** `/events`
  gains `title` and `template` and drops `read` events, so use `title`
  directly instead of looking titles up through the entry list.
- **Vault paths (merged in core).**
  - Slugs may contain `/`, and the tree already nests them.
  - **Unverified trap:** the routes are single-segment (`/vault/{slug}`
    in Go, and `:slug` in `App.tsx`), and the navigator links with
    `encodeURIComponent(slug)`.
  - Verified on 7992228: an entry with a `/` in its slug opens, saves, and
    can be created in a directory. History is unchecked until K10 exists; if
    it fails, ask trellis-2f for `{slug...}` routes and switch the app route
    to a splat.
- **D1 and D2.** A template grouping mode for the vault tree, and template
  filter chips. Both are frontend only.
- **B2 remainder, labels on board tiles.** The backend has landed:
  `handleBoardCards` returns `labels` and `tags`. What is left is rendering
  them on the tiles, unverified from this branch. The label picker is also
  empty until a project defines labels, which is P3 (label create, delete,
  merge; all four routes exist).
- **C2, C3, C5 to C9, K2 to K9, P1 to P3, S1, S2, M1 to M5.** See the tables;
  C1 and C4 are done. C2, C5, K2 and K10 have their backend and need only the
  UI; most of the rest still need routes.
  - The smallest wins are C5 (claim a free card and release your own claim)
    and K2 (delete an entry): both routes exist.
  - C5 matters most now that web writes have a stable identity: on this
    branch the UI can steal a claim but not release it.

### Contracts landed since (f36fa23, from trellis-2f)

- **Board tiles:** `GET .../b/{board}/cards` carries `labels` and `tags`, as
  sorted arrays that are empty when there are none.
- **Labels:** `POST /api/p/{key}/labels` with `{name, description}` returns 201
  and the label. `DELETE /api/p/{key}/labels/{name}` returns 204.
- **Claims:**
  - `POST .../cards/{card}/claim` with `{ttl_minutes?}` returns 200 and the
    card, or 409 when another actor's claim is live.
  - `POST .../cards/{card}/release` returns 200.
- **Comments:** `POST .../cards/{card}/comments` with `{body}` returns 201 and
  the comment, or 400 when the body is empty.
- **Deleting an entry:** `DELETE /api/p/{key}/b/{board}/vault/{slug}`
  returns 204. Slugs containing `/` travel percent-encoded, and edit, history,
  diff and delete are tested that way.
- **Entry PATCH** also takes `private`, `tags` and `labels`; a field left
  out keeps its value.
- **Coming, do not build on the old shape:**
  - `type` becomes `template` everywhere: entry JSON (and optional), PATCH,
    and `/events`. Group and filter by `template`.
  - Entry lists will stop carrying `body`, and for private entries
    `summary` and `recap` as well. `GET /api/p/{key}/vault/{slug}` returns
    one entry with its body; fetch it when an entry is selected.

### TRELLIS-31 handled; the graph waits on a links route

- The vault page now fetches the open entry from
  `GET /api/p/{key}/vault/{slug}` when it is chosen, and again after a
  save or a 409.
- **Graph edges come from the server now.** `buildGraph`
  (`lib/knowledge-graph.ts`) takes `GET /api/p/{key}/links/vault`
  (f4e56a1) and matches entries to links by address (`entry.ref`, for
  example `/KEY/vault/<slug>`). Nothing parses bodies any more. A link with
  no target is a stub, labelled with its `raw` text minus the anchor.
- **Fixed with it:** the rich-text editor escaped `[[wikilinks]]` on save
  (`\[\[slug]]`), unmaking every link it touched. `restoreWikilinks` in
  `MarkdownEditorImpl` puts them back. No real data was affected.

### Card relations: done

- **Where it lives.** `RelationsEditor` sits in the card's facts column, fed by
  aa1a985. Relations come back beside `card` in the detail response, not
  inside it, and both pages merge them in with `withRelations`.
- **What it shows.** Rows are grouped by kind. Each shows the other card's
  ref, title and column; an unfinished blocker is red. The add form picks a
  kind, then a card by search, and Enter takes the first match. Every row has
  a remove button.
- **History.** `related` and `unrelated` events read "Resolved by REL-2" and
  "No longer blocked by REL-4".

### Card comments: done

- **Backend.** Built on 3c8ab29. The detail's `comments` array (oldest first,
  `{id, card_id, actor, body, created_at}`) sits beside `card`. New comments
  go to `POST .../b/{board}/cards/{card}/comments` with `{body}`.
- **One timeline.** The card's **Timeline** section (the user's word;
  "history" is reserved for revisions) is `CommentBox` on top, then
  `CardTimeline` (formerly `HistoryList`) merging comments and `activity` by
  time. A comment writes no card event, so nothing shows twice. Comments render
  as markdown on a card surface, and the separate Notes section is gone.

### Vocabulary (user decisions, relayed by trellis-2f)

- A card's merged comments and events are its **Timeline**
  (`CardTimeline`).
- The project-wide event stream is the **Event log** (`EventLogPage` at
  `/event-log`, and the Overview's Event log section).
- **History** means revisions only.
- "Activity" and "feed" are retired, in labels and in identifiers.
- **The wire names have since been renamed, with no aliases.** The project
  event stream is `GET /api/events`, a card's detail carries `events`, and an
  event row carries `entity`. Every vault route now says `vault`:
  `/api/global/vault`, `/api/p/{key}/vault…`, `/api/p/{key}/links/vault` and
  `/api/p/{key}/b/{board}/vault…`. An old path 404s, and an old request key is
  refused as unknown. The full old-to-new map is the integrator's wire list; it
  is not repeated here.
- The labels, components and files the web half still owes follow the same
  glossary. This file keeps their current spelling, because that is what is on
  disk until the web session renames them.

### Templates: done

- **The field.** Built on 7dcb63e. Entries carry `template` (`""` for none),
  and `type` is gone from the web.
- **Labels.** The facts column, the Overview and the graph read "Template" and
  "No template" through `templateLabel` in `lib/format.ts`. The vault filter
  matches template names.
- **Saving.** A save whose template only warns shows its `warnings` in a
  warning toast; a rejecting template's 400 shows its message.
- **Still open.** D1 grouping by template, and D2 template filter chips.

### Creating entries and switching templates: done (K1, D3)

Built on 5c8e6c0 (`GET /api/templates`) and 7992228 (PATCH `sources` and
`set`, POST `dir`, errors as `{error, code, problems}`).

- **New entry.** The vault toolbar has a New entry button, and each project
  folder's row has one that starts inside that folder. `NewEntryDialog` asks
  for the title, the folder (top level, an existing folder, or a new one
  typed in), the template, and only what the template needs:
  - sources when `required` has them;
  - a select per `choices` field;
  - a line per other required field, sent as `set`.
  The chosen template's rules are described under the picker. The body is
  left empty, so the server writes the template's skeleton, and the new
  entry opens for editing with the cursor in its body.
- **Template on an entry.** The facts column starts with a Template select,
  applied at once like a card's column. An advisory template, or a strict one
  the entry already meets, switches straight away; warnings go to a toast.
  A strict template the entry does not meet opens `TemplateSwitchDialog`:
  - it asks for the sources and fields the entry lacks;
  - it appends missing sections to the body, empty, in the same save;
  - it waits while the body is open in the editor, because adding sections
    would write under the edit.
- **Sources on an entry.** A Sources group in the facts column lists them.
  Cards and entries link, URLs open in a new tab, and prose shows as written.
  Adding and removing apply at once; a strict template can refuse either.
  A bare `KEY-12` of this project is stored as `/KEY/cards/KEY-12`, so it is
  checked.
- **Refusals.** `readRefusal` keeps `problems` apart. Dialogs list them in a
  `RefusalAlert`; toasts join them after the message.
- **Every entry PATCH needs `version`.** Fact changes send the version on
  screen. The reload after them advances it, so an open body edit saves over
  the new version without a false conflict.
- **Title heading.** Skeletons begin with `# {{title}}`, and trellis-2f keeps
  it, because editors such as Obsidian show it as the page title. The entry
  page hides a leading H1 that matches the title, in reading and editing
  alike, and a save writes it back under the title as saved
  (`lib/title-heading.ts`).
- **Template fields (12f5dd0).** Entry responses carry `fields`: the
  frontmatter keys `Frontmatter` does not name, as strings or string lists;
  lists send `{}` for private entries, so the page reads them from the full
  entry. The facts column has a Fields group (`FieldsEditor`):
  - every field the template names, set or not, then any other field;
  - a select for a field with choices, a line of text for the rest, saved on
    Enter or when left, through PATCH `set`;
  - a list is shown, not edited;
  - a field the template does not name can be removed (`set` to `""`).
  The switch dialog no longer asks for a field the entry already has, unless
  its value is outside the choices. Verified: a choice change, a text change,
  clearing a required field under a strict template (refused, value kept to
  fix), removing an extra field, and switching an entry that already had the
  template's fields (only the missing section was added).
- **Editor.** The rich editor keeps `-` bullets and `---` rules, instead of
  rewriting them as `*` on the first save. Template guidance comments
  (`<!-- ... -->`) are kept, shown muted in mono, and not editable in rich
  text; the source view edits them.
- **Verified** in the browser on a scratch home, against 7992228:
  - create with no template;
  - create with decision and a card source;
  - create with a user template that has a choice and a required field,
    inside `ops/db`;
  - switch to decision with an unresolved source (refused, listed), then with
    real ones (sections added);
  - removing the last source under decision (refused);
  - an advisory switch while editing, then saving the body (no conflict).

### Next, when trellis-2f lands them

- **Wikilinks in rendered markdown are plain text.** `[[slug]]` should
  render as a link to the entry, a stub looking like one. `MarkdownContent`
  needs a remark plugin that resolves targets against the entry list.

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
TRELLIS-36 is closed — an entry's `path` is derived from the storage root, the
project key and the slug, and no longer stored (`db:"-"` on `Entry.Path`) — so a
copy no longer writes back into the real vault. Use a scratch home anyway: a
binary built from a branch migrates whatever `TRELLIS_HOME` points at, and one
built here has already migrated a real `~/.trellis` once.

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
