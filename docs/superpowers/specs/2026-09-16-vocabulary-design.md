# One word per concept — design

Trellis names the same concept four ways and the same word for four things. An
agent reading `escalate` in the CLI, `promote` in a spec and `Vault` in the UI
cannot tell whether they are one operation or three. This spec fixes the
vocabulary, then renames the code, help text and flags to match it. It also
gives agents a way to keep a glossary in Trellis for any project (§8).

**Two deliveries, two plans.**

- **The general rename runs last.** It lands after every other branch has
  merged into `feat/memory-groundwork`. Plan: `2026-09-16-vocabulary.md`.
- **The glossary skills do not wait for it.** They can land as soon as
  `wip/template` has merged. Plan: `2026-09-16-glossary-skills.md`.

Everything below is written against concepts, not line numbers, because the
tree will move before the rename starts.

## Decisions

| Question | Decision | By |
|---|---|---|
| The knowledge container | **vault**: a project vault per project, one global vault | author |
| The CLI noun | `trellis vault` | author |
| One item in a vault | **entry** | author |
| Moving an entry to the global vault | **promote**, opposite **demote** | author |
| The nomination family | **nominate → nomination → nominee** | author |
| The global review clock | **verify / unverified** | author |
| What lint reports | **diagnostic** | author |
| How deep the rename goes | every layer: CLI, JSON, URLs, disk, Go, SQL | author |
| `type` versus `template` | **template** is the only classification; no template means no shape. **Delivered by `wip/template`** (now inside migration 0014), not by this rename | author |
| Card notes | **comment**: `card comment`, table `comment`, many per card. **Delivered by `wip/comments`** (now inside migration 0014), not by this rename | author |
| The `.trellis` file versus a pinned entry | entries keep **pin**; the file is the **marker** | author |
| The glossary skills | separate skills that teach an agent to keep a glossary in Trellis and to use it; they carry no term list | author |
| When the rename happens | **last**, after everything else merges | author |
| claim / lease / owner / holder | **claim** as the one root | author |
| The event stream | **event log**; "feed" and "Activity" retired | author |
| A card's comments and events shown together | **timeline**, replacing the name "history" for that view; **history** means revisions only | author |
| The template rule `verify:` | renamed **`resolve:`**, freeing verify for the global vault. `required:` was considered and rejected: it already names the mandatory-fields rule | author |
| How a glossary is stored | an entry built from a shipped `glossary` template. **Not pinned by default**: the skill asks the user. Kept simple | author |
| Trellis's own glossary | an entry in the TRELLIS vault, like any other glossary. No `docs/glossary.md` | author |

## 1. The rule

**One concept, one word, at every layer.** A word that reaches the CLI, a flag,
help text, JSON, a URL, a filename, the database or the UI is the same word in
all of them. Grepping a concept must find all of it.

Three consequences:

- **No abbreviations of a glossary word.** `kb` and `dupes` are the same words
  as vault and duplicate, spelled so that grep misses them.
- **A generic attribute is scoped to its object.** `kind` on a diagnostic and
  `kind` on an agent are fine. `kind` used where a domain word exists — an
  event's entity — is not.
- **A glossary word is never borrowed for something else.** "Claim" in help
  text means a card claim, never an assertion. "Path" means a filesystem path,
  never an ingestion route.

The rename is a rename, not a feature. No behaviour changes. Bugs found while
surveying are listed in §10 and stay out of this work.

## 2. The glossary

### Vault

| Term | Means | Not |
|---|---|---|
| **vault** | Where entries live. Each project has a **project vault**; there is one **global vault**. | kb, knowledge base, knowledge store |
| **entry** | One markdown file in a vault, with frontmatter. | doc, document, item, knowledge (as a count noun) |
| **template** | The shape an entry follows, and the name it records in `template:`. Enforced on every Trellis write; lint reports hand edits that break it. An entry without one has no shape. | type, doc_type, kind, category |
| **summary** | The one-line description in an entry's frontmatter. | |
| **recap** | The text injected for a pinned entry. Defaults to the summary. | summary |
| **brief** | What the session-start hook injects: board state and pinned recaps. | injection brief, digest |
| **pin** | An entry whose recap is injected into the brief. | |
| **marker** | The `.trellis` file binding a directory to a project. | pin |
| **artifact** | A file linked to a card or an entry. | attachment |
| **cite** | A link into an entry, from a card or another entry. `cited` counts them. | documents |
| **source** | Evidence an entry names for what it says. | claim |
| **provenance** | How an entry was ingested. | ingestion path, origin |
| **private** | An entry whose content never leaves the machine. Egress, not access. | confidential, visibility |
| **body** | An entry's markdown after the frontmatter. | content |
| **directory** | A folder inside a vault. `vault new --in`, `vault ls <directory>`. | folder, collection |
| **tree** | A vault listed by directory. | |
| **leaf** | The last segment of an entry's slug. A bare leaf names an entry when it is unique. | |
| **revision** | A retained earlier copy of an entry or card. | |
| **history** | An entry's or card's retained revisions, oldest first. | timeline, activity, event log |
| **diff** | The change between two revisions. | |
| **version** | The counter an edit must name with `--if-version`. Revisions are identified by it. | |
| **duplicate cluster** | Entries `vault health --duplicates` groups as likely the same. | dupe, near-duplicate |
| **look-alike directory** | A new directory name close to an existing one. | duplicate |
| **glossary** | An entry built from the `glossary` template: one table of Term, Means, Not. A project keeps one. | dictionary, vocabulary list |

A template's frontmatter carries its **rules**: **enforce** (`reject` or
`warn`), **required** (fields that must be supplied), **choices** (allowed
values) and **resolve** (fields whose internal references must resolve). The
last was `verify`, which is the global-vault word.

### Promotion

| Term | Means |
|---|---|
| **nominate** | An agent argues an entry belongs in the global vault. |
| **nomination** | The record of one such argument: who, why, when. |
| **nominee** | An entry with at least one nomination. |
| **promote** | A human moves an entry into the global vault. Moves, never copies. |
| **demote** | A human returns a global entry to its origin project. |
| **verify** | A human confirms a global entry still holds, resetting `verify_by`. |
| **unverified** | A global entry past its `verify_by` date. |

`escalate` is retired: it suggests raising an incident, and its opposite is
de-escalate, not demote.

### Claims

| Term | Means | Not |
|---|---|---|
| **claim** | Exclusive, expiring hold on a card. Verb and noun. | lease, lock, ownership |
| **claimant** | The actor holding a claim, stored as `claimed_by`. | owner, holder |
| **claim_until** | When the claim expires. | lease_until |
| **release** / **renew** / **steal** | Give up, extend, take from another actor with a reason. | take |
| **expired** | A claim past `claim_until`. | stale |

The spec's "leases, not locks" becomes **"claims expire; locks don't"**.

### Diagnostics

| Term | Means | Not |
|---|---|---|
| **diagnostic** | One thing `vault lint` reports. Neutral about severity. | finding, problem, violation, issue |
| **stub** | A wikilink whose target does not resolve: not yet written, deleted, or cross-project. | dangling |
| **orphan** | An entry with no links in either direction. | unlinked |
| **cold** | An entry nothing has read in 30 days. | |
| **stale** | A derived copy that no longer matches its source: a pinned recap, a vector. | |
| **leftover** | A file left behind: a temp file, a PID file, a revision directory whose entry is gone. | orphan, stale |
| **unindexed** | An entry the vector index has not embedded yet. | stale |
| **health** | The vault's tidiness counts. The daemon's liveness is **ping**. | |
| **check** | One thing `doctor` tests about the installation. | diagnostic |

A diagnostic's **kind** is lowercase snake_case naming the condition:
`stub`, `broken_anchor`, `orphan`, `missing_artifact`, `template_violation`,
`unknown_template`. "finding" stays only as a template name.

**conflict** means two versions of one thing disagree and nothing is written
until someone picks: an edit made against an old version, or two projects
holding different content under one name during a merge.

### Board

| Term | Means | Not |
|---|---|---|
| **card** | A unit of work. | task, ticket, item, entry |
| **column** | A stage on a board. The UI label is "Column". | status, lane |
| **done column** | A column whose cards count as finished. | terminal |
| **board** | A workstream inside a project. | lane |
| **project** | What a directory resolves to. | workspace |
| **comment** | A line appended to a card. A card has many. | note |
| **timeline** | A card's comments and events, shown together in the UI. | history, activity |
| **archive** / **restore** | Take a card off the board; put it back. | unarchive |
| **event** | An immutable record of one change. | |
| **event log** | Every event, in order. `trellis events` reads it; the UI page is "Events". | feed, activity, history |
| **seq** | An event's position in the event log. | |
| **consumer** | A named cursor over the event log. | subscriber |
| **ack** | Advance a consumer's cursor. | |
| **extension** | A program outside Trellis that reads the event log and its `extensions.<name>` config. | plugin |
| **entity** | What an event is about: card, entry, board, label, comment. | kind |
| **merge** | Fold one label or project into another. | |
| **collapse** | Drop an identical duplicate during a merge. | dedupe |
| **blocked_by** | A link from a card to what blocks it. | dependency |
| **label** | A term from the project's controlled list: created explicitly, described, enforced. | |
| **tag** | A free-form word, created on first use. Not a label. | |
| **actor** | The identity behind an event: `agent:…`, `human:…`, `daemon:…`. | user |

### Addressing

| Term | Means |
|---|---|
| **ref** | The `ref` a command prints and accepts: `KEY-12` for a card, the address for an entry. A bare slug also names an entry in the current project. |
| **address** | `/KEY`, `/KEY/boards/<slug>`, `/KEY/cards/<ref>`, `/KEY/vault/<slug>`, `/GLOBAL/vault/<slug>`, `/KEY/artifacts/<name>`. Replaces "virtual path". |
| **path** | A filesystem path. Nothing else. |
| **slug** | An entry's vault-relative path without `.md` (`deployment/rollback`), or a board's URL-safe name. |
| **key** | A project's identifier, also the card ref prefix. |

Argument placeholders follow the noun: `<card>`, `<entry>`, `<artifact>`,
`<board>`. An `<entry>` is a slug, a ref or an address; the `vault` command's
Long text says so once.

## 3. Assumed: the claim root

Made without the author. The alternatives were a **lease** root (`card lease`,
`leased_by`), which keeps expiry inside the noun but changes the command agents
call most, and keeping two roots while fixing only `owner`. `claim` wins because
the verb agents already type becomes the whole family.

## 4. Delivered elsewhere

Two parts of this vocabulary are being built ahead of the rename, and the rename
must neither redo nor contradict them.

- **`wip/template`:**
  - `template` is the only classification, and the frontmatter key is `template:`.
  - The column is `knowledge.template`; the rename later changes only the table's name.
  - `--template` replaces every `--type`, and the JSON key is `template`.
  - The `note` template is gone.
  - Templates are enforced on every Trellis write, and lint reports `template_violation` and `unknown_template`.
- **`wip/comments`:**
  - Card notes become comments: `trellis card comment`, table `comment`, API `/comments`, JSON `comments`, event entity `comment`.
  - The UI shows a card's comments and events together as its **timeline**.

Since 3564b86, this release's migrations are collapsed into one Go migration,
`0014_memory_groundwork.go`, which includes both of these. The rename's
migration is 0015, and it treats
`template` and `comment` as already correct.

## 5. The rename map

### Storage

| Now | Becomes |
|---|---|
| `~/.trellis/projects/<KEY>/knowledge/` | `…/<KEY>/vault/` |
| `~/.trellis/global/knowledge/` | `~/.trellis/global/vault/` |
| address segment `/<KEY>/knowledge/`, `/GLOBAL/knowledge/` | `/<KEY>/vault/`, `/GLOBAL/vault/` |
| template rule `verify:` | `resolve:` |
| config key `lease.ttl` | `claim.ttl` |

The directory moves take everything under the old directory, revision directories included. Then, through the atomic writer:

- every entry file has its addresses rewritten;
- every template file has its `verify:` rule renamed `resolve:`;
- `config.yaml` renames `lease:` to `claim:`.

Addresses are also rewritten in card bodies, comments and `link.to_raw`, so no
link written before the rename becomes a stub. Retained revisions and event
values are history and keep what they said.

This is one Go migration; goose runs Go migrations alongside SQL ones. Files
move first, then the schema changes in one transaction, and a failed schema step
puts the files back. A rewritten file is not an external edit: the migration
carries each row's hash, and each current recap's hash, over to the new bytes.

### Schema

| Now | Becomes |
|---|---|
| tables `knowledge`, `knowledge_label`, `knowledge_tag`, `knowledge_fts`, `knowledge_search_state` | `entry`, `entry_label`, `entry_tag`, `entry_fts`, `entry_search_state` |
| indexes and triggers named `knowledge_*` | `entry_*` |
| `doc_id`, `knowledge_id` | `entry_id` |
| `review_by`, `reviewed_at` | `verify_by`, `verified_at` |
| `card.owner`, `card.lease_until` | `card.claimed_by`, `card.claim_until` |
| `link.from_type`/`to_type` `'doc'` | `'entry'` |
| `link.rel` `'documents'` | `'cites'` |
| `event.entity_type` `'knowledge'` | `'entry'` |
| `event.action` `escalated`, `unarchived`, `privatised`, `default` | `promoted`, `restored`, `privatized`, `set_default` |
| `event.field` `owner`, `lease_until`, `review_by` | `claimed_by`, `claim_until`, `verify_by` |
| `project_config` key `lease.ttl` | `claim.ttl` |

Event rows are rewritten. The log is history, but its words are vocabulary, and
the UI renders them. The migration test fails on any schema object still named
in a retired word, which catches objects added after this spec was written.

### Commands

| Now | Becomes |
|---|---|
| `trellis knowledge …` | `trellis vault …` |
| `knowledge escalate` | `vault promote` |
| `knowledge template …` | `vault template …` |

### Flags

| Now | Becomes |
|---|---|
| `events --kind card\|knowledge\|board\|label\|comment` | `--entity card\|entry\|board\|label\|comment` |
| `artifact link/unlink --doc` | `--entry` |
| `knowledge health --dupes` | `vault health --duplicates` |
| `maintenance prune --orphan-history` | `--leftover-revisions` |

### JSON

| Now | Becomes |
|---|---|
| `{"knowledge": […]}` | `{"entries": […]}` |
| `owner`, `lease_until`, contention `holder` | `claimed_by`, `claim_until` |
| lint `findings` | `diagnostics` |
| `unreviewed` | `unverified` |
| search and recall hit `kind: knowledge` | `kind: entry` |
| graph node `type: doc` | `type: entry` |
| `trellis events` line `kind` | `entity` |
| web API event rows `entity_type` | `entity` |
| card detail `activity` | `events` |
| `orphan_history` (`GET /api/maintenance`, `POST /api/maintenance/prune`) | `leftover_revisions` |
| setting key `lease.ttl` in `/api/settings` | `claim.ttl` |
| `init` output `pin_path`, `pin_written` | `marker_path`, `marker_written` |
| `/api/projects` `stale_leases` | `expired_claims` |
| `vector status` `stale_documents` | `unindexed_entries` |
| duplicate `clusters` | `duplicate_clusters` |
| nomination `noms` | `nominations` |

### Error codes

- `<noun>_not_found` everywhere: `entry_not_found`, `board_not_found`,
  `column_not_found`. `unknown_board` and `unknown_column` are dropped.
- `project_has_vault_entries` → `project_has_global_entries`, which is what it
  counts.
- `project_leased` → `project_has_claims`.
- `not_owned` splits: `not_yours` when you do not hold the claim, `contention`
  when another actor does.
- `bad_pin` → `bad_marker`; any other code naming the file's pin sense follows.

### Go

| Now | Becomes |
|---|---|
| type `Knowledge`, vars `doc`/`docs` | `Entry`, `entry`/`entries` |
| `loadDoc`, `docView`, `resolveDocRef`, `resolveDocStubs`, `RenderDoc`, `LinkCardToDoc`, `matchKnowledge` | `loadEntry`, `entryView`, `resolveEntryRef`, `resolveEntryStubs`, `RenderEntry`, `LinkCardToEntry`, `matchEntries` |
| `KnowledgeFilter`, `ListKnowledge`, `CreateKnowledge`, … | `EntryFilter`, `ListEntries`, `CreateEntry`, … |
| `kbDir`, `kbRoot`, `kbCore`, `WithKBRoot`, the event log's `KB*` fields | `vaultDir`, `vaultRoot`, `vaultCore`, `WithVaultRoot`, `Entry*` |
| `EscalateKnowledge` | `PromoteEntry` |
| `UnarchiveCard` | `RestoreCard` |
| `RenewLease` | `RenewClaim` |
| `NewCardID` (mints every id) | `NewID` |
| `resolve.Pin`, `PinFile`, `FindPin`, `ReadPin`, `PinError`, `PinPath` | `resolve.Marker`, `MarkerFile`, `FindMarker`, `ReadMarker`, `MarkerError`, `MarkerPath` |
| package `vpath`, type `vpath.Path` | package `address`, type `address.Address` |
| `vpath.ParsePin` (reads a marker's contents) | `address.ParseMarker` |
| `vpath.CollectionKnowledge`, `KnowledgePath`, `GlobalKnowledgePath`, `CardPath`, `ArtifactPath`, `ProjectPath` | `address.CollectionVault`, `address.Entry`, `address.GlobalEntry`, `address.Card`, `address.Artifact`, `address.Project` |
| `DocAddress` | `EntryAddress` |
| `Template.Verify`, `template_verify.go` | `Template.Resolve`, `template_resolve.go` |
| `EventFeed`, `FeedEvent` | `EventLog`, `LogEvent` |
| `LintFinding` | `Diagnostic` |
| `DupeCluster`, `dupes.go` | `DuplicateCluster`, `duplicates.go` |

`internal/core/knowledge.go` becomes `entry.go`. `pin.go` holds pins,
nominations, promotion and verification; it splits into `pin.go`,
`nomination.go` and `promote.go`.

### Web and TUI

The UI adopts the code's words, and the event log gets its own name:

- **Labels:** "Status" → "Column", "Kind" → "Template", "Visibility" → "Private", "Attachments" → "Artifacts", "Take the lease" → "Steal the claim", "Held by" → "Claimed by", "Stale leases" → "Expired claims", "Unlinked" → "Orphans", "Documents"/"Document" → "Entries"/"Entry", "Activity" → "Events".
- **Nav:** the item "Vault" names which vault it shows.
- **Routes:** `/p/:key/knowledge` → `/p/:key/vault`; `/api/global/knowledge` → `/api/global/vault`; `/api/activity` → `/api/events`; every `/api/p/{key}/…/knowledge…` → `…/vault…`, including `/api/p/{key}/links/knowledge`. `/api/templates/*`, `/api/settings`, `/api/logs` and `/api/maintenance/*` keep their paths. The web code itself is changed by the UI session, from the list in the rename plan's Task 12.
- **Raw event actions** stop reaching the screen; each gets a label.
- **localStorage keys** already say `vault`.

The card's timeline belongs to `wip/comments`; its label is "Timeline", not "History".

### Docs, skills and the hook

This covers README, CLAUDE.md, PRODUCT.md, the plugin manifests,
`plugin/hooks/trellis_hook.py`, every skill, and `scripts/`.

- **The hook:** its command list is the only channel that reaches every
  session, so it names the new commands exactly.
- **`writing-knowledge`:** the skill lists categories that no template ships —
  Failed approach, Measurement, Trap, Convention. It is corrected to the
  shipped templates.

`docs/superpowers/` is dated history and is not rewritten, with one exception: a
spec or plan that has not been executed when this work starts is reworded, so
it is not built with the old words.

## 6. Help text and flags

Audited against the tree at `9bcd869`. The `--type` flags and `card note` rows
are gone from this table, because `wip/template` and `wip/comments` change them.
Re-run the audit at the start of the rename. Commands added after that commit
are covered by the vocabulary test in §9, not by this table.

| Command | Now | Becomes |
|---|---|---|
| `trellis` | Local kanban and knowledge base for AI agents | Local kanban boards and vaults for AI agents |
| `agent ls` | List agents and what they hold | List agents and the cards they have claimed |
| `agent remind` | Report cards you hold with no note | Report claimed cards with no comment |
| `artifact` | Store files and attach them to cards or knowledge entries | Store files and link them to cards or entries |
| `artifact` Long | Attaching one to a knowledge entry adds … | Linking one to an entry adds … |
| `artifact link` | Attach an existing artifact to a card or a knowledge entry | Link an existing artifact to a card or an entry |
| `artifact unlink` | Detach an artifact from a card or a knowledge entry | Unlink an artifact from a card or an entry |
| `artifact unlink` Long | Detaching from an entry … | Unlinking from an entry … |
| `artifact rm` | … keep the name as a stub | … keep the name, and lint reports it |
| `--card` / `--doc` | a knowledge entry slug | `--entry`: an entry |
| error `missing_target` | pass --card <ref> or --doc <slug> | pass --card <card> or --entry <entry> |
| `board show --brief` | show as injection brief | show as the session brief |
| `card claim` | Claim ownership of a card | Claim a card |
| `card claim/renew/next --ttl` | lease duration in minutes | claim duration in minutes |
| `card claim --steal` | take it from a quiet holder | steal the claim from a quiet claimant |
| `card release` | Release ownership of a card | Release your claim on a card |
| `card renew` | Extend the lease on a card you own | Extend your claim on a card |
| `card archive` | Archive a card, releasing any lease | Archive a card, releasing any claim |
| `card archive --restore` | return an archived card to the board | restore an archived card to its board |
| `column add --done` | mark as terminal | make this a done column |
| `column rm` | Remove a column | Delete a column |
| `doctor` | Diagnose this Trellis installation | Check this Trellis installation |
| `events` | Read the event feed | Read the event log |
| `events --kind` | card\|knowledge\|board\|label\|… | `--entity`: card\|entry\|board\|label\|comment |
| `events --template` | knowledge templates (repeatable) | templates (repeatable) |
| `import` Long | so an entry can depend on one that has no reference yet | so a card can depend on one that has no ref yet |
| `init` | Pin this directory to a project, creating the project if needed | Mark this directory with a project, creating the project if needed |
| `project new` | Create a project without pinning any directory | Create a project without marking any directory |
| `project merge` Long | Move every board, card, knowledge entry and artifact of SRC into DST, keep card refs such as SRC-12 working, and retire SRC's key. Without --apply the merge only reports what it would do; with --apply it backs up first. A document or artifact that both projects name, with different content, stops the merge; --rename-conflicts renames SRC's side instead. Pins that name SRC under the enclosing repository are rewritten: commit them. | Move every board, card, entry and artifact of SRC into DST, keep card refs such as SRC-12 working, and retire SRC's key. Without --apply the merge only reports what it would do; with --apply it backs up first. An entry or artifact that both projects name, with different content, stops the merge; --rename-conflicts renames SRC's side instead. Markers that name SRC under the enclosing repository are rewritten: commit them. |
| `knowledge` | Work with knowledge entries | `vault`: Work with entries in the project and global vaults |
| `new` | Create a knowledge entry | Create an entry |
| `new --body` | markdown body (default: the template) | markdown body (default: the template's skeleton) |
| `new --provenance` | ingestion path: … | how the entry was ingested: … |
| `new --source` | cite what a claim is based on: … | evidence for what the entry says: … |
| `new --label` | labels from the project vocabulary | labels defined in this project |
| `new --board` | associate with a board (association, never ownership) | associate with a board (association, never a claim) |
| `new --private` | do not transmit this body automatically: no vector index, no recap, no content in the event log, pointer-only injection | do not transmit this body automatically: no vector index, no recap, no body in the event log, pointer-only injection |
| `show`, `edit`, `rm`, `pin`, `nominate`, `promote`, `demote`, `verify`, `history`, `diff` | `<slug>` | `<entry>` |
| `mv` | `<ref> <new-path>` | `<entry> <new-path>` |
| `edit --if-version` | … (knowledge show --json) | … (vault show --json) |
| `ls --provenance`, `recall --provenance` | only these ingestion paths | only entries with these provenances |
| `health --dupes` | list duplicate clusters instead | `--duplicates`: list duplicate clusters instead |
| `pin` | Pin an entry's recap into every session start | Pin an entry, injecting its recap into the session brief |
| `pin --recap` | the summary to inject; you write it … | text to inject, defaulting to the entry's summary; you write it … |
| `pins` | List what session start injects | List pinned entries |
| `lint` | Report stubs, broken anchors and orphans | Report diagnostics: stubs, broken anchors, orphans, … (every kind the code emits) |
| `nominate` | Propose an entry for the global vault | Nominate an entry for the global vault |
| `nominations` | The escalation queue, with its evidence | List nominees, ranked by evidence |
| `escalate` | Move an entry to the global vault (human only) | `promote`: same text |
| `verify` | Reset the review clock on a global entry | Confirm a global entry still holds, resetting verify_by (human only) |
| `uptake` | How often a recalled identifier was then opened, by ingestion path | How often a recalled ref was then opened, by provenance |
| `template` | List, show and edit the shared knowledge templates | List, show and edit the shared templates |
| `template check` | `<name> <slug>`: Report a document's violations of a template | `<name> <entry>`: Report an entry's diagnostics against a template |
| human gate | agents nominate; humans escalate | agents nominate; humans promote |
| `link` | `<card> <slug[#anchor]>`: Link a card to a knowledge entry | `<card> <entry[#anchor]>`: Link a card to an entry |
| `graph` | `<card\|slug>`, `--rel blocked_by, documents, wikilink` | `<card\|entry>`, `--rel blocked_by, cites, wikilink` |
| `maintenance prune --orphan-history` | remove revision directories whose entry file is gone | `--leftover-revisions`: same text |
| `recall` | Surface knowledge and cards bearing on a passage of text | Surface entries and cards bearing on a passage of text |
| `recall` Long | returns identifiers … | returns refs … |
| `recall --record` | note each hit as injected, so `knowledge uptake` … | record each hit as injected, so `vault uptake` … |
| `search` | Search cards and knowledge entries | Search cards and entries |
| `tui` | Open the interactive terminal workspace | Open the interactive terminal interface |
| `vector` | Manage the optional document vector index | Manage the optional vector index |
| `vector status` | Show vector configuration and index health | Show vector configuration and index coverage |
| `vector rebuild` | Embed and rebuild the current project's document index | Embed entries and rebuild the current project's vector index |
| `vector prune` | Remove vectors for documents no longer in the knowledge base | Remove stale vectors, whose entry has left the vault |
| `--as` comment | claiming, releasing, noting and editing all record or check an owner | … record or check a claimant |

Every error hint that names a command follows the command rename:
`trellis knowledge show`, `knowledge new`, `knowledge ls`, `knowledge mv`,
`knowledge lint`, `knowledge pins`, `knowledge nominate`, `knowledge demote`,
and the rest.

## 7. Order of work

The rename starts only after every other branch has merged, including
`wip/template` and `wip/comments`. Each layer builds, tests and is committed
before the next; no layer leaves the product unusable.

1. **Glossary and gate.** Trellis's glossary entry from §2, the rule in
   CLAUDE.md, and the vocabulary test from §9 with every current hit
   allowlisted. The
   allowlist is the work queue: each later layer deletes lines from it.
2. **Storage, schema and core.** The Go migration, then the Go renames in §5.
   Command names, flags and JSON keys are left alone. Stored values the
   migration rewrites change here, because the code that writes them has to
   change in the same commit: event actions, entity names, link relations,
   addresses, the config key.
3. **CLI.** Commands, flags, help text, error codes, then JSON tags. This is the
   one layer where output changes.
4. **Web and TUI.**
5. **Hook, skills, README, PRODUCT, CLAUDE.md, scripts**, and any unexecuted
   spec or plan.
6. **Trellis's glossary entry** brought up to date, and pinned only if the
   author says so. The allowlist holds only the lines §9 says it may keep.

## 8. Glossaries in Trellis

A glossary is an ordinary entry, so any project can keep one. The design
borrows from Anthropic's `productivity` plugin — its `memory-management`
skill's glossary, and `/update`'s habit of proposing additions rather than
making them — and keeps only what Trellis needs.

**The `glossary` template** ships beside the others. Its skeleton is a
`## Terms` section holding one table: Term, Means, Not. Its one rule is that
section, so a Trellis write cannot drop it. `vault ls --template glossary`
finds a project's glossary, and a project keeps one.

**Pinning is the user's choice.** A pinned glossary's recap reaches every
session through the brief. That costs tokens on every read, so the entry is not
pinned unless the user says so. If it is pinned, the recap stays one line.

**Two skills**, neither carrying terms. The core `trellis` skill stays CLI
mechanics, and the hook's judgment-skill list names both new skills.

- **`using-glossary`** — before naming anything others will see (a command,
  flag, field, table, UI label, help string, title), or when a user's word is
  unclear:
  - Check the brief's recap if the glossary is pinned, then the entry itself.
    Ask the user only if neither answers.
  - Use the **Term**. A word in a **Not** column is an existing concept under
    that Term.
  - Never coin a synonym. If the word is missing, hand over to
    `keeping-glossary`.
- **`keeping-glossary`** — when the user says what a word means, something new
  needs a name, a term is renamed or retired, or the user asks for a glossary:
  - **Start:** propose rows from the user's own words, ask, create the entry
    from the template, then ask whether to pin it.
  - **Change:** read the entry, change one row, and write it back with
    `--if-version`. Never retype the table from memory.
  - **What to record:** record what the user states; propose what you infer.
    A rename moves the old word into **Not**, and a retired concept's row is
    deleted.
  - **Keep it in one place:** if pinned, the recap stays one line. The glossary
    is never copied into CLAUDE.md or harness memory.

**Trellis's own glossary** is an entry in the TRELLIS vault, like any other. It
is written from §2 when the rename starts, so the rename has one place to look
words up, and it is pinned only if the author says so.

## 9. Testing

- **Vocabulary test.**
  - Scans Go, SQL, TypeScript, Python and markdown outside `docs/superpowers/`
    for the retired words: `kb`, `knowledge base`, `escalate`, `doc_type`,
    `DocType`, `dupe`, `lease`, `owner` as a claim, `holder`, `unreviewed`,
    `dangling`, `orphan-history`, `ingestion path`, `unarchive`, `feed`,
    `noms`, `card note`, the file sense of `pin`, and the `/knowledge/` address
    segment.
  - Fails on any hit not in its allowlist.
  - Migrations are excluded by path, because they must name the old words.
  - When the work ends, the allowlist holds only hits where a rule cannot tell a
    second, legitimate meaning apart, such as `owner` as an example template
    field. Each such line carries a comment saying why.
  - The test keeps the rename from decaying, and catches commands added after
    the audit.
- **Help test.** `TestHelpListsEveryCommand` already walks every command. It
  also runs every `Short`, `Long` and flag usage string through the vocabulary
  test.
- **Migration test.**
  - A storage root and database at the schema just before the migration, with
    old-style directories, addresses, template rules and stored words, is
    migrated and checked.
  - The check scans `sqlite_master` for retired words.
  - Two refusals are tested: two vaults for one project, and a failed schema
    step. Both leave every file where it started.
- **No false edits.** On a copy of a real storage root, every migrated entry
  reads back with its new ref and path, no `reloaded` event is recorded, and no
  pin that was fresh turns stale.
- **Skills.** Each glossary skill is tested with a pressure scenario before it
  ships (superpowers:writing-skills), e.g. an agent asked to add a `--kb` flag
  must find the glossary and use `vault`.
- **Cross-platform.** The directory move runs on Windows CI, where renaming
  open files fails differently.

## 10. Not in this work

Bugs found while surveying. Each becomes a card:

- `card next --claim` records no `claimed` event.
- `unverified` is hard-coded to 0 in vector and hybrid search.
- `ListKnowledge` does not exclude global rows, so the project and global lists
  can overlap.
- Hints name things that do not exist: `knowledge ls --global`, "escalate from
  the browser", demote as a way to delete.
- A demoted entry's old nominations return to the queue.
- The Overview "Pinned" list is really "has a recap".
- The lamp announces "Blocked" for urgent cards.
- `in_progress` counts every card that is not done, backlog included.
- A seeded `blocked` label duplicates `blocked_by` links.
- `vault health` omits unverified entries, duplicates and missing files.
- `scripts/ui-browser-smoke.sh` waits for text the UI no longer renders.
