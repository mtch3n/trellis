package core

import (
	"strings"
	"testing"
)

func TestSetProjectDescriptionCollapsesWhitespaceAndRecordsAnEvent(t *testing.T) {
	c := testCore(t)
	p, err := c.CreateProject(t.Context(), "DESC", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.SetProjectDescription(t.Context(), "desc", "  A kanban board\n for   agents. ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "A kanban board for agents." {
		t.Errorf("description = %q", got.Description)
	}
	reread, err := c.ProjectByKey(t.Context(), "DESC")
	if err != nil {
		t.Fatal(err)
	}
	if reread.Description != got.Description {
		t.Errorf("stored description = %q, want %q", reread.Description, got.Description)
	}
	var events int
	if err := c.db.Get(&events,
		`SELECT count(*) FROM event WHERE entity_id = ? AND action = 'edited' AND field = 'description'`, p.ID); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("edited events = %d, want 1", events)
	}

	// The same text again is not a change.
	if _, err := c.SetProjectDescription(t.Context(), "DESC", "A kanban board for agents."); err != nil {
		t.Fatal(err)
	}
	if err := c.db.Get(&events,
		`SELECT count(*) FROM event WHERE entity_id = ? AND action = 'edited'`, p.ID); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("edited events after a no-op = %d, want 1", events)
	}
}

func TestSetProjectDescriptionRefusesMoreThanFiftyWords(t *testing.T) {
	c := testCore(t)
	if _, err := c.CreateProject(t.Context(), "DESC", false); err != nil {
		t.Fatal(err)
	}
	fifty := strings.Repeat("word ", MaxDescriptionWords)
	if _, err := c.SetProjectDescription(t.Context(), "DESC", fifty); err != nil {
		t.Fatalf("fifty words: %v", err)
	}
	if code := errCode(t, func() error {
		_, err := c.SetProjectDescription(t.Context(), "DESC", fifty+"more")
		return err
	}()); code != "description_too_long" {
		t.Errorf("code = %s, want description_too_long", code)
	}
}
