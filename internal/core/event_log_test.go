package core

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestEventLogOrdersBySeqAndPages(t *testing.T) {
	// vaultCore's project and board setup already recorded a "board created"
	// event before either card exists, so every query here is filtered to
	// Entities: []string{"card"} — otherwise the very first page would return
	// that board event, not "one".
	c, p, b := vaultCore(t)
	card1, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "one"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	card2, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "two"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	first, next, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"card"}, Limit: 1})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(first) != 1 || first[0].Title != "one" || next == nil {
		t.Fatalf("first page = %+v, next = %v", first, next)
	}

	second, next2, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"card"}, After: *next, Limit: 1})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(second) != 1 || second[0].Title != "two" || next2 == nil || *next2 <= *next {
		t.Fatalf("second page = %+v, next = %v (first next %v)", second, next2, next)
	}
	_ = card1
	_ = card2

	empty, emptyNext, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"card"}, After: *next2})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(empty) != 0 || emptyNext != nil {
		t.Errorf("empty page = %+v, next = %v, want nil next", empty, emptyNext)
	}
}

func TestEventLogDefaultAndMaxLimit(t *testing.T) {
	c, p, _ := vaultCore(t)

	// The default (1000) and the cap (5000) only bite past that many rows.
	// Driving that many writes through CreateCard would make this the
	// slowest test in the suite for no benefit -- the limit logic does not
	// care how a row got there -- so the events are inserted directly.
	// Entities: []string{"card"} below excludes vaultCore's own "board created"
	// event, so every one of these rows, and only these, is in scope.
	const bulk = 5100
	var sb strings.Builder
	args := make([]any, 0, bulk*3)
	for i := 0; i < bulk; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(?, 'bulk', 'card', ?, 'created', ?)")
		args = append(args, c.clock.NowMS(), "bulk-card-"+strconv.Itoa(i), p.ID)
	}
	if _, err := c.db.Exec(
		`INSERT INTO event (ts, actor, entity_type, entity_id, action, project_id) VALUES `+sb.String(),
		args...); err != nil {
		t.Fatalf("bulk insert events: %v", err)
	}

	def, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"card"}})
	if err != nil {
		t.Fatalf("EventLog (default limit): %v", err)
	}
	if len(def) != 1000 {
		t.Fatalf("default limit must be 1000; got %d events", len(def))
	}

	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"card"}, Limit: 50000})
	if err != nil {
		t.Fatalf("EventLog (limit above cap): %v", err)
	}
	if len(events) != 5000 {
		t.Fatalf("a limit above 5000 must still be capped sanely; got %d events for %d writes", len(events), bulk)
	}
}

func TestEventLogFiltersByEntity(t *testing.T) {
	c, p, b := vaultCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "card"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateLabel(t.Context(), p.ID, "urgent", "needs attention"); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"label"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(events) != 1 || events[0].Entity != "label" || events[0].Ref != "urgent" {
		t.Fatalf("events = %+v, want exactly one label event named urgent", events)
	}
}

// review-cli #8: a stale or misspelled --entity must fail loudly, not match
// nothing silently. "note" is the exact case: migration 0021 renamed those
// events to "comment", and the CLI's own help text said "note" until this
// fix.
func TestEventLogRejectsAnUnknownEntity(t *testing.T) {
	c, p, b := vaultCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "card"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	_, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"note"}})
	ce, ok := errors.AsType[*Error](err)
	if !ok || ce.Code != "unknown_entity" {
		t.Fatalf("err = %v, want an unknown_entity usage error", err)
	}
}

func TestEventLogExcludesReadByDefaultButNotWhenAsked(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Runbook"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.ReadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}

	byDefault, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	for _, ev := range byDefault {
		if ev.Action == "read" {
			t.Errorf("a read event appeared without being asked for: %+v", ev)
		}
	}

	withReads, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Actions: []string{"read"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(withReads) != 1 || withReads[0].Action != "read" {
		t.Fatalf("events = %+v, want exactly the one read event", withReads)
	}
}

func TestEventLogFiltersByTemplate(t *testing.T) {
	c, p, b := vaultCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "unrelated"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Bug", Template: "finding", Sources: []string{"https://example.com/report"},
	}); err != nil {
		t.Fatalf("CreateEntry finding: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Notes", Template: ""}); err != nil {
		t.Fatalf("CreateEntry note: %v", err)
	}

	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Templates: []string{"finding"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(events) != 1 || events[0].Entity != "entry" || events[0].Template != "finding" || events[0].Title != "Bug" {
		t.Fatalf("events = %+v, want exactly the one finding, and no card event", events)
	}
}

func TestEventLogNotActorSkipsItsOwnWrites(t *testing.T) {
	c, p, b := vaultCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "mine"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, NotActor: c.actor})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events = %+v, want none: every write in this test was made by NotActor", events)
	}
}

func TestEventLogScopesByProject(t *testing.T) {
	c, p, b := vaultCore(t)
	p2 := seededProject2(t, c)
	b2 := seededBoard(t, c, p2)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "in p"}); err != nil {
		t.Fatalf("CreateCard p: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p2.ID, b2.ID, NewCard{Title: "in p2"}); err != nil {
		t.Fatalf("CreateCard p2: %v", err)
	}

	// Filtered to Entities: []string{"card"} throughout: vaultCore and
	// seededBoard each already record a "board created" event for their own
	// project, and this test is about project scoping, not board noise.
	scoped, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"card"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Title != "in p" {
		t.Fatalf("scoped events = %+v, want only p's card", scoped)
	}

	all, _, err := c.EventLog(t.Context(), EventQuery{Entities: []string{"card"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("--all-projects (ProjectID \"\") card events = %+v, want both", all)
	}
}

func TestEventLogNeverReturnsEditedContent(t *testing.T) {
	c, p, b := vaultCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Old Title"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	newTitle := "New Title"
	version := card.Version
	if _, err := c.EditCard(t.Context(), p.ID, ParseCardRef(card.ID), CardEdit{Title: &newTitle, IfVersion: &version}); err != nil {
		t.Fatalf("EditCard: %v", err)
	}

	// The raw row really does hold both titles: prove the event log's
	// blanking is its own policy, not a coincidence of what got written.
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

	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Actions: []string{"edited"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(events) != 1 || events[0].Old != "" || events[0].New != "" {
		t.Fatalf("events = %+v, want old/new blanked even though the row holds real text", events)
	}
}

func TestEventLogCardMovedCarriesColumnNames(t *testing.T) {
	c, p, b := vaultCore(t)
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

	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Actions: []string{"moved"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(events) != 1 || events[0].Old != cols[0].Name || events[0].New != cols[1].Name {
		t.Fatalf("events = %+v, want old=%s new=%s", events, cols[0].Name, cols[1].Name)
	}
}

func TestEventLogDeletedCardHasEmptyRefAndTitleExceptItsOwnDeletedEvent(t *testing.T) {
	c, p, b := vaultCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Gone"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := c.DeleteCard(t.Context(), p.ID, ParseCardRef(card.ID)); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}

	// A project-scoped read now reaches a hard-deleted entity's history
	// because the event table carries project_id.
	scoped, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"card"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("project-scoped events = %+v, want created + deleted", scoped)
	}

	// Entities: []string{"card"} excludes vaultCore's own "board created"
	// event, whose ref is still the (undeleted) board's name and would
	// otherwise trip the loop below, which assumes every returned event is
	// this card's.
	events, _, err := c.EventLog(t.Context(), EventQuery{Entities: []string{"card"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
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

func TestEventLogDeletedEntryHasEmptyRefAndTitleExceptItsOwnDeletedEvent(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Temporary"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if err := c.DeleteEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	// A project-scoped read now reaches a hard-deleted entity's history
	// because the event table carries project_id.
	// Entities: []string{"entry"} excludes vaultCore's own "board created"
	// event, whose ref and title are still the (undeleted) board's — the
	// loop below assumes every returned event is this entry's.
	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"entry"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
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

func TestEventLogPrivateEntryCarriesRefAndTitleOnly(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Prod credentials", Private: true})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	body := "the actual secret body"
	if _, err := c.EditEntryFields(t.Context(), p.ID, entry.Slug, EntryEdit{Body: &body, IfVersion: &entry.Version}); err != nil {
		t.Fatalf("EditEntryFields: %v", err)
	}

	// Entities: []string{"entry"} excludes vaultCore's own "board created"
	// event, whose title is the board's name, not this entry's.
	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"entry"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
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

func TestEventLogCommentRefIsItsCardsRef(t *testing.T) {
	c, p, b := vaultCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Has a comment"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateComment(t.Context(), card.ID, "handed off"); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"comment"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(events) != 1 || events[0].Ref != card.Ref || events[0].Title != "Has a comment" {
		t.Fatalf("comment event = %+v, want ref=%s title=%s", events[0], card.Ref, "Has a comment")
	}
}

func TestEventLogEntryRefUsesGlobalForAPromotedEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Widely useful"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "applies everywhere"); err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}

	// Entities: []string{"entry"} excludes vaultCore's own "board created"
	// event, which also has action "created".
	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Entities: []string{"entry"}, Actions: []string{"created"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if want := EntryAddress("", true, entry.Slug); len(events) != 1 || events[0].Ref != want {
		t.Fatalf("events = %+v, want ref %s", events, want)
	}
}

// A template filter still shows the deletion of an entry, whose row, and
// template, are gone.
func TestEventLogTemplateFilterKeepsDeletions(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Latency", Template: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatal(err)
	}
	events, _, err := c.EventLog(t.Context(), EventQuery{ProjectID: p.ID, Templates: []string{"research"}})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	if !slices.Contains(actions, "deleted") {
		t.Errorf("actions = %v, want the deletion", actions)
	}
}
