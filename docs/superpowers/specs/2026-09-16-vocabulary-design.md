# One word per concept — design

Trellis names the same concept four ways and the same word for four things. An
agent reading `escalate` in the CLI, `promote` in the spec and `Vault` in the UI
cannot tell whether they are one operation or three. This spec fixes the
vocabulary, then renames the code to match it.

Two decisions below were made without the author present and should be checked
first: the **claim** root (§3) and keeping both senses of **note** (§4).

## 1. The rule

**One concept, one word, at every layer.** A word that reaches the CLI, JSON, a
URL, a filename, the database or the UI is the same word in all of them.
Grepping a concept must find all of it.

This is a rename, not a feature. No behaviour changes. Bugs found while
surveying are listed in §9 and stay out of this work.

## 2. The glossary

### The knowledge store

| Term | Means | Not |
|---|---|---|
| **vault** | Where entries live. Each project has a **project vault**; there is one **global vault**. | kb, knowledge base, knowledge store, library |
| **entry** | One markdown file in a vault, with frontmatter. | doc, document, item, page |
| **template** | The skeleton a new entry is built from, with its required fields. Input only; an entry does not remember which one it came from. | type |
| **type** | What an entry is: decision, finding, note, reference, research, runbook. | doc_type, kind |
| **summary** | The one-line description stored in an entry's frontmatter. | recap |
| **recap** | The text injected for a **pinned** entry. Falls back to the summary. | summary, brief |
| **brief** | What the session-start hook injects: board state plus pinned recaps. | digest |
| **pin** | An entry whose recap is injected into the brief. | |
| **marker** | The `.trellis` file binding a directory to a project. | pin |
| **artifact** | A file attached to an entry. | attachment |
| **provenance** | How an entry was ingested. | source, origin |
| **private** | An entry whose content never leaves the machine. Egress, not access. | confidential, secret, visibility |

### Promotion

| Term | Means |
|---|---|
| **nominate** | An agent argues an entry belongs in the global vault. |
| **nomination** | The record of one such argument: who, why, when. |
| **nominee** | An entry with at least one open nomination. |
| **nomination queue** | The list of nominees, ranked by evidence. |
| **promote** | A human moves an entry into the global vault. Moves, never copies. |
| **demote** | A human returns a global entry to its origin project. |
| **verify** | A human confirms a global entry still holds, resetting its clock. |
| **unverified** | A global entry past its `verify_by` date. |

`escalate` is gone. It suggests raising an incident or handing off to a human,
which is the wrong cue for an agent, and its opposite is de-escalate, not demote.
`promote`/`demote` are opposites and the newer specs already used them.

### Claims

One root replaces claim/lease/owner/holder.

| Term | Means |
|---|---|
| **claim** | Exclusive, expiring hold on a card. The verb and the noun. |
| **claimed_by** | The actor holding it. |
| **claim_until** | When it expires. |
| **release** / **renew** / **steal** | Give up, extend, take from another actor with a reason. |
| **expired** | A claim past `claim_until`. Never "stale". |

The spec's "leases, not locks" becomes **"claims expire; locks don't"**.

### Diagnostics

| Term | Means |
|---|---|
| **diagnostic** | One thing `vault lint` reports. Neutral about severity, because a stub is normal. |
| **kind** | Which diagnostic: `stub`, `broken_anchor`, `orphan`. `<adjective>_<noun>`, lowercase, underscored. |
| **stub** | A wikilink whose target does not resolve — not yet written, deleted, or cross-project. |
| **orphan** | An entry with no links in either direction. |
| **cold** | An entry nothing has read in 30 days. |
| **stale** | A derived copy that no longer matches its source: a pinned recap, or a vector. |
| **health** | The count of diagnostics, cold, stale and unverified entries, per project. |

"finding" is retired as an umbrella word; it stays an entry **type**. "dangling"
is retired in favour of stub.

**Every other use of "stale" is renamed:** an expired claim, an **unindexed**
entry, a **disconnected** update stream, a **leftover** PID or temp file, a
**lagging** private flag.

### Board

| Term | Means | Not |
|---|---|---|
| **card** | A unit of work. | task, ticket, item, issue |
| **column** | A stage on a board. The UI label is "Column". | status, lane, state |
| **board** | A workstream inside a project. | lane |
| **project** | What a directory resolves to. | workspace |
| **note** | A progress line appended to a card. Renews the claim. | comment |
| **event** | An immutable record in the log. The UI calls the feed "Activity". | |
| **blocked_by** | A link from a card to what blocks it. | dependency |
| **label** | A shared, project-defined vocabulary term. | tag |
| **tag** | Frontmatter or inline `#tag` on an entry; resolves to a label. | |
| **actor** | The identity that performed an event: `agent:…`, `human:…`, `daemon:…`. | user |

### Addressing

| Term | Means |
|---|---|
| **ref** | What a caller types: `KEY-12` for a card, `KEY/slug` for an entry. |
| **address** | The virtual path form: `/KEY/vault/<slug>`, `/GLOBAL/vault/<slug>`. |
| **path** | A filesystem path. Nothing else. |
| **slug** | The URL-safe leaf of an entry or board name. |
| **key** | A project's identifier, also the card ref prefix. |

Wikilinks to the global vault take exactly one form, the virtual-paths one:
`[[/GLOBAL/vault/slug]]`. `[[GLOBAL/slug]]` and `[[GLOBAL:slug]]` are dropped.

## 3. Open decision: the claim root

Made without the author. The alternatives were a **lease** root (`card lease`,
`leased_by`), which keeps expiry inside the noun but changes the command agents
call most, and keeping two roots while fixing only `owner` → `holder`, which
leaves the sprawl that caused this. `claim` wins because the verb agents already
type becomes the whole family, and `claim_until` states the expiry that `owner`
hid.

## 4. Open decision: note keeps both senses

A card **note** and an entry of **type** `note` stay as they are. They sit in
different namespaces — a log line on a card, versus the shapeless default for a
standalone entry — and neither rename is cheap: `card note` is the most-called
agent command and is advertised in the session-start hook. The glossary states
both senses. The rejected alternatives were `card log` and an entry type named
`memo`.

## 5. The rename map

### Storage

| Now | Becomes |
|---|---|
| `~/.trellis/projects/<KEY>/knowledge/` | `…/<KEY>/vault/` |
| `~/.trellis/global/knowledge/` | `~/.trellis/global/vault/` |

One numbered migration does both this move and the schema rename below, in that
order, in one transaction: the files move, then `entry.path` is rewritten to
match. The files are the source of truth, so the migration is verified by
dropping the database and reindexing.

### Schema

| Now | Becomes |
|---|---|
| table `knowledge`, `knowledge_label`, `knowledge_tag`, `knowledge_fts` | `entry`, `entry_label`, `entry_tag`, `entry_fts` |
| `doc_type` | `type` |
| `doc_id`, `knowledge_id` | `entry_id` |
| `review_by`, `reviewed_at` | `verify_by`, `verified_at` |
| `card.owner`, `card.lease_until` | `card.claimed_by`, `card.claim_until` |
| `link.from_type`/`to_type` value `'doc'` | `'entry'` |
| `event.entity_type` value `'knowledge'` | `'entry'` |
| `event.action` `escalated`, `privatised`, `default` | `promoted`, `privatized`, `set_default` |

Event rows are rewritten by the migration. The log is history, but the words in
it are vocabulary, and a UI that renders raw actions must not show two.

FTS5 external-content tables are rebuilt rather than renamed in place.

### CLI

| Now | Becomes |
|---|---|
| `trellis knowledge …` | `trellis vault …` |
| `knowledge escalate` | `vault promote` |
| `knowledge lint` JSON `findings` | `diagnostics` |
| `init --pin` | `init --marker` |
| `card ls` JSON `owner`, `lease_until` | `claimed_by`, `claim_until` |
| `{"knowledge": […]}` | `{"entries": […]}` |
| search/recall hit `kind: knowledge` | `kind: entry` |
| graph node `type: doc` | `type: entry` |
| `unreviewed` | `unverified` |

Error codes standardise on `<noun>_not_found`: `entry_not_found`,
`board_not_found`, `column_not_found`. `unknown_board` and `unknown_column` are
dropped. `project_has_vault_entries` becomes `project_has_global_entries`, which
is what it counts. `not_owned` splits: `not_yours` when you do not hold the
claim, `contention` when another actor does.

### Go

| Now | Becomes |
|---|---|
| type `Knowledge`, vars `doc`/`docs` | `Entry`, `entry`/`entries` |
| `loadDoc`, `docView`, `resolveDocRef`, `RenderDoc`, `DocType`, `LinkCardToDoc` | `loadEntry`, `entryView`, `resolveEntryRef`, `RenderEntry`, `EntryType`, `LinkCardToEntry` |
| `kbDir`, `kbRoot`, `kbCore`, `WithKBRoot` | `vaultDir`, `vaultRoot`, `vaultCore`, `WithVaultRoot` |
| `EscalateKnowledge` | `PromoteEntry` |
| `RenewLease` | `RenewClaim` |
| `NewCardID` (mints ids for notes, pins, labels, artifacts) | `NewID` |
| `resolve.Pin`, `findPin`, `pinBoundary` | `resolve.Marker`, `findMarker`, `markerBoundary` |
| `LintFinding` | `Diagnostic` |
| `MaxInjectedPins` | unchanged |

`internal/core/knowledge.go` becomes `entry.go`. `pin.go` currently holds pins,
nominations, promotion and verification; it splits into `pin.go`,
`nomination.go` and `promote.go`.

### Web and TUI

The UI adopts the code's words: "Status" → "Column", "Kind" → "Type",
"Attachments" → "Artifacts", "Take the lease" → "Steal the claim", "Held by" →
"Claimed by", "Stale leases" → "Expired claims", "Unlinked" → "Orphans",
"Documents" → "Entries". The nav item "Vault" scopes to a project vault or the
global vault explicitly. Raw event actions stop reaching the screen; each action
gets a label.

### Docs, skills and the hook

README, CLAUDE.md, PRODUCT.md, the plugin manifests, `plugin/hooks/trellis_hook.py`
and all four skills. The hook's command list is the only channel that reaches
every session, so it must name the new commands exactly. The `writing-knowledge`
skill lists types — Failed approach, Measurement, Trap, Convention — that no
template ships; it is corrected to the shipped list.

The four specs and plans written today (virtual paths, project merge, pin-only
projects, disclosure) are unstarted and use the old words throughout. They are
reworded in the same pass: `/KEY/knowledge/` becomes `/KEY/vault/`, pin-only
projects becomes marker-only projects, and "corpus" becomes "the vector index".

## 6. Order of work

Each layer builds, tests and is committed before the next. No layer leaves the
product unusable.

1. **Glossary.** `docs/glossary.md`, plus the rule in CLAUDE.md. No code.
2. **Schema and core.** Migration, `Entry`, claims, verify, promote. JSON tags
   are left alone here, so every output stays byte-identical and the product
   keeps working while its insides are renamed.
3. **CLI.** `trellis vault`, `promote`, `--marker`, then the JSON tags and error
   codes — the one layer where output changes.
4. **Web and TUI labels.**
5. **Hook, skills, README, PRODUCT, and the four unstarted specs and plans.**
6. **Glossary skill and pinned entry.**

## 7. The glossary's three homes

`docs/glossary.md` is canonical: a fresh clone can read it, and it is reviewed
in the same diff as the code that uses the words.

A **skill** manages the glossary rather than restating it, in the shape of
Anthropic's `memory-management` skill: a tiered lookup (the pinned recap, then
`docs/glossary.md`, then ask), a rule to search before naming anything new, how
to add a term when one is coined, how to record a rename, and when to prune. It
carries no term list, so it cannot drift.

A **pinned vault entry** in TRELLIS holds a short recap — the rule, and where
the table lives — so every session starts knowing the vocabulary exists.

## 8. Testing

- A migration test: a database written with the old schema, migrated, then
  compared against one reindexed from the files.
- Round trip: create an entry, drop the database, reindex, confirm the ref,
  address and path are unchanged.
- `TestHelpListsEveryCommand` already enforces that every command is visible
  with a `Short`; it must pass against the renamed tree.
- A vocabulary test: grep the tree for the retired words — `kb`, `knowledge
  base`, `escalate`, `doc_type`, `owner` as a card field, `lease_until`,
  `unreviewed`, `dangling` — and fail on any hit outside `docs/glossary.md` and
  the migration that renames them. It runs over Go, SQL, TypeScript, Python and
  markdown, with one allowlist file for the exceptions. This is what keeps the
  rename from decaying.
- Cross-platform: the storage move runs on Windows CI, where the rename of an
  open file behaves differently.

## 9. Not in this work

Bugs found while surveying. Each becomes a card:

- `card next --claim` records no `claimed` event, so those claims never reach
  the activity feed.
- `unverified` is hard-coded to 0 in vector and hybrid search.
- `ListKnowledge` does not exclude global rows, so a project's list and the
  global list can overlap.
- Hints name things that do not exist: `knowledge ls --global`, "escalate from
  the browser", and demote as a way to delete entries.
- A demoted entry's old nominations return to the queue.
- Card notes are recorded as their own entity, so card history never shows them.
- The Overview "Pinned" list is really "has a recap", which is wrong in both
  directions.
- The lamp announces "Blocked" for urgent cards.
- `in_progress` counts every card that is not done, backlog included.
- A seeded `blocked` label duplicates `blocked_by` links.
- `scripts/ui-browser-smoke.sh` waits for text the UI no longer renders.
