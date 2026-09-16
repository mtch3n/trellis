# Event Feed and Repository Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **WHERE TO WORK — read before anything else.** Every path in this plan is
> relative to the worktree **`/home/mtchen/Personal/trellis-worktrees/knowledge-artifacts`**
> on branch `feat/knowledge-artifacts`. Run every command from there, e.g.
> `cd /home/mtchen/Personal/trellis-worktrees/knowledge-artifacts && go test ./...`.
> **Never** run a command in, or edit a file under, `/home/mtchen/Personal/trellis`.
> That is a shared checkout where other sessions hold uncommitted work.

**Goal:** Give an extension a reliable, content-safe way to read what changed
in Trellis (`Core.EventFeed`, a durable named cursor, and a `trellis events`
CLI), and let a repository declare its own settings in a committed
`.trellis.yaml`/`.trellis.yml` that core reads and passes through, uninterpreted,
for extensions.

**Architecture:** Part 1 adds one core query, `EventFeed`, over the existing
append-only `event` table. It never forwards a body, summary or edited value —
only a card's column move carries `old`/`new` — and it resolves each event's
`ref`/`title` from the entity's *current* row via `LEFT JOIN`, so a since-deleted
entity's ref and title come back empty except for its own `deleted` event, whose
title is what the log recorded at deletion. A new `event_consumer` table gives a
named, durable cursor; reading never advances it, only `ack` does, and `ack`
never moves it backwards. Part 2 adds a `.trellis.yaml` reader to
`internal/config` that validates every key against a repository-safe allowlist
declared in one function, decodes allowed keys into the existing `Config`
struct (so `GetValue` keeps working unchanged), and passes `extensions` through
as opaque YAML. The CLI's project-resolution helpers (`currentProject`,
`currentBoard`) learn the directory that resolved the project and layer that
repository's config between the global file and a project's database override.

**Tech Stack:** Go 1.27, SQLite via `sqlx` (modernc driver), goose migrations,
`encoding/json/v2`, `gopkg.in/yaml.v3` (`yaml.Node` for round-tripping),
cobra CLI.

**Spec:** `docs/superpowers/specs/2026-09-16-event-feed-and-repo-config-design.md`

## Global Constraints

- The feed never copies content. `old`/`new` in `FeedEvent` are populated only
  when `Kind == "card" && Action == "moved"`; every other event's `old`/`new`
  are empty in the feed regardless of what the underlying `event` row holds
  (a card's `title` edit, for example, records real old/new text in the row,
  and the feed must still blank it).
- **Ruling — deleted-entity project scoping (accepted limitation, not
  engineered around):** `EventFeed`'s `ProjectID` filter is implemented by
  `LEFT JOIN` to each kind's *current* table (`card`, `knowledge`, `board`,
  `label`, and `note`→`card`). This is exact for every live entity. The
  `event` table carries no `project_id` of its own (verified: it was never
  added by any migration through `0012_private.sql`), and `card`, `knowledge`,
  `board` and `label` deletes are hard deletes (`DELETE FROM <table> WHERE id
  = ?`, confirmed in `card.go:534`, `knowledge.go:754`, `board.go:289`,
  `label.go:105`). So once an entity is hard-deleted, its entire event
  history — including its own `deleted` event — can only be attributed to a
  project by re-deriving it from a row that no longer exists, which is
  impossible. Such events remain visible under `--all-projects` but drop out
  of a project-scoped `trellis events`/`EventQuery{ProjectID: p.ID}` read.
  This mirrors the existing, unremarked behavior of
  `internal/ui/server.go`'s `handleBoardEvents`, which also scopes via `EXISTS`
  against current rows. Adding a `project_id` column to `event` and a
  projectID parameter to `recordEvent` (touching ~30 call sites across 15
  files, several of which — `recall.go`'s `injected` event on a `global`
  knowledge doc — do not have an unambiguous project to attribute at that call
  site) was considered and rejected as scope the approved spec does not ask
  for.
- **Ruling — `DocTypes` filter scope:** `EventQuery.DocTypes`, when non-empty,
  keeps only `knowledge` events whose *current* `doc_type` is in the list, and
  excludes every non-`knowledge` event. `k.doc_type` is `NULL` for a joined
  non-knowledge or since-deleted row, and `NULL IN (...)` is never true in
  SQLite, so this falls out of the join naturally with no special-casing.
- **Ruling — consumer `gap`:** a gap is reported only when a consumer has
  acked at least once (`cursor > 0`) and that cursor now sits behind the
  oldest retained event (`oldest > cursor + 1`). A brand-new consumer
  (`cursor == 0`) never reports a gap on its first read, even if pruning
  happened before it was created — it is not skipping anything it was ever
  tracking.
- **Ruling — repository-safe key list:** "declared next to each key" is
  implemented as one exhaustive function, `config.RepoSafe(key string) bool`,
  with every key spelled out in its `switch`. A key not listed there is
  refused by construction, including any key added later.
- **Ruling — `board.default_columns` via `--repo`:** `trellis config set
  --repo` takes one positional string value. For this one list-valued
  repository-safe key, the value is split on `,` into a YAML sequence
  (matching the comma-splitting `StringSliceVar` convention already used by
  `--label`, `--tag` and `--type` throughout `internal/cli`); every other
  repository-safe key is written as a plain scalar.
- Spec-mandated order: this plan runs **after** the revision-history and
  knowledge-templates plans on this branch. Task 8, the web endpoint, is
  written to be the *last* thing this plan does specifically because the
  spec says the other session's event-feed web handler (uncommitted,
  different branch) must land first and this plan's commit is what replaces
  its query with `EventFeed` — see Task 8 for the exact verification and
  fallback if that handler is not present yet when this plan executes.
- Migration numbering: the next unused file in `internal/store/migrations/` is
  `0013_*.sql` (highest existing is `0012_private.sql`). **The parallel
  revision-history plan also adds a migration on this branch.** Whichever
  plan's commit lands second must renumber its migration file so the two do
  not collide.
- Shared-file discipline: `internal/config/config.go`, `internal/cli/config.go`
  and `internal/cli/maintenance.go` are also touched by the parallel
  revision-history and knowledge-templates plans running on this branch. Every
  edit to those three files in this plan is additive (new functions, new
  fields, new parameters threaded through) — no existing function is deleted
  or restructured beyond adding parameters it did not have.
- `internal/resolve.Identity.RootPath` already carries the directory that
  resolved the project for `Kind` `"pin"`, `"remote"` and `"path"` (verified in
  `internal/resolve/resolve.go`: `RootPath: root` for a pin,
  `RootPath: info.TopLevel` otherwise), and is the empty string for
  `Kind == "env"` (`Identity{Kind: "env", ...}` never sets `RootPath`). No
  change to `internal/resolve` is needed. Note that `internal/cli`'s
  `projectKey()` (`root.go:253`, `cmp.Or(projectFlagKey,
  os.Getenv("TRELLIS_PROJECT"))`) already short-circuits *before*
  `resolve.Identify` is ever called whenever `--project` or `TRELLIS_PROJECT`
  is set, so `Identity{Kind: "env", ...}` is presently unreachable from the
  CLI; a repository directory is simply never computed in that branch, which
  already satisfies "reads no `.trellis.yaml`."
- Gates before any task is done: `go build ./...`, `go test ./...`,
  `go vet ./...`, `gofmt -l .` printing nothing, `GOOS=windows go build ./...`.
- CI runs Linux, macOS and Windows; all three must pass.
- Stage by explicit path only. Before every commit run
  `git diff --cached --name-only` and confirm each path is one this task
  changed.
- Every commit message ends with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/event_feed.go` | Create: `EventQuery`, `FeedEvent`, `EventFeed`, `feedRow` |
| `internal/core/event_feed_test.go` | Create: every `EventFeed` behavior |
| `internal/core/event_consumer.go` | Create: `EventConsumer`, `ConsumerStatus`, `EnsureEventConsumer`, `AckEventConsumer`, `ListEventConsumers`, `DeleteEventConsumer`, `EventGapAfter`, `gapExists` |
| `internal/core/event_consumer_test.go` | Create: consumer behavior |
| `internal/store/migrations/0013_event_consumer.sql` | Create: `event_consumer` table |
| `internal/cli/events.go` | Create: `trellis events`, `ack`, `consumers`, `consumers rm`, `--follow` |
| `internal/cli/events_cmd_test.go` | Create: CLI behavior |
| `internal/cli/root.go` | Modify: register `newEventsCmd()`; `currentBoard` learns the repo directory and re-applies layered settings; `appCtx` gains `cfg`; `configInt` stops reloading config |
| `internal/config/config.go` | Modify: `RepoSafe`, `RepoConfigPath`, `RepoDoc`, `LoadRepo`, `setConfigField`, `AllKeys`, `LoadWithPresence`, `EffectiveValue` (new signature), `SetRepoValue`, `UnsetRepoValue`, YAML node helpers, `writeFileAtomic` |
| `internal/config/config_test.go` | Modify: every new function above |
| `internal/cli/config.go` | Modify: `projectContext` gains `repo`/`present`; `currentProject` computes the repo directory and loads it; `get`/`set`/`ls` use the new `EffectiveValue`; new `set --repo`/`unset --repo` flag handling; `newExtensionCmd` |
| `internal/cli/config_cmd_test.go` | Create: CLI behavior for repo config |
| `internal/cli/search.go` | Modify: one `config.Load()` call replaced with the already-resolved `app.cfg` |
| `internal/cli/vector.go` | Modify: `effectiveVectorConfig`'s `EffectiveValue` call updated for the new signature (no behavior change: no `search.vector.*` key is repository-safe) |
| `internal/ui/server.go` | Modify: add `GET /api/p/{key}/events`, or replace the other session's handler's query, calling `EventFeed` |
| `internal/ui/events_test.go` | Create: web endpoint shape |

---

### Task 1: `Core.EventFeed` — the read path, content-safe by construction

**Files:**
- Create: `internal/core/event_feed.go`
- Create: `internal/core/event_feed_test.go`

**Interfaces:**
- Consumes: `Core.Tx`, `inClause` (`internal/core/recall.go:47`), `itoa` (`internal/core/ids.go:41`), `GlobalKey` (`internal/core/knowledge.go:24`), `seededProject`/`seededProject2`/`seededBoard`/`testCore`/`kbCore` (existing test helpers), `CreateCard`, `MoveCard`, `EditCard`, `DeleteCard`, `CreateLabel`, `DeleteLabel`, `RenameBoard`, `DeleteBoard`, `CreateKnowledge`, `EditKnowledgeFields`, `DeleteKnowledge`, `CreateNote`.
- Produces:
  - `type EventQuery struct { ProjectID string; After int64; Limit int; Kinds, Actions, DocTypes []string; NotActor string }`
  - `type FeedEvent struct { Seq int64; TS int64; Actor string; Kind string; Ref string; Title string; Type string; Action string; Field string; Old string; New string }` with the exact JSON tags in the spec.
  - `func (c *Core) EventFeed(ctx context.Context, q EventQuery) (events []FeedEvent, next *int64, err error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/core/event_feed_test.go`:

```go
package core

import (
	"testing"
)

func TestEventFeedOrdersBySeqAndPages(t *testing.T) {
	// kbCore's project and board setup already recorded a "board created"
	// event before either card exists, so every query here is filtered to
	// Kinds: []string{"card"} — otherwise the very first page would return
	// that board event, not "one".
	c, p, b := kbCore(t)
	card1, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "one"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	card2, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "two"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	first, next, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"card"}, Limit: 1})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(first) != 1 || first[0].Title != "one" || next == nil {
		t.Fatalf("first page = %+v, next = %v", first, next)
	}

	second, next2, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"card"}, After: *next, Limit: 1})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(second) != 1 || second[0].Title != "two" || next2 == nil || *next2 <= *next {
		t.Fatalf("second page = %+v, next = %v (first next %v)", second, next2, next)
	}
	_ = card1
	_ = card2

	empty, emptyNext, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"card"}, After: *next2})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(empty) != 0 || emptyNext != nil {
		t.Errorf("empty page = %+v, next = %v, want nil next", empty, emptyNext)
	}
}

func TestEventFeedDefaultAndMaxLimit(t *testing.T) {
	c, p, b := kbCore(t)
	for i := 0; i < 3; i++ {
		if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"}); err != nil {
			t.Fatalf("CreateCard: %v", err)
		}
	}
	// Kinds: []string{"card"} excludes kbCore's own "board created" event, so
	// the count below is exactly the three writes this test made.
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"card"}, Limit: 50000})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("a limit above 5000 must still be capped sanely; got %d events for 3 writes", len(events))
	}
}

func TestEventFeedFiltersByKind(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "card"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateLabel(t.Context(), p.ID, "urgent", "needs attention"); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"label"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "label" || events[0].Ref != "urgent" {
		t.Fatalf("events = %+v, want exactly one label event named urgent", events)
	}
}

func TestEventFeedExcludesReadByDefaultButNotWhenAsked(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.ReadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("ReadKnowledge: %v", err)
	}

	byDefault, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	for _, ev := range byDefault {
		if ev.Action == "read" {
			t.Errorf("a read event appeared without being asked for: %+v", ev)
		}
	}

	withReads, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Actions: []string{"read"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(withReads) != 1 || withReads[0].Action != "read" {
		t.Fatalf("events = %+v, want exactly the one read event", withReads)
	}
}

func TestEventFeedFiltersByDocType(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "unrelated"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Bug", Template: "finding"}); err != nil {
		t.Fatalf("CreateKnowledge finding: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Template: "note"}); err != nil {
		t.Fatalf("CreateKnowledge note: %v", err)
	}

	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, DocTypes: []string{"finding"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "knowledge" || events[0].Type != "finding" || events[0].Title != "Bug" {
		t.Fatalf("events = %+v, want exactly the one finding, and no card event", events)
	}
}

func TestEventFeedNotActorSkipsItsOwnWrites(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "mine"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, NotActor: c.actor})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events = %+v, want none: every write in this test was made by NotActor", events)
	}
}

func TestEventFeedScopesByProject(t *testing.T) {
	c, p, b := kbCore(t)
	p2 := seededProject2(t, c)
	b2 := seededBoard(t, c, p2)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "in p"}); err != nil {
		t.Fatalf("CreateCard p: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p2.ID, b2.ID, NewCard{Title: "in p2"}); err != nil {
		t.Fatalf("CreateCard p2: %v", err)
	}

	// Filtered to Kinds: []string{"card"} throughout: kbCore and
	// seededBoard each already record a "board created" event for their own
	// project, and this test is about project scoping, not board noise.
	scoped, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"card"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Title != "in p" {
		t.Fatalf("scoped events = %+v, want only p's card", scoped)
	}

	all, _, err := c.EventFeed(t.Context(), EventQuery{Kinds: []string{"card"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("--all-projects (ProjectID \"\") card events = %+v, want both", all)
	}
}

func TestEventFeedNeverReturnsEditedContent(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Old Title"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	newTitle := "New Title"
	version := card.Version
	if _, err := c.EditCard(t.Context(), p.ID, ParseCardRef(card.ID), CardEdit{Title: &newTitle, IfVersion: &version}); err != nil {
		t.Fatalf("EditCard: %v", err)
	}

	// The raw row really does hold both titles: prove the feed's blanking is
	// its own policy, not a coincidence of what got written.
	var rawOld, rawNew string
	if err := c.db.Get(&rawOld, `SELECT old_value FROM event WHERE entity_id = ? AND action = 'edited' AND field = 'title'`, card.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.db.Get(&rawNew, `SELECT new_value FROM event WHERE entity_id = ? AND action = 'edited' AND field = 'title'`, card.ID); err != nil {
		t.Fatal(err)
	}
	if rawOld != "Old Title" || rawNew != "New Title" {
		t.Fatalf("test setup: raw event old/new = %q/%q, want the real titles", rawOld, rawNew)
	}

	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Actions: []string{"edited"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 1 || events[0].Old != "" || events[0].New != "" {
		t.Fatalf("events = %+v, want old/new blanked even though the row holds real text", events)
	}
}

func TestEventFeedCardMovedCarriesColumnNames(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "moves"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	cols, err := c.ListColumns(t.Context(), b.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) < 2 {
		t.Fatalf("test needs at least two columns, got %d", len(cols))
	}
	if _, err := c.MoveCard(t.Context(), p.ID, b.ID, ParseCardRef(card.ID), cols[1].Name); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}

	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Actions: []string{"moved"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 1 || events[0].Old != cols[0].Name || events[0].New != cols[1].Name {
		t.Fatalf("events = %+v, want old=%s new=%s", events, cols[0].Name, cols[1].Name)
	}
}

func TestEventFeedDeletedCardHasEmptyRefAndTitleExceptItsOwnDeletedEvent(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Gone"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := c.DeleteCard(t.Context(), p.ID, ParseCardRef(card.ID)); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}

	// Kinds: []string{"card"} excludes kbCore's own "board created" event,
	// whose ref is still the (undeleted) board's name and would otherwise
	// trip the loop below, which assumes every returned event is this card's.
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"card"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want created + deleted", events)
	}
	for _, ev := range events {
		if ev.Ref != "" {
			t.Errorf("%+v: ref must be empty for a deleted entity's event", ev)
		}
		if ev.Action == "created" && ev.Title != "" {
			t.Errorf("%+v: an earlier event's title must be empty once the entity is gone", ev)
		}
		if ev.Action == "deleted" && ev.Title != "Gone" {
			t.Errorf("%+v: the deleted event must carry the title the log recorded at deletion", ev)
		}
	}
}

func TestEventFeedDeletedKnowledgeHasEmptyRefAndTitleExceptItsOwnDeletedEvent(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Temporary"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := c.DeleteKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("DeleteKnowledge: %v", err)
	}

	// Kinds: []string{"knowledge"} excludes kbCore's own "board created"
	// event, whose ref and title are still the (undeleted) board's — the
	// loop below assumes every returned event is this entry's.
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"knowledge"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	var sawDeleted bool
	for _, ev := range events {
		if ev.Ref != "" {
			t.Errorf("%+v: ref must be empty once the entry is gone", ev)
		}
		if ev.Action == "deleted" {
			sawDeleted = true
			if ev.Title != "Temporary" {
				t.Errorf("%+v: deleted event must carry the recorded title", ev)
			}
		} else if ev.Title != "" {
			t.Errorf("%+v: a non-deleted event's title must be empty once the entry is gone", ev)
		}
	}
	if !sawDeleted {
		t.Fatal("no deleted event found")
	}
}

func TestEventFeedPrivateEntryCarriesRefAndTitleOnly(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Prod credentials", Private: true})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	body := "the actual secret body"
	if _, err := c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Body: &body}); err != nil {
		t.Fatalf("EditKnowledgeFields: %v", err)
	}

	// Kinds: []string{"knowledge"} excludes kbCore's own "board created"
	// event, whose title is the board's name, not this entry's.
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"knowledge"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("events = %+v, want created + edited", events)
	}
	for _, ev := range events {
		if ev.Title != "Prod credentials" {
			t.Errorf("%+v: title must still travel for a private entry", ev)
		}
		if ev.Ref == "" {
			t.Errorf("%+v: ref must still travel for a private entry", ev)
		}
		if ev.Old != "" || ev.New != "" {
			t.Errorf("%+v: a private entry's old/new must never carry its body", ev)
		}
	}
}

func TestEventFeedNoteRefIsItsCardsRef(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Has a note"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateNote(t.Context(), card.ID, "handed off"); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"note"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 1 || events[0].Ref != card.Ref || events[0].Title != "Has a note" {
		t.Fatalf("note event = %+v, want ref=%s title=%s", events[0], card.Ref, "Has a note")
	}
}

func TestEventFeedKnowledgeRefUsesGlobalForAnEscalatedDoc(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Widely useful"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "applies everywhere"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}

	// Kinds: []string{"knowledge"} excludes kbCore's own "board created"
	// event, which also has action "created".
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"knowledge"}, Actions: []string{"created"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) != 1 || events[0].Ref != GlobalKey+"/"+doc.Slug {
		t.Fatalf("events = %+v, want ref %s/%s", events, GlobalKey, doc.Slug)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestEventFeed' -v`
Expected: the package does not compile — `EventQuery`, `FeedEvent` and `EventFeed` are undefined.

- [ ] **Step 3: Write `internal/core/event_feed.go`**

```go
package core

import "context"

// EventQuery filters a read of the event feed. The zero value reads every
// project's events from the beginning, all kinds, every action but "read".
type EventQuery struct {
	ProjectID string   // "" = every project
	After     int64    // exclusive
	Limit     int      // default 1000, max 5000
	Kinds     []string // card | knowledge | board | label | note; empty = all
	Actions   []string // created, edited, moved, ...; empty = all but read
	DocTypes  []string // knowledge only: finding, decision, ...
	NotActor  string   // skip events written by this actor
}

// FeedEvent is one entry an extension, the CLI or the web timeline can react
// to. Content never ships: old and new are populated only for a card's
// column move, and a deleted entity's ref and title are empty except for its
// own deleted event, whose title is what was recorded at deletion.
type FeedEvent struct {
	Seq    int64  `json:"seq"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Type   string `json:"type,omitempty"`
	Action string `json:"action"`
	Field  string `json:"field,omitempty"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new,omitempty"`
}

// feedRow is what the join returns, before the disclosure policy in
// toFeedEvent decides what of it may leave. Every joined column is
// COALESCE'd to its type's zero value, so "" (or 0 for a seq) means the
// corresponding entity is not the one this event is about, or no longer
// exists.
type feedRow struct {
	Seq       int64  `db:"seq"`
	TS        int64  `db:"ts"`
	Actor     string `db:"actor"`
	Kind      string `db:"kind"`
	Action    string `db:"action"`
	Field     string `db:"field"`
	OldValue  string `db:"old_value"`
	NewValue  string `db:"new_value"`
	CardKey   string `db:"card_key"`
	CardSeq   int64  `db:"card_seq"`
	CardTitle string `db:"card_title"`
	KBKey     string `db:"kb_key"`
	KBSlug    string `db:"kb_slug"`
	KBTitle   string `db:"kb_title"`
	KBDocType string `db:"kb_doctype"`
	BoardName string `db:"board_name"`
	LabelName string `db:"label_name"`
	NoteKey   string `db:"note_key"`
	NoteSeq   int64  `db:"note_seq"`
	NoteTitle string `db:"note_title"`
}

// toFeedEvent applies the feed's disclosure policy. It is the only place that
// decides what leaves: old/new travel only for a card's column move, and ref
// and title come from the entity's current row, empty when it no longer
// exists, except that a deleted event's own title is what old_value recorded.
func (r feedRow) toFeedEvent() FeedEvent {
	ev := FeedEvent{
		Seq: r.Seq, TS: r.TS, Actor: r.Actor, Kind: r.Kind, Action: r.Action, Field: r.Field,
	}
	if r.Kind == "card" && r.Action == "moved" {
		ev.Old, ev.New = r.OldValue, r.NewValue
	}
	switch r.Kind {
	case "card":
		if r.CardKey != "" {
			ev.Ref = r.CardKey + "-" + itoa(r.CardSeq)
			ev.Title = r.CardTitle
		}
	case "knowledge":
		if r.KBKey != "" {
			ev.Ref = r.KBKey + "/" + r.KBSlug
			ev.Title = r.KBTitle
			ev.Type = r.KBDocType
		}
	case "board":
		if r.BoardName != "" {
			ev.Ref = r.BoardName
			ev.Title = r.BoardName
		}
	case "label":
		if r.LabelName != "" {
			ev.Ref = r.LabelName
			ev.Title = r.LabelName
		}
	case "note":
		if r.NoteKey != "" {
			ev.Ref = r.NoteKey + "-" + itoa(r.NoteSeq)
			ev.Title = r.NoteTitle
		}
	}
	if r.Action == "deleted" {
		ev.Ref = ""
		ev.Title = r.OldValue
	}
	return ev
}

// EventFeed is the one query behind the CLI, the web endpoint and any future
// extension. See toFeedEvent for the disclosure policy and the Global
// Constraints in the plan for why ProjectID scoping cannot reach a
// hard-deleted entity's history.
func (c *Core) EventFeed(ctx context.Context, q EventQuery) ([]FeedEvent, *int64, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}

	kindClause, kindArgs := inClause("e.entity_type", q.Kinds)
	var actionClause string
	var actionArgs []any
	if len(q.Actions) == 0 {
		actionClause = " AND e.action != 'read'"
	} else {
		actionClause, actionArgs = inClause("e.action", q.Actions)
	}
	docTypeClause, docTypeArgs := inClause("k.doc_type", q.DocTypes)
	var actorClause string
	var actorArgs []any
	if q.NotActor != "" {
		actorClause = " AND e.actor != ?"
		actorArgs = []any{q.NotActor}
	}
	var projectClause string
	var projectArgs []any
	if q.ProjectID != "" {
		projectClause = " AND COALESCE(c.project_id, k.project_id, b.project_id, l.project_id, nc.project_id) = ?"
		projectArgs = []any{q.ProjectID}
	}

	args := []any{q.After}
	args = append(args, kindArgs...)
	args = append(args, actionArgs...)
	args = append(args, docTypeArgs...)
	args = append(args, actorArgs...)
	args = append(args, projectArgs...)
	args = append(args, limit)

	query := `
		SELECT
		    e.seq, e.ts, e.actor, e.entity_type AS kind, e.action,
		    COALESCE(e.field, '') AS field,
		    COALESCE(e.old_value, '') AS old_value,
		    COALESCE(e.new_value, '') AS new_value,
		    COALESCE(pc.key, '') AS card_key,
		    COALESCE(c.seq, 0) AS card_seq,
		    COALESCE(c.title, '') AS card_title,
		    COALESCE(CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE pk.key END, '') AS kb_key,
		    COALESCE(k.slug, '') AS kb_slug,
		    COALESCE(k.title, '') AS kb_title,
		    COALESCE(k.doc_type, '') AS kb_doctype,
		    COALESCE(b.name, '') AS board_name,
		    COALESCE(l.name, '') AS label_name,
		    COALESCE(pn.key, '') AS note_key,
		    COALESCE(nc.seq, 0) AS note_seq,
		    COALESCE(nc.title, '') AS note_title
		FROM event e
		LEFT JOIN card c ON c.id = e.entity_id AND e.entity_type = 'card'
		LEFT JOIN project pc ON pc.id = c.project_id
		LEFT JOIN knowledge k ON k.id = e.entity_id AND e.entity_type = 'knowledge'
		LEFT JOIN project pk ON pk.id = k.project_id
		LEFT JOIN board b ON b.id = e.entity_id AND e.entity_type = 'board'
		LEFT JOIN label l ON l.id = e.entity_id AND e.entity_type = 'label'
		LEFT JOIN note n ON n.id = e.entity_id AND e.entity_type = 'note'
		LEFT JOIN card nc ON nc.id = n.card_id
		LEFT JOIN project pn ON pn.id = nc.project_id
		WHERE e.seq > ?
		  AND e.entity_type IN ('card', 'knowledge', 'board', 'label', 'note')` +
		kindClause + actionClause + docTypeClause + actorClause + projectClause + `
		ORDER BY e.seq ASC
		LIMIT ?`

	var rows []feedRow
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&rows, query, args...)
	}); err != nil {
		return nil, nil, err
	}

	events := make([]FeedEvent, len(rows))
	for i, r := range rows {
		events[i] = r.toFeedEvent()
	}
	if len(events) == 0 {
		return events, nil, nil
	}
	next := events[len(events)-1].Seq
	return events, &next, nil
}
```

Add `"github.com/jmoiron/sqlx"` to the import block (needed for the `*sqlx.Tx` parameter type in the closure).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestEventFeed' -v`
Expected: PASS, all fourteen.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass; `gofmt -l .` prints nothing.

**Known flake:** `TestListKnowledgeFiltersByTypeAndProvenance/both_dimensions` fails about one run in five (TRELLIS-26, pre-existing — `KnowledgeFilter.where()` ranges over a Go map). If that exact test fails, say so and move on. Investigate any other failure; never attribute it to this flake.

- [ ] **Step 6: Commit**

```bash
git add internal/core/event_feed.go internal/core/event_feed_test.go
git commit -m "$(cat <<'EOF'
feat(core): add EventFeed, the content-safe read path over the event log

One query serves the CLI, the web endpoint and any future extension. old and
new are populated only for a card's column move; every other event's old/new
are blanked in the feed even though the underlying row can hold real text. A
deleted entity's events keep an empty ref and title, except its own deleted
event, whose title is what the log recorded at deletion.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Named consumers — durable cursors, ack, and gap reporting

**Files:**
- Create: `internal/store/migrations/0013_event_consumer.sql`
- Create: `internal/core/event_consumer.go`
- Create: `internal/core/event_consumer_test.go`

**Interfaces:**
- Consumes: Task 1's `EventFeed`, `Core.Tx`, `c.clock.NowMS()`, `PruneHistory` (`internal/core/maintenance.go:10`).
- Produces:
  - `type EventConsumer struct { Name string; Cursor int64; CreatedAt int64; UpdatedAt int64 }`
  - `type ConsumerStatus struct { Name string; Cursor int64; Lag int64; Gap bool }`
  - `func (c *Core) EnsureEventConsumer(ctx context.Context, name string) (EventConsumer, error)`
  - `func (c *Core) AckEventConsumer(ctx context.Context, name string, seq int64) (EventConsumer, error)`
  - `func (c *Core) ListEventConsumers(ctx context.Context) ([]ConsumerStatus, error)`
  - `func (c *Core) DeleteEventConsumer(ctx context.Context, name string) error`
  - `func (c *Core) EventGapAfter(ctx context.Context, after int64) (gap bool, oldest int64, err error)`

- [ ] **Step 1: Write the migration**

Create `internal/store/migrations/0013_event_consumer.sql`:

```sql
-- +goose Up
-- A consumer is a named, durable cursor into the event feed (§ event feed
-- design). Reading never advances cursor; only `ack` does, and ack never
-- moves it backwards. This gives an extension at-least-once delivery: a
-- crash between handling an event and acking it repeats the event next time.
CREATE TABLE event_consumer (
    name       TEXT PRIMARY KEY,
    cursor     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE event_consumer;
```

- [ ] **Step 2: Write the failing tests**

Create `internal/core/event_consumer_test.go`:

```go
package core

import "testing"

func TestEnsureEventConsumerCreatesOnFirstUse(t *testing.T) {
	c := testCore(t)
	ec, err := c.EnsureEventConsumer(t.Context(), "reviewer")
	if err != nil {
		t.Fatalf("EnsureEventConsumer: %v", err)
	}
	if ec.Name != "reviewer" || ec.Cursor != 0 {
		t.Fatalf("consumer = %+v, want cursor 0", ec)
	}

	again, err := c.EnsureEventConsumer(t.Context(), "reviewer")
	if err != nil {
		t.Fatalf("EnsureEventConsumer again: %v", err)
	}
	if again.CreatedAt != ec.CreatedAt {
		t.Errorf("second EnsureEventConsumer created a new row: %+v vs %+v", again, ec)
	}
}

func TestAckAdvancesTheCursor(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events to ack")
	}
	if _, err := c.EnsureEventConsumer(t.Context(), "worker"); err != nil {
		t.Fatalf("EnsureEventConsumer: %v", err)
	}

	ec, err := c.AckEventConsumer(t.Context(), "worker", events[len(events)-1].Seq)
	if err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}
	if ec.Cursor != events[len(events)-1].Seq {
		t.Errorf("cursor = %d, want %d", ec.Cursor, events[len(events)-1].Seq)
	}
}

func TestAckNeverMovesBackwards(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	last := events[len(events)-1].Seq
	first := events[0].Seq

	if _, err := c.AckEventConsumer(t.Context(), "worker", last); err != nil {
		t.Fatalf("first ack: %v", err)
	}
	ec, err := c.AckEventConsumer(t.Context(), "worker", first)
	if err != nil {
		t.Fatalf("second (older) ack: %v", err)
	}
	if ec.Cursor != last {
		t.Errorf("cursor = %d after acking an older seq, want it to stay at %d", ec.Cursor, last)
	}
}

func TestAckRefusesASeqPastTheNewest(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	newest := events[len(events)-1].Seq

	_, err = c.AckEventConsumer(t.Context(), "worker", newest+1000)
	if code := consumerErrCode(err); code != "seq_too_new" {
		t.Fatalf("err = %v, want seq_too_new", err)
	}
}

// A single FixedClock timestamps every write identically, so PruneHistory's
// timestamp cutoff cannot express "prune some but not all" within one Core.
// This test opens a second Core on the same database, one tick later, the
// same technique internal/core/lease_test.go:474-478 already uses to test
// time-dependent behavior against a shared connection.
func TestEventGapAfterPartialPruning(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if _, err := c.AckEventConsumer(t.Context(), "worker", events[0].Seq); err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}

	baseMS := c.clock.NowMS()
	later := New(c.db, FixedClock{MS: baseMS + 1000}, c.actor)
	if _, err := later.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "later"}); err != nil {
		t.Fatalf("CreateCard (later): %v", err)
	}

	if _, err := c.PruneHistory(t.Context(), baseMS+500, true, false); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	gap, oldest, err := c.EventGapAfter(t.Context(), events[0].Seq)
	if err != nil {
		t.Fatalf("EventGapAfter: %v", err)
	}
	if !gap {
		t.Fatal("want a gap: pruning removed events past the consumer's cursor")
	}
	if oldest <= events[0].Seq {
		t.Errorf("oldest = %d, want it greater than the acked cursor %d", oldest, events[0].Seq)
	}
}

// If pruning removes every event, MIN(seq) has nothing to report at all --
// COALESCE would otherwise default it to 0 and the ordinary oldest > after+1
// comparison would wrongly say there is no gap, when in fact everything,
// including whatever the consumer had not yet reached, is gone.
func TestEventGapWhenEveryEventIsPruned(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if _, err := c.AckEventConsumer(t.Context(), "worker", events[0].Seq); err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}

	if _, err := c.PruneHistory(t.Context(), c.clock.NowMS()+1, true, false); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	gap, _, err := c.EventGapAfter(t.Context(), events[0].Seq)
	if err != nil {
		t.Fatalf("EventGapAfter: %v", err)
	}
	if !gap {
		t.Error("want a gap: every event, including ones past the cursor, is gone")
	}
}

func TestEventGapIsFalseForABrandNewConsumer(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.PruneHistory(t.Context(), c.clock.NowMS()+1, true, false); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	gap, _, err := c.EventGapAfter(t.Context(), 0)
	if err != nil {
		t.Fatalf("EventGapAfter: %v", err)
	}
	if gap {
		t.Error("a brand-new consumer (cursor 0) must not report a gap: it never tracked the pruned events")
	}
}

func TestListEventConsumersReportsCursorLagAndGap(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if _, err := c.AckEventConsumer(t.Context(), "worker", events[0].Seq); err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}

	list, err := c.ListEventConsumers(t.Context())
	if err != nil {
		t.Fatalf("ListEventConsumers: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v, want one consumer", list)
	}
	s := list[0]
	if s.Name != "worker" || s.Cursor != events[0].Seq {
		t.Fatalf("status = %+v, want name=worker cursor=%d", s, events[0].Seq)
	}
	if s.Lag != events[len(events)-1].Seq-events[0].Seq {
		t.Errorf("lag = %d, want %d", s.Lag, events[len(events)-1].Seq-events[0].Seq)
	}
	if s.Gap {
		t.Error("no prune happened; gap must be false")
	}
}

func TestDeleteEventConsumer(t *testing.T) {
	c := testCore(t)
	if _, err := c.EnsureEventConsumer(t.Context(), "worker"); err != nil {
		t.Fatalf("EnsureEventConsumer: %v", err)
	}
	if err := c.DeleteEventConsumer(t.Context(), "worker"); err != nil {
		t.Fatalf("DeleteEventConsumer: %v", err)
	}
	list, err := c.ListEventConsumers(t.Context())
	if err != nil {
		t.Fatalf("ListEventConsumers: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list = %+v, want empty after delete", list)
	}
	// Deleting an unknown consumer is not an error: a caller cleaning up
	// after an extension it never ran needs no special case.
	if err := c.DeleteEventConsumer(t.Context(), "never-existed"); err != nil {
		t.Errorf("DeleteEventConsumer of an unknown name: %v, want nil", err)
	}
}
```

This test file needs a small helper to read an error's code. `internal/core`'s
existing tests each declare their own differently-named copy of this
(`artifactErrCode` in `artifact_link_test.go`, `fileErrCode` in
`artifact_file_test.go` — verified, there is no shared `errCode` to reuse), so
add one scoped to this file at the bottom of `internal/core/event_consumer_test.go`:

```go
func consumerErrCode(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}
```

and add `"errors"` to the file's imports. Change the one call site above from
`errCode(err)` to `consumerErrCode(err)`.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestEnsureEventConsumer|TestAck|TestEventGap|TestListEventConsumers|TestDeleteEventConsumer' -v`
Expected: the package does not compile — `EnsureEventConsumer` and friends are undefined.

- [ ] **Step 4: Write `internal/core/event_consumer.go`**

```go
package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// EventConsumer is a named, durable cursor into the event feed.
type EventConsumer struct {
	Name      string `db:"name" json:"name"`
	Cursor    int64  `db:"cursor" json:"cursor"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
	UpdatedAt int64  `db:"updated_at" json:"updated_at"`
}

// ConsumerStatus is what `trellis events consumers` lists: name, cursor, how
// far behind the newest event it is, and whether pruning has left a hole it
// has not yet acknowledged.
type ConsumerStatus struct {
	Name   string `json:"name"`
	Cursor int64  `json:"cursor"`
	Lag    int64  `json:"lag"`
	Gap    bool   `json:"gap"`
}

// EnsureEventConsumer returns the named consumer, creating it with cursor 0
// on first use.
func (c *Core) EnsureEventConsumer(ctx context.Context, name string) (EventConsumer, error) {
	var ec EventConsumer
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		now := c.clock.NowMS()
		if _, err := tx.Exec(
			`INSERT INTO event_consumer (name, cursor, created_at, updated_at)
			 VALUES (?, 0, ?, ?)
			 ON CONFLICT(name) DO NOTHING`, name, now, now); err != nil {
			return err
		}
		return tx.Get(&ec, `SELECT * FROM event_consumer WHERE name = ?`, name)
	})
	return ec, err
}

// AckEventConsumer records that name has handled everything up to and
// including seq. It never moves the cursor backwards, and it refuses a seq
// past the newest event: acking work that has not happened yet would let a
// later gap check believe events were handled that never were.
func (c *Core) AckEventConsumer(ctx context.Context, name string, seq int64) (EventConsumer, error) {
	var ec EventConsumer
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var newest int64
		if err := tx.Get(&newest, `SELECT COALESCE(MAX(seq), 0) FROM event`); err != nil {
			return err
		}
		if seq > newest {
			return ErrUsage("seq_too_new",
				"seq is past the newest event", "trellis events consumers")
		}
		now := c.clock.NowMS()
		if _, err := tx.Exec(
			`INSERT INTO event_consumer (name, cursor, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET
			   cursor = MAX(event_consumer.cursor, excluded.cursor),
			   updated_at = ?`,
			name, seq, now, now, now); err != nil {
			return err
		}
		return tx.Get(&ec, `SELECT * FROM event_consumer WHERE name = ?`, name)
	})
	return ec, err
}

// ListEventConsumers lists every consumer with its lag against the newest
// event and whether a prune has left a gap it has not acknowledged past.
func (c *Core) ListEventConsumers(ctx context.Context) ([]ConsumerStatus, error) {
	out := []ConsumerStatus{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var consumers []EventConsumer
		if err := tx.Select(&consumers, `SELECT * FROM event_consumer ORDER BY name`); err != nil {
			return err
		}
		var count int
		var newest, oldest int64
		if err := tx.Get(&count, `SELECT COUNT(*) FROM event`); err != nil {
			return err
		}
		if err := tx.Get(&newest, `SELECT COALESCE(MAX(seq), 0) FROM event`); err != nil {
			return err
		}
		if err := tx.Get(&oldest, `SELECT COALESCE(MIN(seq), 0) FROM event`); err != nil {
			return err
		}
		for _, ec := range consumers {
			out = append(out, ConsumerStatus{
				Name:   ec.Name,
				Cursor: ec.Cursor,
				Lag:    newest - ec.Cursor,
				Gap:    gapExists(ec.Cursor, count, oldest),
			})
		}
		return nil
	})
	return out, err
}

// DeleteEventConsumer removes a named consumer. Removing a name that does
// not exist is not an error.
func (c *Core) DeleteEventConsumer(ctx context.Context, name string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(`DELETE FROM event_consumer WHERE name = ?`, name)
		return err
	})
}

// gapExists is the one rule EventGapAfter and ListEventConsumers both apply:
// a cursor that has never acked (0) never has a gap, and otherwise there is
// one when every event is gone (count == 0) or the oldest surviving one is
// past what the cursor already saw. If prune has removed every event,
// MIN(seq) has nothing to report and COALESCE would default oldest to 0 --
// indistinguishable from "nothing has ever been pruned; the log starts at
// seq 0" -- so count is checked separately rather than folded into oldest.
func gapExists(cursor int64, count int, oldest int64) bool {
	return cursor > 0 && (count == 0 || oldest > cursor+1)
}

// EventGapAfter reports whether resuming a read from after (a consumer's
// stored cursor) would skip events that maintenance prune already removed.
func (c *Core) EventGapAfter(ctx context.Context, after int64) (gap bool, oldest int64, err error) {
	var count int
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&count, `SELECT COUNT(*) FROM event`); err != nil {
			return err
		}
		return tx.Get(&oldest, `SELECT COALESCE(MIN(seq), 0) FROM event`)
	})
	if err != nil {
		return false, 0, err
	}
	return gapExists(after, count, oldest), oldest, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestEnsureEventConsumer|TestAck|TestEventGap|TestListEventConsumers|TestDeleteEventConsumer' -v`
Expected: PASS, all nine.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/store/migrations/0013_event_consumer.sql internal/core/event_consumer.go internal/core/event_consumer_test.go
git commit -m "$(cat <<'EOF'
feat(core): named event consumers with at-least-once ack semantics

A consumer is a durable cursor an extension can stop and resume from.
Reading never advances it; ack does, never backwards, and never past the
newest event. EventGapAfter tells a consumer when maintenance prune has
removed events it had not reached yet, so pruning never waits on consumers
and never hides what it removed.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: `trellis events` — list, ack, consumers, and `--follow`

**Files:**
- Create: `internal/cli/events.go`
- Create: `internal/cli/events_cmd_test.go`
- Modify: `internal/cli/root.go` (register the command)

**Interfaces:**
- Consumes: Tasks 1–2 (`core.EventFeed`, `core.EventQuery`, `core.FeedEvent`, `core.EnsureEventConsumer`, `core.AckEventConsumer`, `core.ListEventConsumers`, `core.DeleteEventConsumer`, `core.EventGapAfter`), `openCore`, `currentProject` (`internal/cli/config.go:28`), `Emit` (`internal/cli/output.go:15`).
- Produces:
  - `func runEventsFollow(ctx context.Context, interval time.Duration, after int64, fetch func(after int64) ([]core.FeedEvent, *int64, error), emit func(core.FeedEvent) error) error`
  - `func newEventsCmd() *cobra.Command`, registered on the root command.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/events_cmd_test.go`:

```go
package cli

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/core"
)

func TestRunEventsFollowPrintsAnEventWrittenAfterItStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var seen []int64
	calls := 0
	fetch := func(after int64) ([]core.FeedEvent, *int64, error) {
		calls++
		if calls == 1 {
			// Nothing yet: this is the poll that runs before anything new
			// has been written.
			return nil, nil, nil
		}
		seq := int64(1)
		// Stop the loop once the second poll has found the new event, so
		// the test does not depend on a real clock.
		cancel()
		return []core.FeedEvent{{Seq: seq, Kind: "card", Action: "created"}}, &seq, nil
	}

	err := runEventsFollow(ctx, time.Millisecond, 0, fetch, func(ev core.FeedEvent) error {
		seen = append(seen, ev.Seq)
		return nil
	})
	if err != nil {
		t.Fatalf("runEventsFollow: %v", err)
	}
	if len(seen) != 1 || seen[0] != 1 {
		t.Fatalf("seen = %v, want [1]", seen)
	}
}

func TestRunEventsFollowStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	err := runEventsFollow(ctx, time.Millisecond, 0, func(int64) ([]core.FeedEvent, *int64, error) {
		calls++
		return nil, nil, nil
	}, func(core.FeedEvent) error { return nil })
	if err != nil {
		t.Fatalf("runEventsFollow: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want exactly one poll before the cancelled context stops the loop", calls)
	}
}

func TestEventsListsCreatedCards(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "First")

	out := runCmd(t, "events")
	if !strings.Contains(out, `"action":"created"`) || !strings.Contains(out, `"kind":"card"`) {
		t.Fatalf("events output missing the card creation:\n%s", out)
	}
}

func TestEventsAfterExcludesEarlierEvents(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "First")
	first := strings.Split(strings.TrimSpace(runCmd(t, "events")), "\n")
	var firstEvent struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal([]byte(first[len(first)-1]), &firstEvent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	runCmd(t, "card", "new", "--title", "Second")
	out := runCmd(t, "events", "--after", itoaTest(firstEvent.Seq))
	if strings.Contains(out, `"First"`) {
		t.Errorf("--after did not exclude the earlier event:\n%s", out)
	}
	if !strings.Contains(out, `"Second"`) {
		t.Errorf("--after excluded the later event too:\n%s", out)
	}
}

func itoaTest(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestEventsKindFilter(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "Card")
	runCmd(t, "label", "new", "urgent", "--description", "needs attention")

	out := runCmd(t, "events", "--kind", "label")
	if strings.Contains(out, `"kind":"card"`) {
		t.Errorf("--kind label still printed a card event:\n%s", out)
	}
	if !strings.Contains(out, `"kind":"label"`) {
		t.Errorf("--kind label printed no label event:\n%s", out)
	}
}

func TestEventsConsumerResumesAfterAck(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "A")

	first := runCmd(t, "events", "--consumer", "worker")
	lines := strings.Split(strings.TrimSpace(first), "\n")
	var lastSeq struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lastSeq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Reading again without acking must return the same events: the cursor
	// has not moved.
	again := runCmd(t, "events", "--consumer", "worker")
	if strings.TrimSpace(again) != strings.TrimSpace(first) {
		t.Fatalf("a second read before ack must repeat the same events:\nfirst=%q\nagain=%q", first, again)
	}

	runCmd(t, "events", "ack", "worker", itoaTest(lastSeq.Seq))

	runCmd(t, "card", "new", "--title", "B")
	afterAck := runCmd(t, "events", "--consumer", "worker")
	if strings.Contains(afterAck, `"A"`) {
		t.Errorf("after ack, the consumer must not see the already-handled event again:\n%s", afterAck)
	}
	if !strings.Contains(afterAck, `"B"`) {
		t.Errorf("after ack, the consumer must see the new event:\n%s", afterAck)
	}
}

func TestEventsConsumersListsAndRemoves(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "A")
	runCmd(t, "events", "--consumer", "worker")

	out := runCmd(t, "events", "consumers", "--json")
	if !strings.Contains(out, `"name":"worker"`) {
		t.Fatalf("consumers listing missing worker:\n%s", out)
	}

	runCmd(t, "events", "consumers", "rm", "worker")
	after := runCmd(t, "events", "consumers", "--json")
	if strings.Contains(after, "worker") {
		t.Errorf("consumer still listed after rm:\n%s", after)
	}
}

func TestEventsAckRejectsANonIntegerSeq(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "events", "ack", "worker", "not-a-number"); cliErrCode(err) != "invalid_seq" {
		t.Errorf("err = %v, want invalid_seq", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestRunEventsFollow|TestEvents' -v`
Expected: the package does not compile — `runEventsFollow` and `newEventsCmd` are undefined, and `trellis events` is an unknown command.

- [ ] **Step 3: Write `internal/cli/events.go`**

```go
package cli

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newEventsCmd() *cobra.Command {
	var after int64
	var limit int
	var kinds, actions, docTypes []string
	var notActor, consumer string
	var allProjects, follow bool

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Read the event feed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEventsList(cmd, after, limit, kinds, actions, docTypes, notActor, consumer, allProjects, follow)
		},
	}
	cmd.Flags().Int64Var(&after, "after", 0, "only events after this seq")
	cmd.Flags().IntVar(&limit, "limit", 0, "row cap (default 1000, max 5000)")
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "card|knowledge|board|label|note (repeatable)")
	cmd.Flags().StringSliceVar(&actions, "action", nil, "created, edited, moved, ... (repeatable; default: everything but read)")
	cmd.Flags().StringSliceVar(&docTypes, "type", nil, "knowledge doc types (repeatable)")
	cmd.Flags().StringVar(&notActor, "not-actor", "", "skip events written by this actor")
	cmd.Flags().StringVar(&consumer, "consumer", "", "resume after this named consumer's cursor; creates it on first use")
	cmd.Flags().BoolVar(&allProjects, "all-projects", false, "every project, not just this one")
	cmd.Flags().BoolVar(&follow, "follow", false, "keep polling for new events, once a second")
	cmd.AddCommand(newEventsAckCmd(), newEventsConsumersCmd())
	return cmd
}

// runEventsList resolves the project (unless --all-projects), starts from
// --consumer's cursor when one is given (--after is ignored in that case: a
// consumer resumes from where it left off, unconditionally), reports a gap
// as its own JSON line before any event, and either prints one page or
// follows.
func runEventsList(cmd *cobra.Command, after int64, limit int, kinds, actions, docTypes []string,
	notActor, consumer string, allProjects, follow bool) error {
	var c *core.Core
	var db interface{ Close() error }
	var projectID string
	if allProjects {
		cc, dd, err := openCore()
		if err != nil {
			return err
		}
		c, db = cc, dd
	} else {
		pctx, err := currentProject()
		if err != nil {
			return err
		}
		c, db, projectID = pctx.Core, pctx.db, pctx.Project.ID
	}
	defer db.Close()

	if consumer != "" {
		ec, err := c.EnsureEventConsumer(cmd.Context(), consumer)
		if err != nil {
			return err
		}
		after = ec.Cursor
		gap, oldest, err := c.EventGapAfter(cmd.Context(), after)
		if err != nil {
			return err
		}
		if gap {
			if err := writeJSONLine(cmd, map[string]any{"gap": true, "oldest": oldest}); err != nil {
				return err
			}
		}
	}

	q := core.EventQuery{
		ProjectID: projectID, Limit: limit,
		Kinds: kinds, Actions: actions, DocTypes: docTypes, NotActor: notActor,
	}
	fetch := func(a int64) ([]core.FeedEvent, *int64, error) {
		q.After = a
		return c.EventFeed(cmd.Context(), q)
	}
	emit := func(ev core.FeedEvent) error { return writeJSONLine(cmd, ev) }

	if !follow {
		events, _, err := fetch(after)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if err := emit(ev); err != nil {
				return err
			}
		}
		return nil
	}
	return runEventsFollow(cmd.Context(), time.Second, after, fetch, emit)
}

// runEventsFollow polls fetch every interval, starting after `after`, and
// calls emit for each event in seq order, advancing its local position from
// next each time. It stops silently when ctx is done. interval is a
// parameter, not a constant, so a test can drive many iterations without
// waiting on a real clock; the CLI passes a real time.Second.
func runEventsFollow(ctx context.Context, interval time.Duration, after int64,
	fetch func(after int64) ([]core.FeedEvent, *int64, error), emit func(core.FeedEvent) error) error {
	for {
		events, next, err := fetch(after)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if err := emit(ev); err != nil {
				return err
			}
		}
		if next != nil {
			after = *next
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func writeJSONLine(cmd *cobra.Command, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(append(b, '\n'))
	return err
}

func newEventsAckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ack NAME SEQ",
		Short: "Advance a consumer's cursor",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seq, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return core.ErrUsage("invalid_seq", "seq must be an integer", "trellis events ack NAME 42")
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			ec, err := c.AckEventConsumer(cmd.Context(), args[0], seq)
			if err != nil {
				return err
			}
			return Emit(cmd, ec, func() string { return fmt.Sprintf("%s -> %d", ec.Name, ec.Cursor) })
		},
	}
}

func newEventsConsumersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consumers",
		Short: "List event consumers: name, cursor, lag, gap",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			list, err := c.ListEventConsumers(cmd.Context())
			if err != nil {
				return err
			}
			return Emit(cmd, list, func() string { return formatConsumerTable(list) })
		},
	}
	cmd.AddCommand(newEventsConsumersRmCmd())
	return cmd
}

func newEventsConsumersRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm NAME",
		Short: "Delete a named consumer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := c.DeleteEventConsumer(cmd.Context(), args[0]); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"removed": args[0]}, func() string { return "removed " + args[0] })
		},
	}
}

func formatConsumerTable(list []core.ConsumerStatus) string {
	if len(list) == 0 {
		return "(no consumers)"
	}
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCURSOR\tLAG\tGAP")
	for _, s := range list {
		fmt.Fprintf(w, "%s\t%d\t%d\t%v\n", s.Name, s.Cursor, s.Lag, s.Gap)
	}
	w.Flush()
	return buf.String()
}
```

`db` in `runEventsList` is typed as `interface{ Close() error }` rather than `*sqlx.DB` because the two branches return `*sqlx.DB` from two different helpers (`openCore` and `currentProject`'s `projectContext.db`) that are both already `*sqlx.DB`; either works, but the narrow interface keeps this file from needing the `sqlx` import for a variable it only ever calls `Close` on.

- [ ] **Step 4: Register the command**

In `internal/cli/root.go`, in `newRootCmd`, add `newEventsCmd()` to the `root.AddCommand(...)` list (anywhere in the list; the codebase's own comment says command files never edit each other, only add themselves here):

```go
	root.AddCommand(newInitCmd(), newCardCmd(), newBoardCmd(), newColumnCmd(), newLabelCmd(), newUICmd(), newSearchCmd(), newRecallCmd(), newConfigCmd(), newAgentCmd(), newBackupCmd(), newVersionCmd(), newUpdateCmd(),
		newKnowledgeCmd(), newArtifactCmd(), newLinkCmd(), newGraphCmd(), newVectorCmd(), newDaemonCmd(), newDoctorCmd(), newMaintenanceCmd(), newTUICmd(), newEventsCmd())
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestRunEventsFollow|TestEvents' -v`
Expected: PASS, all nine.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/events.go internal/cli/events_cmd_test.go internal/cli/root.go
git commit -m "$(cat <<'EOF'
feat(cli): add trellis events, ack, consumers, and --follow

Output is JSON lines, one FeedEvent per line, so a shell pipeline or a
long-running extension can read it incrementally. --consumer resumes from a
named cursor and reports a gap left by a prior maintenance prune before any
event; --follow polls every second without needing the daemon, on an
interval a test can shrink to avoid a real sleep.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: `.trellis.yaml` — read and validate a repository's own config

**Files:**
- Modify: `internal/config/config.go` (new: `RepoSafe`, `RepoConfigPath`, `RepoDoc`, `LoadRepo`, `setConfigField`)
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `Config`, `GetValue` (unchanged).
- Produces:
  - `func RepoSafe(key string) bool`
  - `func RepoConfigPath(dir string) (path string, err error)`
  - `type RepoDoc struct { Config Config; Present map[string]bool; Extensions any }`
  - `func LoadRepo(dir string) (doc RepoDoc, path string, ok bool, err error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go` (add `"gopkg.in/yaml.v3"` is already imported; also add nothing new to imports — `os`, `path/filepath`, `testing` are already there):

```go
func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestRepoSafeKeys(t *testing.T) {
	safe := []string{
		"card.ls_limit", "card.duplicate_check", "card.duplicate_threshold",
		"lease.ttl", "board.default_columns",
		"labels.preset", "labels.require_on_card", "tags.require_on_card",
		"search.limit", "search.method",
	}
	for _, k := range safe {
		if !RepoSafe(k) {
			t.Errorf("RepoSafe(%q) = false, want true", k)
		}
	}
	refused := []string{"ui.port", "ui.bind", "ui.enabled", "db.busy_timeout_ms", "git.timeout",
		"search.vector.enabled", "search.vector.embed_command", "search.vector.endpoint"}
	for _, k := range refused {
		if RepoSafe(k) {
			t.Errorf("RepoSafe(%q) = true, want false", k)
		}
	}
}

func TestRepoConfigPathBothPresentIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  lease.ttl: 45m\n")
	writeRepoFile(t, dir, ".trellis.yml", "config:\n  lease.ttl: 45m\n")

	if _, err := RepoConfigPath(dir); err == nil {
		t.Fatal("want an error naming both files")
	}
}

func TestRepoConfigPathAcceptsEitherExtension(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yml", "config:\n  lease.ttl: 45m\n")
	path, err := RepoConfigPath(dir)
	if err != nil {
		t.Fatalf("RepoConfigPath: %v", err)
	}
	if filepath.Base(path) != ".trellis.yml" {
		t.Errorf("path = %q, want .trellis.yml", path)
	}
}

func TestRepoConfigPathWithNeitherFileIsNotAnError(t *testing.T) {
	path, err := RepoConfigPath(t.TempDir())
	if err != nil || path != "" {
		t.Fatalf("path=%q err=%v, want (\"\", nil)", path, err)
	}
}

func TestLoadRepoWithNoFileReturnsNotOK(t *testing.T) {
	doc, path, ok, err := LoadRepo(t.TempDir())
	if err != nil || ok || path != "" || len(doc.Present) != 0 {
		t.Fatalf("doc=%+v path=%q ok=%v err=%v, want a not-ok zero result", doc, path, ok, err)
	}
}

func TestLoadRepoAppliesAllowedKeys(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", `config:
  card.ls_limit: 25
  lease.ttl: 45m
  labels.require_on_card: true
  board.default_columns: [todo, doing, done]
`)
	doc, path, ok, err := LoadRepo(dir)
	if err != nil {
		t.Fatalf("LoadRepo: %v", err)
	}
	if !ok || path == "" {
		t.Fatalf("ok=%v path=%q, want a loaded repo config", ok, path)
	}
	if doc.Config.Card.LsLimit != 25 {
		t.Errorf("Card.LsLimit = %d, want 25", doc.Config.Card.LsLimit)
	}
	if doc.Config.Lease.TTL != "45m" {
		t.Errorf("Lease.TTL = %q, want 45m", doc.Config.Lease.TTL)
	}
	if !doc.Config.Labels.RequireOnCard {
		t.Error("Labels.RequireOnCard = false, want true")
	}
	if len(doc.Config.Board.DefaultColumns) != 3 || doc.Config.Board.DefaultColumns[0] != "todo" {
		t.Errorf("Board.DefaultColumns = %v", doc.Config.Board.DefaultColumns)
	}
	for _, k := range []string{"card.ls_limit", "lease.ttl", "labels.require_on_card", "board.default_columns"} {
		if !doc.Present[k] {
			t.Errorf("Present[%q] = false, want true", k)
		}
	}
	if doc.Present["search.limit"] {
		t.Error("Present[\"search.limit\"] = true, but the file never set it")
	}
}

func TestLoadRepoRefusesADisallowedKey(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  ui.port: 9999\n")
	_, path, _, err := LoadRepo(dir)
	if err == nil {
		t.Fatal("want an error: ui.port is not repository-safe")
	}
	if !strings.Contains(err.Error(), "ui.port") {
		t.Errorf("error %q does not name the refused key", err)
	}
	_ = path
}

func TestLoadRepoRefusesAnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  nonexistent.key: 1\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "nonexistent.key") {
		t.Fatalf("err = %v, want an error naming nonexistent.key", err)
	}
}

func TestLoadRepoRejectsABadValue(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  card.ls_limit: not-a-number\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "card.ls_limit") {
		t.Fatalf("err = %v, want an error naming card.ls_limit", err)
	}
}

func TestLoadRepoRejectsAnUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "storage:\n  path: /tmp\n")
	_, _, _, err := LoadRepo(dir)
	if err == nil || !strings.Contains(err.Error(), "storage") {
		t.Fatalf("err = %v, want an error naming the unknown top-level key storage", err)
	}
}

func TestLoadRepoPassesExtensionsThroughUntouched(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", `config:
  lease.ttl: 45m

extensions:
  actions:
    - on: knowledge.created
      type: finding
      run: ./scripts/review-finding.sh
`)
	doc, _, ok, err := LoadRepo(dir)
	if err != nil || !ok {
		t.Fatalf("LoadRepo: ok=%v err=%v", ok, err)
	}
	m, isMap := doc.Extensions.(map[string]any)
	if !isMap {
		t.Fatalf("Extensions = %#v (%T), want a map", doc.Extensions, doc.Extensions)
	}
	actions, isSlice := m["actions"].([]any)
	if !isSlice || len(actions) != 1 {
		t.Fatalf("Extensions[actions] = %#v, want a one-item list", m["actions"])
	}
}

func TestLoadRepoWithEmptyDirReadsNothing(t *testing.T) {
	doc, path, ok, err := LoadRepo("")
	if err != nil || ok || path != "" || doc.Extensions != nil {
		t.Fatalf("doc=%+v path=%q ok=%v err=%v, want a not-ok zero result for an empty dir", doc, path, ok, err)
	}
}
```

Add `"strings"` to `internal/config/config_test.go`'s imports if not already present (check the existing import block; `os` and `path/filepath` are already there per the file read earlier).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config -run 'TestRepoSafe|TestRepoConfigPath|TestLoadRepo' -v`
Expected: the package does not compile — `RepoSafe`, `RepoConfigPath`, `RepoDoc` and `LoadRepo` are undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/config/config.go`:

```go
// RepoSafe reports whether a repository's .trellis.yaml may set key. Every
// key is refused until listed here: the daemon, the web UI, storage and
// anything else that belongs to the machine are never listed, because a
// repository file is committed and arrives with every clone — it must not be
// able to redirect storage, open a port, or run a program.
func RepoSafe(key string) bool {
	switch key {
	case "card.ls_limit", "card.duplicate_check", "card.duplicate_threshold",
		"lease.ttl",
		"board.default_columns",
		"labels.preset", "labels.require_on_card",
		"tags.require_on_card",
		"search.limit", "search.method":
		return true
	default:
		return false
	}
}

// RepoConfigPath returns the repository config file under dir: ".trellis.yaml"
// or ".trellis.yml". Both present is an error naming both. Neither present
// returns ("", nil): dir simply has no repository config.
func RepoConfigPath(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	yamlPath := filepath.Join(dir, ".trellis.yaml")
	ymlPath := filepath.Join(dir, ".trellis.yml")
	_, err1 := os.Stat(yamlPath)
	_, err2 := os.Stat(ymlPath)
	if err1 != nil && !errors.Is(err1, os.ErrNotExist) {
		return "", err1
	}
	if err2 != nil && !errors.Is(err2, os.ErrNotExist) {
		return "", err2
	}
	has1, has2 := err1 == nil, err2 == nil
	switch {
	case has1 && has2:
		return "", fmt.Errorf("%s and %s are both present; keep only one", yamlPath, ymlPath)
	case has1:
		return yamlPath, nil
	case has2:
		return ymlPath, nil
	default:
		return "", nil
	}
}

// RepoDoc is a parsed, validated .trellis.yaml. Config holds only the keys
// RepoSafe allows, decoded onto a zero Config so GetValue can read them back
// with its existing per-key formatting. Present marks exactly which dotted
// keys the file set, distinguishing an explicit value from one that happens
// to share Config's zero value. Extensions is the "extensions" subtree
// exactly as written, decoded to a generic value: core parses it as YAML and
// never interprets it.
type RepoDoc struct {
	Config     Config
	Present    map[string]bool
	Extensions any
}

// LoadRepo reads and validates the repository config file in dir. ok is
// false with a zero RepoDoc when dir has no ".trellis.yaml"/".trellis.yml"
// (including dir == ""); err is non-nil when one exists but is invalid —
// both files present, an unknown top-level key, a key a repository may not
// set, or a value that does not parse for its key — and always names the
// file and, where applicable, the key.
func LoadRepo(dir string) (doc RepoDoc, path string, ok bool, err error) {
	path, err = RepoConfigPath(dir)
	if err != nil || path == "" {
		return RepoDoc{}, path, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RepoDoc{}, path, false, fmt.Errorf("read %s: %w", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return RepoDoc{}, path, false, fmt.Errorf("parse %s: %w", path, err)
	}
	doc = RepoDoc{Config: Config{}, Present: map[string]bool{}}
	if len(root.Content) == 0 {
		// An empty file: a valid, empty document.
		return doc, path, true, nil
	}
	body := root.Content[0]
	if body.Kind != yaml.MappingNode {
		return RepoDoc{}, path, false, fmt.Errorf("%s: the document must be a mapping", path)
	}

	for i := 0; i+1 < len(body.Content); i += 2 {
		topKey := body.Content[i].Value
		topVal := body.Content[i+1]
		switch topKey {
		case "config":
			if topVal.Kind != yaml.MappingNode {
				return RepoDoc{}, path, false, fmt.Errorf("%s: config must be a mapping of dotted keys to values", path)
			}
			for j := 0; j+1 < len(topVal.Content); j += 2 {
				key := topVal.Content[j].Value
				valueNode := topVal.Content[j+1]
				if !RepoSafe(key) {
					return RepoDoc{}, path, false, fmt.Errorf("%s: %q may not be set by a repository", path, key)
				}
				if err := setConfigField(&doc.Config, key, valueNode); err != nil {
					return RepoDoc{}, path, false, fmt.Errorf("%s: %q: %w", path, key, err)
				}
				doc.Present[key] = true
			}
		case "extensions":
			var ext any
			if err := topVal.Decode(&ext); err != nil {
				return RepoDoc{}, path, false, fmt.Errorf("%s: extensions: %w", path, err)
			}
			doc.Extensions = ext
		default:
			return RepoDoc{}, path, false, fmt.Errorf("%s: unknown top-level key %q", path, topKey)
		}
	}
	return doc, path, true, nil
}

// setConfigField decodes one repository-safe dotted key's YAML value into the
// matching field of cfg. Every key RepoSafe allows is handled here.
func setConfigField(cfg *Config, key string, node *yaml.Node) error {
	switch key {
	case "card.ls_limit":
		return node.Decode(&cfg.Card.LsLimit)
	case "card.duplicate_check":
		return node.Decode(&cfg.Card.DuplicateCheck)
	case "card.duplicate_threshold":
		return node.Decode(&cfg.Card.DuplicateThreshold)
	case "lease.ttl":
		return node.Decode(&cfg.Lease.TTL)
	case "board.default_columns":
		return node.Decode(&cfg.Board.DefaultColumns)
	case "labels.preset":
		return node.Decode(&cfg.Labels.Preset)
	case "labels.require_on_card":
		return node.Decode(&cfg.Labels.RequireOnCard)
	case "tags.require_on_card":
		return node.Decode(&cfg.Tags.RequireOnCard)
	case "search.limit":
		return node.Decode(&cfg.Search.Limit)
	case "search.method":
		return node.Decode(&cfg.Search.Method)
	default:
		return fmt.Errorf("not a repository-safe key")
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config -run 'TestRepoSafe|TestRepoConfigPath|TestLoadRepo' -v`
Expected: PASS, all eleven.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): read and validate .trellis.yaml / .trellis.yml

config: takes only the dotted keys RepoSafe allows, declared in one
exhaustive function so a new key is refused until marked safe. extensions is
parsed as YAML and never interpreted. Both files present, an unknown
top-level key, a refused key, or a value that does not parse for its key is
an error naming the file and the key.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Precedence — `EffectiveValue` gains a repo layer, wired into `config get`/`set`/`ls`

This task changes a function every non-test caller in the codebase depends
on, so it fixes every one of them itself rather than leaving the tree
non-building at a commit boundary: `internal/config/config.go`'s core change,
then `internal/cli/root.go` and `internal/cli/vector.go` (two call sites that
never read a repository-safe key, fixed with an empty `RepoDoc`), then
`internal/cli/config.go`'s `currentProject`/`get`/`set`/`ls` (the two call
sites that actually need the real repo layer).

**Files:**
- Modify: `internal/config/config.go` (new: `AllKeys`, `LoadWithPresence`, `presentKeys`; changed: `EffectiveValue` signature)
- Modify: `internal/config/config_test.go`
- Modify: `internal/cli/root.go` (`configInt`'s `EffectiveValue` call)
- Modify: `internal/cli/vector.go` (`effectiveVectorConfig`'s `EffectiveValue` call)
- Modify: `internal/cli/config.go` (`projectContext` gains `cfg`/`present`/`repo`; `currentProject` computes the repo directory and loads it; `get`/`set`/`ls` use the new `EffectiveValue` and report `"repo"`)
- Create: `internal/cli/config_cmd_test.go`

**Interfaces:**
- Consumes: Task 4's `RepoDoc`, `RepoSafe`, `LoadRepo`, `GetValue` (unchanged), `GetProjectConfig` (unchanged), `resolve.Identity.RootPath`, `resolve.Identify`.
- Produces:
  - `func AllKeys() []string`
  - `func LoadWithPresence() (cfg Config, present map[string]bool, err error)` — `Load()` itself is untouched and keeps its existing `(Config, error)` signature, so none of its thirteen existing call sites change in this task.
  - `func EffectiveValue(ctx context.Context, cfg Config, present map[string]bool, repo RepoDoc, db *sqlx.DB, projectID, key string) (value, source string, err error)` — **signature change**; `source` is one of `"default"`, `"config"`, `"repo"`, `"project"`.
  - `func ApplyRepoOverrides(cfg Config, repo RepoDoc) Config` — every repository-safe key `repo.Present` marks, copied onto `cfg`; Task 6's `currentBoard` uses this to prime the Core from a whole merged `Config` rather than one key at a time.
  - `projectContext` gains `cfg config.Config`, `present map[string]bool` and `repo config.RepoDoc`, populated by `currentProject`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestAllKeysIncludesEveryKeyGetValueKnows(t *testing.T) {
	for _, k := range AllKeys() {
		if _, found := GetValue(Defaults(), k); !found {
			t.Errorf("AllKeys lists %q, but GetValue does not recognize it", k)
		}
	}
	if !slices.Contains(AllKeys(), "lease.ttl") {
		t.Error("AllKeys is missing lease.ttl")
	}
}

func TestLoadWithPresenceDistinguishesFileFromDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("lease:\n  ttl: 10m\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, present, err := LoadWithPresence()
	if err != nil {
		t.Fatalf("LoadWithPresence: %v", err)
	}
	if cfg.Lease.TTL != "10m" {
		t.Fatalf("Lease.TTL = %q, want 10m", cfg.Lease.TTL)
	}
	if !present["lease.ttl"] {
		t.Error(`present["lease.ttl"] = false, want true: the file set it`)
	}
	if present["card.ls_limit"] {
		t.Error(`present["card.ls_limit"] = true, but the file never mentioned it`)
	}
}

func TestLoadWithPresenceOnAnEmptyHomeMarksNothingPresent(t *testing.T) {
	t.Setenv("TRELLIS_HOME", t.TempDir())
	_, present, err := LoadWithPresence()
	if err != nil {
		t.Fatalf("LoadWithPresence: %v", err)
	}
	if len(present) != 0 {
		t.Errorf("present = %v, want empty: there is no config.yaml", present)
	}
}

func TestEffectiveValueReportsDefaultThenConfigThenRepoThenProject(t *testing.T) {
	// openTestDB (config_test.go:248) already exists and sets up both
	// `project` and `project_config`; reuse it rather than declaring a
	// second, duplicate in-memory schema helper.
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// 1. Nothing set anywhere: default.
	value, source, err := EffectiveValue(ctx, Defaults(), map[string]bool{}, RepoDoc{}, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "default" || value != "30m" {
		t.Fatalf("value=%q source=%q, want 30m/default", value, source)
	}

	// 2. The global file set it: config.
	globalCfg := Defaults()
	globalCfg.Lease.TTL = "20m"
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"lease.ttl": true}, RepoDoc{}, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "config" || value != "20m" {
		t.Fatalf("value=%q source=%q, want 20m/config", value, source)
	}

	// 3. The repository file also set it: repo wins over the global file.
	repo := RepoDoc{Config: Config{Lease: LeaseConfig{TTL: "45m"}}, Present: map[string]bool{"lease.ttl": true}}
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"lease.ttl": true}, repo, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "repo" || value != "45m" {
		t.Fatalf("value=%q source=%q, want 45m/repo", value, source)
	}

	// 4. A project override wins over everything.
	if err := SetProjectConfig(ctx, db, "p1", "lease.ttl", "5m"); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}
	value, source, err = EffectiveValue(ctx, globalCfg, map[string]bool{"lease.ttl": true}, repo, db, "p1", "lease.ttl")
	if err != nil {
		t.Fatalf("EffectiveValue: %v", err)
	}
	if source != "project" || value != "5m" {
		t.Fatalf("value=%q source=%q, want 5m/project", value, source)
	}
}

func TestEffectiveValueUnknownKey(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, _, err := EffectiveValue(context.Background(), Defaults(), map[string]bool{}, RepoDoc{}, db, "p1", "no.such.key"); err == nil {
		t.Fatal("want an error for an unknown key")
	}
}
```

Add `"slices"` to `internal/config/config_test.go`'s imports (`"context"`, `"github.com/jmoiron/sqlx"` and `_ "modernc.org/sqlite"` are already imported, verified above).

This signature change also breaks the file's existing `TestProjectOverrideTakesPrecedence` (`config_test.go:153`), which calls the old 5-argument `EffectiveValue(ctx, cfg, db, projectID, key)`. Update its two calls now, in the same edit:

```go
	// Without override, should get default.
	value, source, err := EffectiveValue(ctx, cfg, map[string]bool{}, RepoDoc{}, db, projectID, "card.ls_limit")
```

and

```go
	// Now should get the override.
	value, source, err = EffectiveValue(ctx, cfg, map[string]bool{}, RepoDoc{}, db, projectID, "card.ls_limit")
```

replacing the two existing `EffectiveValue(ctx, cfg, db, projectID, "card.ls_limit")` calls in that test (its assertions on `source`/`value` do not change: neither `present` nor `RepoDoc` names `card.ls_limit`, so the resolved source is still `"default"` then `"project"`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config -run 'TestAllKeys|TestLoadWithPresence|TestEffectiveValue|TestApplyRepoOverrides|TestProjectOverrideTakesPrecedence' -v`
Expected: the package does not compile — `AllKeys`, `LoadWithPresence` and `ApplyRepoOverrides` are undefined, and `EffectiveValue` is called with the wrong number of arguments both in the new tests and in the pre-existing `TestProjectOverrideTakesPrecedence`.

- [ ] **Step 3: Add `AllKeys`, `LoadWithPresence` and `presentKeys`**

Append to `internal/config/config.go`:

```go
// AllKeys lists every dotted config key GetValue understands, in the order
// `config ls` displays them. internal/cli/config.go's newConfigLsCmd uses
// this instead of keeping its own copy of the list.
func AllKeys() []string {
	return []string{
		"ui.port", "ui.bind", "ui.enabled",
		"db.busy_timeout_ms",
		"git.timeout",
		"lease.ttl",
		"board.default_columns",
		"labels.preset", "labels.require_on_card",
		"tags.require_on_card",
		"card.ls_limit", "card.duplicate_check", "card.duplicate_threshold",
		"search.limit",
		"search.method",
		"search.vector.enabled", "search.vector.provider", "search.vector.embed_command", "search.vector.endpoint",
		"search.vector.model", "search.vector.dimension", "search.vector.limit",
	}
}

// LoadWithPresence is Load, plus which dotted keys the global file itself
// set. This is why it exists: EffectiveValue must report "config", not
// "default", for a value that came from the file, and by the time Load
// applies its defaults onto an unset field the two are indistinguishable.
func LoadWithPresence() (Config, map[string]bool, error) {
	cfg := Defaults()
	path, err := configPath()
	if err != nil {
		return cfg, map[string]bool{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, map[string]bool{}, nil
		}
		return cfg, map[string]bool{}, fmt.Errorf("read config: %w", err)
	}
	var raw Config
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, map[string]bool{}, fmt.Errorf("parse config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, map[string]bool{}, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	return cfg, presentKeys(raw), nil
}

// presentKeys reports which of AllKeys raw actually set, by comparing
// GetValue's string form of raw against the same key read from an entirely
// zero Config. This shares GetValue's one known blind spot: an explicit
// value equal to the zero value (search.limit: 0, ui.enabled: true) is
// indistinguishable from absence — the same limitation applyDefaults already
// has no way around.
func presentKeys(raw Config) map[string]bool {
	present := map[string]bool{}
	var zero Config
	for _, key := range AllKeys() {
		rawStr, _ := GetValue(raw, key)
		zeroStr, _ := GetValue(zero, key)
		if rawStr != zeroStr {
			present[key] = true
		}
	}
	return present
}
```

- [ ] **Step 4: Change `EffectiveValue`'s signature**

Replace the existing `EffectiveValue` in `internal/config/config.go` entirely:

```go
// EffectiveValue resolves key through every precedence layer, lowest to
// highest: built-in defaults, the global config file, a repository's
// .trellis.yaml, then a project override in the database. source names
// whichever layer answered: "default", "config", "repo" or "project".
func EffectiveValue(ctx context.Context, cfg Config, present map[string]bool, repo RepoDoc, db *sqlx.DB, projectID, key string) (value, source string, err error) {
	value, found := GetValue(cfg, key)
	if !found {
		return "", "", fmt.Errorf("unknown config key: %q", key)
	}
	source = "default"
	if present[key] {
		source = "config"
	}
	if repo.Present[key] {
		if v, ok := GetValue(repo.Config, key); ok {
			value, source = v, "repo"
		}
	}

	override, ok, err := GetProjectConfig(ctx, db, projectID, key)
	if err != nil {
		return "", "", err
	}
	if ok {
		return override, "project", nil
	}
	return value, source, nil
}

// ApplyRepoOverrides returns cfg with every repository-safe key repo.Present
// sets copied on top, field by field. EffectiveValue answers one key at a
// time against a database; this is for a caller — currentBoard, priming the
// Core's lease TTL, default columns and label/tag requirements — that needs
// a whole Config to seed something with, before any per-key project override
// is even in the picture.
func ApplyRepoOverrides(cfg Config, repo RepoDoc) Config {
	for key := range repo.Present {
		switch key {
		case "card.ls_limit":
			cfg.Card.LsLimit = repo.Config.Card.LsLimit
		case "card.duplicate_check":
			cfg.Card.DuplicateCheck = repo.Config.Card.DuplicateCheck
		case "card.duplicate_threshold":
			cfg.Card.DuplicateThreshold = repo.Config.Card.DuplicateThreshold
		case "lease.ttl":
			cfg.Lease.TTL = repo.Config.Lease.TTL
		case "board.default_columns":
			cfg.Board.DefaultColumns = repo.Config.Board.DefaultColumns
		case "labels.preset":
			cfg.Labels.Preset = repo.Config.Labels.Preset
		case "labels.require_on_card":
			cfg.Labels.RequireOnCard = repo.Config.Labels.RequireOnCard
		case "tags.require_on_card":
			cfg.Tags.RequireOnCard = repo.Config.Tags.RequireOnCard
		case "search.limit":
			cfg.Search.Limit = repo.Config.Search.Limit
		case "search.method":
			cfg.Search.Method = repo.Config.Search.Method
		}
	}
	return cfg
}
```

Add the test for it to Step 1's block, so it lands with the rest (append inside the same `internal/config/config_test.go` edit):

```go
func TestApplyRepoOverridesCopiesEveryPresentKey(t *testing.T) {
	cfg := Defaults()
	repo := RepoDoc{
		Config: Config{
			Card:   CardConfig{LsLimit: 5},
			Lease:  LeaseConfig{TTL: "5m"},
			Search: SearchConfig{Limit: 3, Method: "vector"},
		},
		Present: map[string]bool{"card.ls_limit": true, "lease.ttl": true, "search.limit": true, "search.method": true},
	}
	merged := ApplyRepoOverrides(cfg, repo)
	if merged.Card.LsLimit != 5 {
		t.Errorf("Card.LsLimit = %d, want 5", merged.Card.LsLimit)
	}
	if merged.Lease.TTL != "5m" {
		t.Errorf("Lease.TTL = %q, want 5m", merged.Lease.TTL)
	}
	if merged.Search.Limit != 3 || merged.Search.Method != "vector" {
		t.Errorf("Search = %+v, want Limit=3 Method=vector", merged.Search)
	}
	// board.default_columns was never present: the default must survive.
	if len(merged.Board.DefaultColumns) != len(Defaults().Board.DefaultColumns) {
		t.Errorf("Board.DefaultColumns = %v, want the untouched default", merged.Board.DefaultColumns)
	}
}
```

- [ ] **Step 5: Run the config package tests to verify they pass**

Run: `go test ./internal/config -run 'TestAllKeys|TestLoadWithPresence|TestEffectiveValue|TestApplyRepoOverrides|TestProjectOverrideTakesPrecedence' -v`
Expected: PASS, all six.

- [ ] **Step 6: Fix `root.go` and `vector.go`, the two call sites that never read a repo-safe key**

`EffectiveValue`'s signature changed, so `go build ./...` now fails at every call site. Find them:

Run: `grep -rn "config\.EffectiveValue(" /home/mtchen/Personal/trellis-worktrees/knowledge-artifacts/internal --include="*.go"`

Expected output (four call sites, all in `internal/cli`): `internal/cli/config.go` (in `newConfigGetCmd` and `newConfigLsCmd`), `internal/cli/root.go` (in `configInt`), `internal/cli/vector.go` (in `effectiveVectorConfig`). Fix the latter two now, with an empty `RepoDoc`, because neither ever reads a repository-safe key (`configInt` is used for `card.ls_limit`, which — see Step 9 below — is layered in earlier, by `currentBoard`, before `configInt` ever runs; `effectiveVectorConfig` only reads `search.vector.*`, none of which is repository-safe). `internal/cli/config.go`'s two call sites are fixed later in this same task, in Step 9, with the real repo layer.

In `internal/cli/root.go`'s `configInt`, change:

```go
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Defaults()
	}
	raw, _, err := config.EffectiveValue(ctx, cfg, app.db, app.Project.ID, key)
```

to:

```go
	cfg, present, err := config.LoadWithPresence()
	if err != nil {
		cfg = config.Defaults()
		present = map[string]bool{}
	}
	raw, _, err := config.EffectiveValue(ctx, cfg, present, config.RepoDoc{}, app.db, app.Project.ID, key)
```

In `internal/cli/vector.go`'s `effectiveVectorConfig`, change:

```go
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Defaults()
	}
	get := func(key, fallback string) string {
		v, _, e := config.EffectiveValue(ctx, cfg, db, projectID, key)
```

to:

```go
	cfg, present, err := config.LoadWithPresence()
	if err != nil {
		cfg = config.Defaults()
		present = map[string]bool{}
	}
	// No search.vector.* key is repository-safe (Global Constraints), so an
	// empty RepoDoc is correct here, not a placeholder to fill in later.
	get := func(key, fallback string) string {
		v, _, e := config.EffectiveValue(ctx, cfg, present, config.RepoDoc{}, db, projectID, key)
```

Note that `internal/cli/config.go`'s two call sites are still broken at this point (`go build ./internal/cli` fails) — that is expected mid-task and is resolved by Step 9 below, before this task's single commit.

- [ ] **Step 7: Write the failing CLI tests**

Create `internal/cli/config_cmd_test.go`:

```go
package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

// repoEnv sets up a fresh TRELLIS_HOME and a real directory that resolves to
// project TEST through a .trellis pin (not TRELLIS_PROJECT), so a
// .trellis.yaml placed alongside the pin is actually read. It returns the
// directory and chdirs the test process into it, restoring the original
// working directory on cleanup.
func repoEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("TRELLIS_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".trellis"), []byte("TEST"), 0o600); err != nil {
		t.Fatalf("write pin: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	path, err := home.DBPath()
	if err != nil {
		t.Fatalf("home.DBPath: %v", err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	id, err := resolve.Identify(dir)
	if err != nil {
		t.Fatalf("resolve.Identify: %v", err)
	}
	if _, err := core.New(db, core.RealClock{}, "test").EnsureProject(context.Background(), id); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	return dir
}

func TestConfigGetReportsConfigForAGlobalFileValue(t *testing.T) {
	dir := repoEnv(t)
	root := os.Getenv("TRELLIS_HOME")
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("lease:\n  ttl: 20m\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	_ = dir

	out := runCmd(t, "config", "get", "lease.ttl")
	if !strings.Contains(out, "20m") || !strings.Contains(out, "config") {
		t.Fatalf("output = %q, want the global file's value and source \"config\"", out)
	}
}

func TestConfigGetReportsRepoForARepositoryFileValue(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  lease.ttl: 45m\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	out := runCmd(t, "config", "get", "lease.ttl")
	if !strings.Contains(out, "45m") || !strings.Contains(out, "repo") {
		t.Fatalf("output = %q, want the repository file's value and source \"repo\"", out)
	}
}

func TestConfigGetRepoLosesToAProjectOverride(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  lease.ttl: 45m\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	runCmd(t, "config", "set", "lease.ttl", "5m")

	out := runCmd(t, "config", "get", "lease.ttl")
	if !strings.Contains(out, "5m") || !strings.Contains(out, "project") {
		t.Fatalf("output = %q, want the project override and source \"project\"", out)
	}
}

func TestConfigGetFailsOnAMalformedRepoFile(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  ui.port: 9999\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	if _, err := runCmdErr(t, "config", "get", "lease.ttl"); err == nil {
		t.Fatal("want an error: ui.port is not repository-safe")
	}
}

func TestConfigLsReportsRepoSource(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  search.limit: 5\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	out := runCmd(t, "config", "ls", "--json")
	if !strings.Contains(out, `"Key":"search.limit"`) || !strings.Contains(out, `"Source":"repo"`) {
		t.Fatalf("output does not show search.limit as repo-sourced:\n%s", out)
	}
}

func TestConfigViaProjectFlagReadsNoRepoFile(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  lease.ttl: 45m\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// Move outside dir so the only way this could see .trellis.yaml is a bug
	// that reads it despite --project bypassing directory resolution.
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(orig)

	out := runCmd(t, "config", "get", "lease.ttl", "--project", "TEST")
	if strings.Contains(out, "45m") || strings.Contains(out, "\"repo\"") {
		t.Fatalf("output = %q, want the repo file ignored under --project", out)
	}
}
```

- [ ] **Step 8: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestConfig' -v`
Expected: the package does not compile — `internal/cli/config.go` still calls the pre-Step-4 `EffectiveValue` signature.

- [ ] **Step 9: Rewrite `currentProject` and the `get`/`set`/`ls` commands**

In `internal/cli/config.go`, replace the `projectContext` struct and `currentProject`:

```go
// projectContext holds the current project info needed for config operations.
type projectContext struct {
	Core    *core.Core
	Project core.Project
	db      *sqlx.DB
	cfg     config.Config
	present map[string]bool
	repo    config.RepoDoc
}

// currentProject resolves the current project without requiring a board. It
// also resolves the directory that answered — the .trellis pin's directory,
// or the repository root when there is no pin yet — and loads that
// directory's .trellis.yaml, if any. A project named by --project or
// TRELLIS_PROJECT skips directory resolution entirely, so it reads no
// repository file, matching the design.
func currentProject() (*projectContext, error) {
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}

	var p core.Project
	var repoDir string
	if key := projectKey(); key != "" {
		// --project XPSCTL settings a project you are not standing in.
		if p, err = c.ProjectByKey(context.Background(), key); err != nil {
			db.Close()
			return nil, err
		}
	} else {
		dir, err := os.Getwd()
		if err != nil {
			db.Close()
			return nil, err
		}
		id, err := resolve.Identify(dir)
		if err != nil {
			db.Close()
			return nil, core.ErrUsage("unresolved", err.Error(), "trellis init --pin")
		}
		repoDir = id.RootPath
		if p, err = c.EnsureProject(context.Background(), id); err != nil {
			db.Close()
			return nil, err
		}
	}

	cfg, present, err := config.LoadWithPresence()
	if err != nil {
		// Log but don't fail: config file issues are warnings, not hard stops.
		// Fall back to defaults.
		cfg = config.Defaults()
		present = map[string]bool{}
	}
	repo, _, _, err := config.LoadRepo(repoDir)
	if err != nil {
		db.Close()
		return nil, core.ErrUsage("bad_repo_config", err.Error(), "fix the file .trellis.yaml/.trellis.yml names")
	}

	return &projectContext{
		Core:    c,
		Project: p,
		db:      db,
		cfg:     cfg,
		present: present,
		repo:    repo,
	}, nil
}
```

Add `"github.com/mtch3n/trellis/internal/config"` is already imported; add nothing new here (`resolve` is already imported too).

Now update every `config.EffectiveValue` call in this file. In `newConfigGetCmd`, replace the whole `RunE` body:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			// A project override always wins where one resolves; outside a
			// repository there is nothing to override with, so the global
			// default (or the repo file, if standing in one) is the answer.
			// The source field says which happened, so no flag is needed to
			// ask.
			pctx, perr := resolveConfigProject()
			if perr != nil {
				return perr
			}
			if pctx != nil {
				defer pctx.db.Close()
				value, source, err := config.EffectiveValue(cmd.Context(), pctx.cfg, pctx.present, pctx.repo, pctx.db, pctx.Project.ID, key)
				if err != nil {
					return core.ErrUsage("unknown_key", err.Error(), "trellis config ls")
				}

				return Emit(cmd, map[string]string{
					"key":    key,
					"value":  value,
					"source": source,
				}, func() string {
					return fmt.Sprintf("%s = %s  (%s)", key, value, source)
				})
			}

			// Global scope: just get the default value.
			globalCfg, present, err := config.LoadWithPresence()
			if err != nil {
				globalCfg, present = config.Defaults(), map[string]bool{}
			}
			value, found := config.GetValue(globalCfg, key)
			if !found {
				return core.ErrUsage("unknown_key",
					fmt.Sprintf("unknown config key: %q", key),
					"trellis config ls")
			}
			source := "default"
			if present[key] {
				source = "config"
			}

			return Emit(cmd, map[string]string{
				"key":    key,
				"value":  value,
				"source": source,
			}, func() string {
				return fmt.Sprintf("%s = %s  (%s)", key, value, source)
			})
		},
```

In `newConfigSetCmd`, replace the two `config.Load()`-based lines:

```go
			globalCfg, err := config.Load()
			if err != nil {
				globalCfg = config.Defaults()
			}

			_, found := config.GetValue(globalCfg, key)
```

with:

```go
			globalCfg, err := config.Load()
			if err != nil {
				globalCfg = config.Defaults()
			}
			_, found := config.GetValue(globalCfg, key)
```

(unchanged — `newConfigSetCmd` writes a project override and never calls `EffectiveValue`, so it needs no signature update; this step is a no-op included for completeness of review, confirming the diff correctly leaves it alone).

In `newConfigLsCmd`, replace the whole `RunE` body:

```go
		RunE: func(cmd *cobra.Command, _ []string) error {
			var rows []configRow

			pctx, perr := resolveConfigProject()
			if perr != nil {
				return perr
			}
			if pctx != nil {
				defer pctx.db.Close()

				for _, key := range config.AllKeys() {
					value, source, err := config.EffectiveValue(cmd.Context(), pctx.cfg, pctx.present, pctx.repo, pctx.db, pctx.Project.ID, key)
					if err != nil {
						continue // Skip unknown keys (shouldn't happen).
					}
					rows = append(rows, configRow{Key: key, Value: value, Source: source})
				}

				return Emit(cmd, rows, func() string {
					return formatConfigTable(rows)
				})
			}

			// Global scope: just show defaults, or the global file's values.
			globalCfg, present, err := config.LoadWithPresence()
			if err != nil {
				globalCfg, present = config.Defaults(), map[string]bool{}
			}
			for _, key := range config.AllKeys() {
				value, _ := config.GetValue(globalCfg, key)
				source := "default"
				if present[key] {
					source = "config"
				}
				rows = append(rows, configRow{Key: key, Value: value, Source: source})
			}

			return Emit(cmd, rows, func() string {
				return formatConfigTable(rows)
			})
		},
```

`newConfigLsCmd` no longer needs its hand-written `allKeys := []string{...}` local slice; delete it (it is replaced by `config.AllKeys()` above).

- [ ] **Step 10: Run the CLI tests to verify they pass**

Run: `go test ./internal/cli -run 'TestConfig' -v`
Expected: PASS, all six.

- [ ] **Step 11: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass — this is the first full `go build ./...` since Step 4 changed `EffectiveValue`'s signature, and it must succeed now that every call site (Steps 6 and 9) compiles again.

- [ ] **Step 12: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/cli/root.go internal/cli/vector.go internal/cli/config.go internal/cli/config_cmd_test.go
git commit -m "$(cat <<'EOF'
feat(config): a repo layer in EffectiveValue, wired into config get/set/ls

Precedence is now default < global file < repository .trellis.yaml < project
override. LoadWithPresence tracks which keys the global file itself set, so
config get can finally tell a file value from a built-in default instead of
always calling it "default". currentProject resolves the directory that
answered project resolution (empty under --project/TRELLIS_PROJECT, matching
the design) and loads its .trellis.yaml through LoadRepo; get and ls now
report "repo" as a source, and a malformed repository file surfaces as an
error instead of being silently ignored. vector.go and configInt pass an
empty RepoDoc: neither ever reads a repository-safe key.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: The repo layer applies wherever a setting is read, not only in `config get`

**Files:**
- Modify: `internal/cli/root.go` (`appCtx` gains `cfg`; `currentBoard` learns the repo directory and re-applies layered settings; `configInt` stops reloading config)
- Modify: `internal/cli/search.go` (one inline `config.Load()` replaced)

**Interfaces:**
- Consumes: Task 4 (`config.LoadRepo`), Task 5 (`config.LoadWithPresence`, `config.EffectiveValue`, `config.ApplyRepoOverrides`), `resolve.Identity.RootPath`.
- Produces: `appCtx.cfg config.Config` — the fully layered (default < global < repo) config for the resolved project, minus project overrides (which `openCore`'s three `Set*` calls have never consulted — see the Global Constraints note on this being pre-existing, unchanged scope).

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/config_cmd_test.go`:

```go
func TestCardLsLimitRespectsRepoConfig(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  card.ls_limit: 1\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	runCmd(t, "card", "new", "--title", "one")
	runCmd(t, "card", "new", "--title", "two")

	out := runCmd(t, "card", "ls", "--json")
	if strings.Count(out, `"title":`) != 1 {
		t.Fatalf("card ls printed %d cards, want the repo's ls_limit of 1 to apply:\n%s",
			strings.Count(out, `"title":`), out)
	}
}

func TestLabelRequireOnCardRespectsRepoConfig(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  labels.require_on_card: true\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	if _, err := runCmdErr(t, "card", "new", "--title", "no label"); cliErrCode(err) != "label_required" {
		t.Errorf("err code = %q, want label_required: the repo's labels.require_on_card must apply", cliErrCode(err))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestCardLsLimitRespectsRepoConfig|TestLabelRequireOnCardRespectsRepoConfig' -v`
Expected: FAIL — both currently apply only the global config, so `card ls` prints both cards and the second test's card creation succeeds instead of failing.

- [ ] **Step 3: Give `appCtx` a `cfg` field, and have `currentBoard` layer the repo file in**

In `internal/cli/root.go`, add `cfg config.Config` to `appCtx`:

```go
type appCtx struct {
	Core    *core.Core
	Project core.Project
	Board   core.Board
	db      *sqlx.DB
	cfg     config.Config
}
```

Replace `currentBoard` entirely:

```go
// currentBoard resolves the working directory to a project and then to a
// board. There is no cwd fallback: outside a git repository with no pin this
// exits 2 rather than silently creating a project.
func currentBoard() (*appCtx, error) {
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}

	key := projectKey()
	var p core.Project
	var repoDir string
	if key != "" {
		// Naming a project skips cwd resolution entirely: --project must work
		// from outside any repository.
		if p, err = c.ProjectByKey(context.Background(), key); err != nil {
			db.Close()
			return nil, err
		}
	} else {
		dir, err := os.Getwd()
		if err != nil {
			db.Close()
			return nil, err
		}
		// Identify returns a plain error: resolve cannot import core without an
		// import cycle. Exit codes are a CLI concern, so the wrapping happens here.
		id, err := resolve.Identify(dir)
		if err != nil {
			db.Close()
			return nil, core.ErrUsage("unresolved", err.Error(), "trellis init --pin")
		}
		repoDir = id.RootPath
		if p, err = c.EnsureProject(context.Background(), id); err != nil {
			db.Close()
			return nil, err
		}
	}

	// openCore already primed the Core's lease TTL, default columns and
	// label/tag requirements from the global file alone. Now that the
	// project's directory (if any) is known, re-derive those same settings
	// with the repository file layered in (ApplyRepoOverrides) and re-apply
	// them: a repo-safe key wins over the global file, and a project
	// override — which these three Core setters have never consulted — still
	// does not apply here, unchanged from today.
	cfg, present, cfgErr := config.LoadWithPresence()
	if cfgErr != nil {
		cfg, present = config.Defaults(), map[string]bool{}
	}
	_ = present
	repo, _, _, repoErr := config.LoadRepo(repoDir)
	if repoErr != nil {
		db.Close()
		return nil, core.ErrUsage("bad_repo_config", repoErr.Error(), "fix the file .trellis.yaml/.trellis.yml names")
	}
	effective := config.ApplyRepoOverrides(cfg, repo)
	if ttl, err := time.ParseDuration(effective.Lease.TTL); err == nil {
		c.SetLeaseTTL(ttl.Milliseconds())
	}
	c.SetDefaultColumns(effective.Board.DefaultColumns)
	c.SetCardRequirements(effective.Labels.RequireOnCard, effective.Tags.RequireOnCard)

	// --board wins; otherwise TRELLIS_BOARD; otherwise the selection rules in
	// core.SelectBoard (sole board, then the default, else exit 2).
	requested := cmp.Or(boardFlag, os.Getenv("TRELLIS_BOARD"))
	b, err := c.SelectBoard(context.Background(), p.ID, requested)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &appCtx{Core: c, Project: p, Board: b, db: db, cfg: effective}, nil
}
```

Now replace `configInt` to use `app.cfg` instead of reloading:

```go
// configInt reads a project-effective integer setting, falling back to def when
// the value is missing or unparseable: a bad setting must not break a listing.
func configInt(ctx context.Context, app *appCtx, key string, def int) int {
	raw, _, err := config.EffectiveValue(ctx, app.cfg, map[string]bool{}, config.RepoDoc{}, app.db, app.Project.ID, key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
```

`app.cfg` is already the merged default/global/repo value (`currentBoard` built it above), so `configInt` only needs `EffectiveValue`'s remaining job: layering a project override on top. Passing an empty `present`/`RepoDoc` here is correct, not a placeholder — `app.cfg` already reflects everything those two parameters would otherwise re-derive, and `EffectiveValue`'s `present`/`repo.Present` checks would only ever raise the reported *source* string from "default" to "config"/"repo", a distinction `configInt`'s caller (`card ls --limit`'s fallback) does not use.

- [ ] **Step 4: Replace `search.go`'s inline `config.Load()`**

In `internal/cli/search.go`, inside `withBoard(func(app *appCtx) error { ... })`, replace:

```go
				cfg, loadErr := config.Load()
				if loadErr != nil {
					cfg = config.Defaults()
				}
				if method != "" {
					cfg.Search.Method = method
				}
```

with:

```go
				cfg := app.cfg
				if method != "" {
					cfg.Search.Method = method
				}
```

If this removal leaves `config` unused as an import in `search.go`, check with:

Run: `grep -n "config\." /home/mtchen/Personal/trellis-worktrees/knowledge-artifacts/internal/cli/search.go`

If nothing besides the removed lines used the `config` package in this file, remove the `"github.com/mtch3n/trellis/internal/config"` import line from `search.go`'s import block; otherwise leave it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestCardLsLimitRespectsRepoConfig|TestLabelRequireOnCardRespectsRepoConfig' -v`
Expected: PASS, both.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass — the `GOOS=windows` build matters here specifically because this task edits `root.go`, used by every command.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/search.go
git commit -m "$(cat <<'EOF'
feat(cli): the repository config layer applies to every command, not just config get

currentBoard now resolves the directory that answered project resolution
before priming the Core's lease TTL, default columns and label/tag
requirements, so a repo-safe .trellis.yaml key actually changes what those
commands do. appCtx carries the merged config so configInt and search.go
stop reloading the global file on their own and layering nothing repo-aware
on top of it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: `config set --repo` / `unset --repo`, and `trellis extension config`

**Files:**
- Modify: `internal/config/config.go` (new: `SetRepoValue`, `UnsetRepoValue`, `writeFileAtomic`, YAML node helpers)
- Modify: `internal/config/config_test.go`
- Modify: `internal/cli/config.go` (`--repo` flag on `set`/`unset`; new `newExtensionCmd`)
- Modify: `internal/cli/root.go` (register `newExtensionCmd()`)
- Create: `internal/cli/extension_cmd_test.go`

**Interfaces:**
- Consumes: Task 4 (`RepoSafe`, `RepoConfigPath`, `LoadRepo`, `RepoDoc`), Task 5's `newConfigSetCmd`/`newConfigUnsetCmd` (extended here, not replaced).
- Produces:
  - `func SetRepoValue(dir, key, value string) (path string, err error)`
  - `func UnsetRepoValue(dir, key string) (path string, err error)`
  - `trellis config set --repo <key> <value>`, `trellis config unset --repo <key>`
  - `trellis extension config <name>`

- [ ] **Step 1: Write the failing tests (config package)**

Append to `internal/config/config_test.go`:

```go
func TestSetRepoValueCreatesTheFileWhenNeitherExists(t *testing.T) {
	dir := t.TempDir()
	path, err := SetRepoValue(dir, "lease.ttl", "45m")
	if err != nil {
		t.Fatalf("SetRepoValue: %v", err)
	}
	if filepath.Base(path) != ".trellis.yaml" {
		t.Errorf("path = %q, want .trellis.yaml created fresh", path)
	}
	doc, _, ok, err := LoadRepo(dir)
	if err != nil || !ok {
		t.Fatalf("LoadRepo after set: ok=%v err=%v", ok, err)
	}
	if doc.Config.Lease.TTL != "45m" {
		t.Errorf("Lease.TTL = %q, want 45m", doc.Config.Lease.TTL)
	}
}

func TestSetRepoValuePreservesOtherKeysAndExtensions(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", `config:
  lease.ttl: 30m
  search.limit: 10

extensions:
  actions:
    - on: knowledge.created
      run: ./review.sh
`)
	if _, err := SetRepoValue(dir, "lease.ttl", "45m"); err != nil {
		t.Fatalf("SetRepoValue: %v", err)
	}

	doc, _, ok, err := LoadRepo(dir)
	if err != nil || !ok {
		t.Fatalf("LoadRepo: ok=%v err=%v", ok, err)
	}
	if doc.Config.Lease.TTL != "45m" {
		t.Errorf("Lease.TTL = %q, want 45m", doc.Config.Lease.TTL)
	}
	if doc.Config.Search.Limit != 10 {
		t.Errorf("Search.Limit = %d, want the untouched 10", doc.Config.Search.Limit)
	}
	m, ok := doc.Extensions.(map[string]any)
	if !ok || m["actions"] == nil {
		t.Fatalf("Extensions = %#v, want the actions list preserved", doc.Extensions)
	}
}

func TestSetRepoValueRefusesADisallowedKey(t *testing.T) {
	if _, err := SetRepoValue(t.TempDir(), "ui.port", "9999"); err == nil {
		t.Fatal("want an error: ui.port is not repository-safe")
	}
}

func TestSetRepoValueSplitsDefaultColumnsOnComma(t *testing.T) {
	dir := t.TempDir()
	if _, err := SetRepoValue(dir, "board.default_columns", "todo,doing,done"); err != nil {
		t.Fatalf("SetRepoValue: %v", err)
	}
	doc, _, ok, err := LoadRepo(dir)
	if err != nil || !ok {
		t.Fatalf("LoadRepo: ok=%v err=%v", ok, err)
	}
	want := []string{"todo", "doing", "done"}
	if len(doc.Config.Board.DefaultColumns) != len(want) {
		t.Fatalf("DefaultColumns = %v, want %v", doc.Config.Board.DefaultColumns, want)
	}
	for i, c := range want {
		if doc.Config.Board.DefaultColumns[i] != c {
			t.Errorf("DefaultColumns[%d] = %q, want %q", i, doc.Config.Board.DefaultColumns[i], c)
		}
	}
}

func TestUnsetRepoValuePreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".trellis.yaml", "config:\n  lease.ttl: 45m\n  search.limit: 10\n")

	if _, err := UnsetRepoValue(dir, "lease.ttl"); err != nil {
		t.Fatalf("UnsetRepoValue: %v", err)
	}
	doc, _, ok, err := LoadRepo(dir)
	if err != nil || !ok {
		t.Fatalf("LoadRepo: ok=%v err=%v", ok, err)
	}
	if doc.Present["lease.ttl"] {
		t.Error(`Present["lease.ttl"] = true, want it gone`)
	}
	if !doc.Present["search.limit"] || doc.Config.Search.Limit != 10 {
		t.Errorf("search.limit was not preserved: doc = %+v", doc)
	}
}

func TestUnsetRepoValueOnAMissingFileIsNotAnError(t *testing.T) {
	if _, err := UnsetRepoValue(t.TempDir(), "lease.ttl"); err != nil {
		t.Errorf("UnsetRepoValue on a directory with no repo file: %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config -run 'TestSetRepoValue|TestUnsetRepoValue' -v`
Expected: the package does not compile — `SetRepoValue` and `UnsetRepoValue` are undefined.

- [ ] **Step 3: Implement `SetRepoValue`/`UnsetRepoValue`**

Append to `internal/config/config.go`:

```go
// SetRepoValue writes key = value into dir's repository config file under
// "config:", creating .trellis.yaml if neither file exists yet, and
// preserving every other key and the "extensions" section untouched. key
// must be RepoSafe.
func SetRepoValue(dir, key, value string) (string, error) {
	if !RepoSafe(key) {
		return "", fmt.Errorf("%q may not be set by a repository", key)
	}
	path, err := RepoConfigPath(dir)
	if err != nil {
		return "", err
	}
	root, err := readOrNewRepoRoot(path)
	if err != nil {
		return "", err
	}
	if path == "" {
		path = filepath.Join(dir, ".trellis.yaml")
	}

	body := root.Content[0]
	configNode := mapValue(body, "config")
	if configNode == nil {
		configNode = &yaml.Node{Kind: yaml.MappingNode}
		body.Content = append(body.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "config"}, configNode)
	}

	var valueNode *yaml.Node
	if key == "board.default_columns" {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range strings.Split(value, ",") {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: strings.TrimSpace(item)})
		}
		valueNode = seq
	} else {
		valueNode = &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	}
	setMapValueNode(configNode, key, valueNode)

	return path, writeRepoRoot(path, root)
}

// UnsetRepoValue removes key from dir's repository config file, leaving
// every other key and "extensions" untouched. Removing a key that is not
// present, or from a file that does not exist, is not an error.
func UnsetRepoValue(dir, key string) (string, error) {
	path, err := RepoConfigPath(dir)
	if err != nil || path == "" {
		return path, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return path, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(root.Content) == 0 {
		return path, nil
	}
	body := root.Content[0]
	if configNode := mapValue(body, "config"); configNode != nil {
		deleteMapValue(configNode, key)
	}
	return path, writeRepoRoot(path, &root)
}

// readOrNewRepoRoot reads path's YAML document tree, or builds an empty one
// when path is "" (neither .trellis.yaml nor .trellis.yml exists yet).
func readOrNewRepoRoot(path string) (*yaml.Node, error) {
	if path == "" {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(root.Content) == 0 {
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	return &root, nil
}

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapValueNode sets key's value node in a mapping node, adding the pair
// if key is not already present.
func setMapValueNode(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

// deleteMapValue removes key's pair from a mapping node, if present.
func deleteMapValue(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func writeRepoRoot(path string, root *yaml.Node) error {
	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out)
}

// writeFileAtomic writes data to path via a temp file and rename, so a
// process killed mid-write never leaves a torn .trellis.yaml. This is
// separate from internal/core's writeAtomic: a repository config file is not
// knowledge content, has no database row to keep in sync with, and is
// deliberately overwritten on every set/unset rather than written
// no-clobber-once.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-trellis-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
```

Add `"strings"` to `internal/config/config.go`'s imports.

- [ ] **Step 4: Run the config package tests to verify they pass**

Run: `go test ./internal/config -run 'TestSetRepoValue|TestUnsetRepoValue' -v`
Expected: PASS, all six.

- [ ] **Step 5: Run the config package gates**

Run: `go test ./internal/config -v && go vet ./internal/config && gofmt -l internal/config`
Expected: all pass.

- [ ] **Step 6: Wire `--repo` into `config set`/`unset`, and add `trellis extension config`**

Write the failing CLI tests first. Append to `internal/cli/config_cmd_test.go`:

```go
func TestConfigSetRepoWritesTheFile(t *testing.T) {
	dir := repoEnv(t)
	runCmd(t, "config", "set", "--repo", "lease.ttl", "45m")

	data, err := os.ReadFile(filepath.Join(dir, ".trellis.yaml"))
	if err != nil {
		t.Fatalf("read .trellis.yaml: %v", err)
	}
	if !strings.Contains(string(data), "45m") {
		t.Errorf(".trellis.yaml = %q, want lease.ttl: 45m", data)
	}

	out := runCmd(t, "config", "get", "lease.ttl")
	if !strings.Contains(out, "45m") || !strings.Contains(out, "repo") {
		t.Errorf("config get = %q, want 45m/repo", out)
	}
}

func TestConfigSetRepoRefusesADisallowedKey(t *testing.T) {
	repoEnv(t)
	if _, err := runCmdErr(t, "config", "set", "--repo", "ui.port", "9999"); cliErrCode(err) == "" {
		t.Fatal("want an error: ui.port is not repository-safe")
	}
}

func TestConfigUnsetRepoRemovesOnlyThatKey(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("config:\n  lease.ttl: 45m\n  search.limit: 10\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	runCmd(t, "config", "unset", "--repo", "lease.ttl")

	data, err := os.ReadFile(filepath.Join(dir, ".trellis.yaml"))
	if err != nil {
		t.Fatalf("read .trellis.yaml: %v", err)
	}
	if strings.Contains(string(data), "lease.ttl") {
		t.Errorf(".trellis.yaml still has lease.ttl:\n%s", data)
	}
	if !strings.Contains(string(data), "search.limit") {
		t.Errorf(".trellis.yaml lost search.limit:\n%s", data)
	}
}
```

Create `internal/cli/extension_cmd_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionConfigPrintsItsSubtree(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte(`config:
  lease.ttl: 45m

extensions:
  reviewer:
    run: ./review.sh
    on: knowledge.created
`), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	out := runCmd(t, "extension", "config", "reviewer")
	if !strings.Contains(out, "review.sh") || !strings.Contains(out, "knowledge.created") {
		t.Fatalf("output = %q, want the reviewer subtree", out)
	}
}

func TestExtensionConfigOnAnUnknownNameIsEmpty(t *testing.T) {
	dir := repoEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte("extensions:\n  reviewer:\n    on: x\n"), 0o600); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	out := runCmd(t, "extension", "config", "nonexistent")
	if strings.TrimSpace(out) != "null" && strings.TrimSpace(out) != "{}" {
		t.Errorf("output = %q, want an empty/null result for an unknown extension name", out)
	}
}
```

Run: `go test ./internal/cli -run 'TestConfigSetRepo|TestConfigUnsetRepo|TestExtensionConfig' -v`
Expected: the package does not compile / the tests fail — `--repo` is an unknown flag, and `extension` is an unknown command.

Now, in `internal/cli/config.go`, add a `--repo` flag to `newConfigSetCmd` and `newConfigUnsetCmd`. Replace `newConfigUnsetCmd` entirely:

```go
func newConfigUnsetCmd() *cobra.Command {
	var repoFlag bool
	cmd := &cobra.Command{
		Use: "unset <key>", Short: "Remove a project override, or a repository config value with --repo", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if repoFlag {
				if !config.RepoSafe(key) {
					return core.ErrUsage("not_repo_safe", fmt.Sprintf("%q may not be set by a repository", key), "trellis config ls")
				}
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				path, err := config.UnsetRepoValue(dir, key)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"unset": key, "file": path}, func() string { return "unset " + key + " in " + path })
			}

			pctx, err := currentProject()
			if err != nil {
				return err
			}
			defer pctx.db.Close()
			if _, ok := config.GetValue(pctx.cfg, key); !ok {
				return core.ErrUsage("unknown_key", fmt.Sprintf("unknown config key: %q", key), "trellis config ls")
			}
			if err := config.UnsetProjectConfig(cmd.Context(), pctx.db, pctx.Project.ID, key); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"unset": key}, func() string { return "unset " + key })
		},
	}
	cmd.Flags().BoolVar(&repoFlag, "repo", false, "unset in the repository's .trellis.yaml instead of a project override")
	return cmd
}
```

In `newConfigSetCmd`, add the `--repo` flag and branch. Replace the whole `RunE` body:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			if repoFlag {
				if !config.RepoSafe(key) {
					return core.ErrUsage("not_repo_safe", fmt.Sprintf("%q may not be set by a repository", key), "trellis config ls")
				}
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				path, err := config.SetRepoValue(dir, key, value)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]string{
					"key": key, "value": value, "scope": "repo", "file": path,
				}, func() string {
					return fmt.Sprintf("%s = %s (%s)", key, value, path)
				})
			}

			// config set (without --repo) only ever writes a project
			// override: the global file is hand-edited YAML (§5.4), so there
			// is no scope to choose.
			globalCfg, err := config.Load()
			if err != nil {
				globalCfg = config.Defaults()
			}
			if _, found := config.GetValue(globalCfg, key); !found {
				return core.ErrUsage("unknown_key",
					fmt.Sprintf("unknown config key: %q", key),
					"trellis config ls")
			}

			pctx, err := currentProject()
			if err != nil {
				return err
			}
			defer pctx.db.Close()

			if err := config.SetProjectConfig(cmd.Context(), pctx.db, pctx.Project.ID, key, value); err != nil {
				return err
			}

			return Emit(cmd, map[string]string{
				"key":   key,
				"value": value,
				"scope": "project",
			}, func() string {
				return fmt.Sprintf("%s = %s (project override)", key, value)
			})
		},
```

`newConfigSetCmd` currently opens with:

```go
func newConfigSetCmd() *cobra.Command {

	cmd := &cobra.Command{
```

Change it to declare the new flag variable before the command literal:

```go
func newConfigSetCmd() *cobra.Command {
	var repoFlag bool

	cmd := &cobra.Command{
```

and add the flag registration right after the `cmd := &cobra.Command{...}` block closes, before `return cmd`:

```go
	cmd.Flags().BoolVar(&repoFlag, "repo", false, "write to the repository's .trellis.yaml instead of a project override")
```

- [ ] **Step 7: Add `trellis extension config <name>`**

Create `internal/cli/extension.go`. `Emit` already marshals `v` with `json.Marshal` for the non-TTY/`--json` path (verified: `internal/cli/output.go:17`, `internal/cli/root.go:187` and `internal/ui/server.go` all call the plain, compact `encoding/json/v2` `json.Marshal` — this codebase has no `MarshalIndent`-style call anywhere, so the `table()` closure below reuses the same compact `json.Marshal` rather than inventing one):

```go
package cli

import (
	"encoding/json/v2"
	"os"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newExtensionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "extension", Short: "Read what a repository's .trellis.yaml sets for an extension"}
	cmd.AddCommand(newExtensionConfigCmd())
	return cmd
}

func newExtensionConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config <name>",
		Short: "Print the extensions.<name> subtree of .trellis.yaml as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var repoDir string
			if projectKey() == "" {
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				repoDir = dir
			}
			doc, _, _, err := config.LoadRepo(repoDir)
			if err != nil {
				return core.ErrUsage("bad_repo_config", err.Error(), "fix the file .trellis.yaml/.trellis.yml names")
			}
			var subtree any
			if m, ok := doc.Extensions.(map[string]any); ok {
				subtree = m[args[0]]
			}
			return Emit(cmd, subtree, func() string {
				b, _ := json.Marshal(subtree)
				return string(b)
			})
		},
	}
}
```

Register the command in `internal/cli/root.go`'s `root.AddCommand(...)` list, alongside `newEventsCmd()` from Task 3:

```go
	root.AddCommand(newInitCmd(), newCardCmd(), newBoardCmd(), newColumnCmd(), newLabelCmd(), newUICmd(), newSearchCmd(), newRecallCmd(), newConfigCmd(), newAgentCmd(), newBackupCmd(), newVersionCmd(), newUpdateCmd(),
		newKnowledgeCmd(), newArtifactCmd(), newLinkCmd(), newGraphCmd(), newVectorCmd(), newDaemonCmd(), newDoctorCmd(), newMaintenanceCmd(), newTUICmd(), newEventsCmd(), newExtensionCmd())
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestConfigSetRepo|TestConfigUnsetRepo|TestExtensionConfig' -v`
Expected: PASS, all five.

- [ ] **Step 9: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/cli/config.go internal/cli/config_cmd_test.go internal/cli/extension.go internal/cli/extension_cmd_test.go internal/cli/root.go
git commit -m "$(cat <<'EOF'
feat(cli): config set/unset --repo, and trellis extension config

--repo edits .trellis.yaml through a yaml.Node document tree rather than a
struct round-trip, so every other key and the whole extensions section
survive untouched. board.default_columns is the one list-valued
repository-safe key; its value is comma-split into a YAML sequence, matching
the --label/--tag/--type convention elsewhere in this CLI. extension config
prints an extension's subtree as JSON without Trellis ever interpreting it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 8 (final): Web `GET /api/p/{key}/events` via `EventFeed`

**Files:**
- Modify: `internal/ui/server.go`
- Create: `internal/ui/events_test.go`

**Interfaces:**
- Consumes: Task 1's `core.EventFeed`, `core.EventQuery`, `core.FeedEvent`; `s.projectAndBoard`/project-by-key lookup pattern already used by other handlers (e.g. `handleActivity`, `handleLabels`).
- Produces: `GET /api/p/{key}/events?after=&limit=` → `{"events": [...], "next": ...}`, each event carrying `seq, ts, actor, kind, ref, action, field, old, new, title, type`.

**This route does not exist on this branch today.** Verified: `internal/ui/server.go`'s route table (`registerRoutes`) has `GET /api/p/{key}/b/{board}/events` (`handleBoardEvents`, a Server-Sent-Events watermark-only endpoint, unrelated) and `GET /api/activity` (`handleActivity`, a global, unscoped, `entity_type IN ('card','knowledge')`-only feed that does not select `old_value`/`new_value` at all), but no `GET /api/p/{key}/events`. The spec describes this route as **another session's uncommitted work on a different branch**, not yet merged here. This task therefore adds the route and handler now, built directly on `EventFeed` from the start — which is exactly what the spec says that other session's handler must be changed to do once it lands. **If that other session's commit merges into this branch before this task runs, do not add a second `/events` route:** find its handler (search for `"GET /api/p/{key}/events"` in `internal/ui/server.go`), and replace only its internal query with the `core.EventFeed` call and response shape below, keeping the existing route registration line.

- [ ] **Step 1: Write the failing tests**

`internal/ui` has no shared server-setup helper across test files today: `server_test.go`'s two tests and `artifacts_test.go`'s `artifactTestServer` (`internal/ui/artifacts_test.go:20`) each build a `*Server` inline with `store.Open` + `core.New` + `EnsureProject` + `CreateBoard` + `NewServer`, and — verified by reading both files — none of them set an `Authorization` header or reference any bearer token when calling `s.mux.ServeHTTP` directly in a test; the token that `NewServer` generates (`rand.Text()`, `internal/ui/server.go`) is for the real listener path, not exercised when a test drives `s.mux` in-process. This task adds its own small helper, `eventsTestServer`, following that exact established pattern.

Create `internal/ui/events_test.go`:

```go
package ui

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

// eventsTestServer follows the same setup artifactTestServer
// (internal/ui/artifacts_test.go:20) and the two tests in server_test.go
// already use: a fresh store, a project via EnsureProject, one board, and a
// *Server built directly on them. No auth header is needed to drive s.mux in
// a test. EnsureProject's own default board, plus this helper's explicit
// one, each record a "board created" event before any test writes anything
// of its own — see baselineSeq below, which every test here reads past.
func eventsTestServer(t *testing.T) (*Server, core.Project, core.Board) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-events-test")
	p, err := c.EnsureProject(context.Background(), resolve.Identity{Kind: "test", Value: "ev", SuggestedKey: "EVENTS"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBoard(context.Background(), p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(c, db, "127.0.0.1:0"), p, b
}

// baselineSeq returns the seq of the last event eventsTestServer's own setup
// already wrote (EnsureProject's default board and this helper's explicit
// one), so a test can query ?after=<baseline> and see only what it writes
// itself.
func baselineSeq(t *testing.T, s *Server, projectID string) int64 {
	t.Helper()
	setup, _, err := s.core.EventFeed(context.Background(), core.EventQuery{ProjectID: projectID})
	if err != nil {
		t.Fatalf("EventFeed (baseline): %v", err)
	}
	if len(setup) == 0 {
		return 0
	}
	return setup[len(setup)-1].Seq
}

func TestHandleEventsReturnsTheDocumentedShape(t *testing.T) {
	s, p, b := eventsTestServer(t)
	baseline := baselineSeq(t, s, p.ID)
	if _, err := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "one"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/p/%s/events?after=%d", p.Key, baseline), nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Events []core.FeedEvent `json:"events"`
		Next   *int64           `json:"next"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if len(out.Events) != 1 || out.Events[0].Kind != "card" || out.Events[0].Title != "one" {
		t.Fatalf("events = %+v", out.Events)
	}
	if out.Next == nil {
		t.Error("next must not be nil for a non-empty page")
	}
}

func TestHandleEventsRejectsABadAfter(t *testing.T) {
	s, p, _ := eventsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/p/"+p.Key+"/events?after=not-a-number", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a bad after", rec.Code)
	}
}

func TestHandleEventsRejectsABadLimit(t *testing.T) {
	s, p, _ := eventsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/p/"+p.Key+"/events?limit=not-a-number", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a bad limit", rec.Code)
	}
}

func TestHandleEventsEmptyPageHasNullNext(t *testing.T) {
	s, p, _ := eventsTestServer(t)
	// A freshly created project is never truly eventless: EnsureProject's
	// own default board writes a "board created" event too. Querying after
	// everything the setup already wrote is what makes this page empty.
	baseline := baselineSeq(t, s, p.ID)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/p/%s/events?after=%d", p.Key, baseline), nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"next":null`) {
		t.Errorf("body = %s, want next: null for an empty page", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui -run 'TestHandleEvents' -v`
Expected: every request 404s — the route does not exist yet.

- [ ] **Step 3: Add the handler and route**

In `internal/ui/server.go`, add to `registerRoutes` (near the other `/api/p/{key}/...` routes, e.g. directly after the `GET /api/p/{key}/b/{board}/events` line):

```go
	s.mux.HandleFunc("GET /api/p/{key}/events", s.handleEvents)
```

Add the handler and its response type anywhere in the file (a natural place is directly after `handleActivity`):

```go
// eventsResponse is GET /api/p/{key}/events's documented shape: every
// FeedEvent field, plus next for pagination.
type eventsResponse struct {
	Events []core.FeedEvent `json:"events"`
	Next   *int64           `json:"next"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}

	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.error(w, http.StatusBadRequest, "after must be an integer")
			return
		}
		after = v
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			s.error(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = v
	}

	events, next, err := s.core.EventFeed(ctx, core.EventQuery{ProjectID: p.ID, After: after, Limit: limit})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: events, Next: next})
}
```

If `strconv` is not already imported in `server.go` (it is — `handleSearch` and `handleGraph` both use `strconv.Atoi`), no import change is needed.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ui -run 'TestHandleEvents' -v`
Expected: PASS, all four.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./...`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/server.go internal/ui/events_test.go
git commit -m "$(cat <<'EOF'
feat(ui): serve GET /api/p/{key}/events through EventFeed

This route did not exist on this branch (verified: the only pre-existing
/events routes are the board-scoped SSE watermark and the unscoped
/api/activity feed, neither of which this replaces). It is built on
EventFeed from the start, so the two cannot drift the way an independently
written query could. If another session's uncommitted handler for this
exact route lands first, replace its query with this same EventFeed call
instead of keeping both.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```
