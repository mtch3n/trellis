package core

import (
	"testing"
)

func TestHeldWithoutNoteIgnoresFirstColumnAndNotedCards(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	inBacklog, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "not started"})
	if err != nil {
		t.Fatal(err)
	}
	silent, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "held, nothing written"})
	if err != nil {
		t.Fatal(err)
	}
	noted, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "held, wrote it down"})
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []Card{inBacklog, silent, noted} {
		if _, err := c.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, card := range []Card{silent, noted} {
		if _, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{Seq: card.Seq}, "in-progress"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.CreateNote(t.Context(), noted.ID, "what I learned"); err != nil {
		t.Fatal(err)
	}

	held, err := c.HeldWithoutNote(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0].ID != silent.ID {
		var refs []string
		for _, h := range held {
			refs = append(refs, h.Ref)
		}
		t.Fatalf("HeldWithoutNote = %v, want only %s", refs, silent.Ref)
	}
}
