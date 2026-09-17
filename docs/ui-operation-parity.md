# Web UI operation parity

Date: 2026-09-16, compiled by session trellis-5b against the code, 26 of the
items checked a second time independently. Rechecked 2026-09-17 after the
vocabulary rename.

**Status: closed on 2026-09-17.** Every item below is built. This file is now
the record of what the web UI covers and where each piece lives, not a work
list. What is deliberately absent is under [Out of scope](#out-of-scope).

## Goal

People use the web UI as their way into Trellis, so it has to expose every
operation Trellis supports, except the ones that are agent or process
mechanics. That is what this list measured.

## D. Decisions: the question that started this

**Is there a "decisions" template?** Yes. `decision` is one of six shipped
templates: `decision`, `finding`, `glossary`, `reference`, `research` and
`runbook`, in `internal/core/templates/`. There is no `note` template; an entry
may have no template at all.

**Is there a separate directory for past decisions?** No, and there must not
be. Entries live at `<root>/projects/<KEY>/vault/<slug>.md`, a slug may hold
directories, and the vault-paths design
(`docs/superpowers/specs/2026-09-16-knowledge-paths-design.md`) says the
template is already a field and directories must not encode it again.

| ID | Item | Where it lives |
|---|---|---|
| D1 | Group the vault tree by template | `VaultNav`'s Filter and group menu → Group files by → Template. A second grouping mode of the same tree (`lib/vault-tree.ts`); directories still come from slugs only |
| D2 | Filter by template | The same menu, as checkboxes rather than chips: six templates would wrap three rows in an 18rem navigator. Provenance filters beside them (K4) |
| D3 | New entry with a template picker | `NewEntryDialog`, which asks only for what the chosen template cannot do without |

## B. Bugs (all fixed)

| ID | Bug | Fix |
|---|---|---|
| B1 | Saving an entry silently dropped its title and summary | PATCH takes `title`, `summary`, `body` and a required `version`; a stale one is 409 (7b0dbc4) |
| B2 | No label or tag UI anywhere | The card's facts column edits both (`ChipEditor`), and board tiles render them: a label on a surface, a tag in outline |
| B3 | Board navigation always went to `boards[0]` | The shell picks the board in the URL, else the project's default, else the first (`lib/boards.ts`, `defaultBoard`), and a board switcher appears when a project has more than one |
| B4 | The Artifacts section could never appear | Entries list their artifacts, and `GET /api/p/{key}/artifacts/{name}` serves the bytes |
| B5 | Web writes were attributed to `daemon:<pid>` | The UI server writes as `webActor()`: `TRELLIS_AGENT`, else `human:<user>`, else `human:web`. `GET /api/me` says who that is |

## C. Cards

| ID | Item | Where it lives |
|---|---|---|
| C1 | Add a comment | `CommentBox` at the top of the card's Timeline; `POST …/cards/{card}/comments` |
| C2 | Labels and tags on tiles and in the card | `CardTile` renders them; `ChipEditor` in the facts column changes them, applying at once |
| C3 | Archive, restore, and an archived view | The card's menu; `POST …/cards/{card}/archive` and `/restore`; the board's Archived tab reads `?archived=1`. Archived cards are a shelf, so they read as rows |
| C4 | Blocked-by, and the other relations | `RelationsEditor`; `POST`/`DELETE …/cards/{card}/relations` |
| C5 | Claim a free card, release your own | The Claim group: Claim it, Release it, and stealing. A claim past its time reads as expired, because the server lets the next caller take it; a refused claim names who holds it, from the error's own `detail` (TRELLIS-50) |
| C6 | Link a card to an entry, both sides | `EntryLinksEditor` in the facts column and "Cited by" on the entry. `POST`/`DELETE …/cards/{card}/links`, `core.CardLinks`, `core.UnlinkCardFromEntry`, and `trellis link --remove` |
| C7 | Board filters | `BoardFilters`: label and priority over the cards already loaded. Archived is its own tab, since it asks the server for the other set |
| C8 | Artifacts on cards | `ArtifactsEditor`: upload a file, link one the project already holds, unlink it, delete it. `POST /api/p/{key}/artifacts` is the one route that takes multipart rather than JSON; the boundary lets it through there alone |
| C9 | Bulk import | `ImportCardsDialog`, `POST …/cards/import`: a JSON array, landing whole or not at all |

## K. Vault

| ID | Item | Where it lives |
|---|---|---|
| K1 | Create an entry | `NewEntryDialog`, with directory and template |
| K2 | Delete an entry | `EntryMenu` → Delete entry, confirmed; `DELETE …/vault/{slug}` |
| K3 | Change template, private, tags, labels, sources or board | The facts column, each applying at once. Board needed core: `EntryEdit.Board` moves the association and `""` clears it, exposed as `vault edit --board` |
| K4 | Filters: template, provenance, cold entries | Template and provenance in the navigator's Filter and group menu; cold entries on the health page |
| K5 | Pin and unpin, with a recap and a stale marker | `EntryMenu` → Pin, through `LifecycleDialog`; the entry's Pinned group shows the recap and marks it stale. `GET /api/p/{key}/pins`, `POST`/`DELETE …/vault/{slug}/pin` |
| K6 | Backlinks from cards on the entry page | "Cited by", from the single entry's own `backlinks` |
| K7 | A vault health view | `HealthPage` at `/p/:key/health`: counts, diagnostics, duplicates, cold entries. `GET /api/p/{key}/health` |
| K8 | Global lifecycle: nominations, promote, demote, verify | The nominations queue on the health page, and the three acts in `EntryMenu`, each through `LifecycleDialog`: the CLI refuses them to an agent and points at a person, so the dialog asks what that person has to think about — the entry's name retyped, and why |
| K9 | Uptake analytics | Recall uptake on the health page, read-only |
| K10 | Version history for entries and cards | `HistoryDialog`, from either menu: the revisions kept, and the server's unified diff for the one chosen, rendered as text |

Wikilinks in a rendered body are links now (`lib/wikilinks.ts`), and one whose
entry nobody has written reads as the stub it is.

## P. Boards, columns, labels

| ID | Item | Where it lives |
|---|---|---|
| P1 | Boards: switcher, create, rename, delete, set default | The switcher in the shell; the rest in `BoardMenu`. A rename keeps the slug, because a `.trellis` marker names it; deleting a board with cards says how many, and a project always keeps one |
| P2 | Columns: add, rename, reorder, delete | `ColumnMenu` on each column header, plus `AddColumnButton` for a board started without any. Moving is left and right, because "after this one" is how the CLI takes it and how a strip reads; deleting a column with cards asks where they go |
| P3 | Labels: list, create, delete, merge | `LabelsDialog` from the board's menu. A new label has to say what it means |

## S. Search and graph

| ID | Item | Where it lives |
|---|---|---|
| S1 | Search options: method, project scope, label, limit | All four on the search page; `GET /api/search` takes `method` and `project` beside `label` and `limit`, and refuses an unknown method. A hit opens what it names |
| S2 | Graph walk with depth, relation and direction | `GraphPage` at `/p/:key/graph/:entity`, reached from a card's or an entry's menu. `GET /api/p/{key}/graph/{entity}` takes `depth`, `rel` and `reverse`; the controls live in the URL, so a walk worth showing someone is a link |

## M. Admin: one Settings area, mostly read-only

| ID | Item | Where it lives |
|---|---|---|
| M1 | Agents, and what each one holds | Settings › Agents. An actor with a claim but no agent row still appears, because the claim is what holds a card |
| M2 | Config: list, get, set, unset | Settings › General. `ui.*` and `search.vector.*` are shown read-only by design |
| M3 | Doctor diagnostics, and the version | Settings › Diagnostics. The checks come from `internal/doctor`, which `trellis doctor` runs too; the release check is a button, since it asks GitHub, and a newer release is reported as the command to run. The browser never replaces the binary serving it |
| M4 | Vector index: status, rebuild, prune, reindex, compact | Settings › Vector index, per project. Off is a state rather than a fault |
| M5 | Maintenance: prune, compact, backup, backup prune | Settings › Maintenance. `core.BackupInto` and `core.BackupsPrune` are shared with the CLI |

Settings also has Templates (list, create, edit the whole file, reinstall a
shipped one, delete) and Logs (the end of `daemon.log`, or where a supervised
daemon logs instead), which this table never listed.

## Out of scope

These are agent or process mechanics and stay out of the UI: `card next`,
`card renew`, `agent register`, `agent remind`, `recall` (including
`--record`), `vault nominate`, `daemon install`/`uninstall`/`start`/`stop`/`restart`,
`init`, `tui`, `ui`, and `update install`.

`vault nominate` is the agent's argument for promotion; the UI reads the
queue and decides, which is the human half of that pair.

## Build, test, verify

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
A binary built from a branch migrates whatever `TRELLIS_HOME` points at, and
one built here has already migrated a real `~/.trellis` once.

```bash
mkdir -p /tmp/h /tmp/r && cd /tmp/r
TRELLIS_HOME=/tmp/h trellis init --key VERIFY
TRELLIS_HOME=/tmp/h trellis board new --name "second board"
TRELLIS_HOME=/tmp/h trellis label new ux --description "Look and feel"
env -u TRELLIS_AGENT TRELLIS_HOME=/tmp/h trellis ui --port 7799   # unset it, or web writes use the agent id
```

**Browser checks.** Use the `agent-browser` CLI.

- Set the viewport, and `localStorage.theme = 'light'`.
- Screenshot every state you change.
- The session token changes on every daemon restart: re-open the URL with
  `?token=` after one, or every request comes back 401 and the page reads as
  offline.
- A Base UI menu does not open from a synthetic click. Focus its trigger and
  press Enter.

**For the user to see a change.** The token changes on every restart, so give
them the new URL.

```bash
go build -o ~/.local/bin/trellis ./cmd/trellis && systemctl --user restart trellis
```

## Traps

- **Shadcn installs.**
  - `pnpm dlx shadcn@latest add <x>` writes `import { cn } from "cn"`;
    change it to `@/lib/utils`.
  - Pipe `yes n |` into it so it does not overwrite existing components.
- **The UI audit.**
  - No arbitrary values except viewport units.
  - No `z-index` in pages: put it in a wrapper, as `ActionRow` does.
  - No raw `<button>` or `<label>`.
  - An icon inside `Button` needs `data-icon`; the rule looks at self-closing
    elements, which is what an icon is.
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
- **Base UI select.** The `items` list is where it reads the shown label from,
  so every option has to be in it, including the one that means "everything".
- **Card versions.** A move or a priority change bumps the version, so refresh
  the open card before a title or body save. A priority-only PATCH needs no
  `if_version`; a title or body replacement does.
- **A card is addressed by project, but written through its board.** The card
  detail carries `board` for that reason; taking the project's first board
  writes to the wrong one once a project has two.
- **A promoted entry is in both lists.** `GET /api/global/vault` and
  `GET /api/p/{key}/vault` both carry it — the vault holds it now, and the
  project can still find what it wrote — so a page merging the two dedupes by
  id or counts it twice.
- **Test shells.** `TRELLIS_AGENT` is set in agent shells; unset it when you
  test the human identity. `pkill -f` and `pgrep -f` patterns match your own
  shell (exit 144), so find processes by port with `ss -lptn 'sport = :5199'`.

## Vocabulary

- A card's merged comments and events are its **Timeline**; the project-wide
  stream is the **Event log**; **History** means revisions only.
- The vault is the collection; one item in it is an **Entry**. A card is
  **claimed**, one past its time has an **expired claim**, and taking one is
  **stealing the claim**.
- Several older words for these concepts are retired; `internal/vocabulary`
  holds the list and `go test ./internal/vocabulary` enforces it.
