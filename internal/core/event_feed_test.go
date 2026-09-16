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
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Bug", Template: "finding", Sources: []string{"https://example.com/report"},
	}); err != nil {
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

	// A project-scoped read now reaches a hard-deleted entity's history
	// because the event table carries project_id.
	scoped, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID, Kinds: []string{"card"}})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("project-scoped events = %+v, want created + deleted", scoped)
	}

	// Kinds: []string{"card"} excludes kbCore's own "board created" event,
	// whose ref is still the (undeleted) board's name and would otherwise
	// trip the loop below, which assumes every returned event is this card's.
	events, _, err := c.EventFeed(t.Context(), EventQuery{Kinds: []string{"card"}})
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

	// A project-scoped read now reaches a hard-deleted entity's history
	// because the event table carries project_id.
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
	if _, err := c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Body: &body, IfVersion: &doc.Version}); err != nil {
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
	if want := DocAddress("", true, doc.Slug); len(events) != 1 || events[0].Ref != want {
		t.Fatalf("events = %+v, want ref %s", events, want)
	}
}
