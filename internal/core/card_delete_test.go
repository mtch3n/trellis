package core

import (
	"errors"
	"testing"
)

func TestDeleteCardRemovesRowAndKeepsHistory(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "mistake"})

	if err := c.DeleteCard(t.Context(), p.ID, CardRef{Seq: card.Seq}); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}

	var n int
	if err := c.db.Get(&n, `SELECT count(*) FROM card WHERE id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("card row count = %d, want 0", n)
	}

	// The event log records every change; deleting a card must not erase it.
	if err := c.db.Get(&n,
		`SELECT count(*) FROM event WHERE entity_id = ? AND action = 'deleted'`, card.ID); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("deleted events = %d, want 1", n)
	}
}

func TestDeleteMissingCardIsExit3(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	err := c.DeleteCard(t.Context(), p.ID, CardRef{Seq: 404})

	te, ok := errors.AsType[*Error](err)
	if !ok || te.Exit != 3 {
		t.Fatalf("error = %v, want exit 3", err)
	}
}
