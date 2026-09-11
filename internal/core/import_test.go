package core

import (
	"errors"
	"testing"
)

func TestImportCardsResolvesLocalBlockers(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	cards, err := c.ImportCards(t.Context(), p.ID, b.ID, []ImportCard{
		{ID: "step1", Title: "write the migration"},
		{Title: "use it", BlockedBy: []string{"step1"}, Priority: "high"},
	})
	if err != nil {
		t.Fatalf("ImportCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("got %d cards, want 2", len(cards))
	}
	if cards[1].Priority != PriorityHigh {
		t.Errorf("second card priority = %v, want high", cards[1].Priority)
	}

	blockers, err := c.Blockers(t.Context(), cards[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0].Ref != cards[0].Ref {
		t.Fatalf("Blockers = %+v, want %s", blockers, cards[0].Ref)
	}

	// The plan's order is the claim order.
	got, err := c.ClaimNextCard(t.Context(), b.ID, 60_000)
	if err != nil || got == nil {
		t.Fatalf("ClaimNextCard = %v, %v", got, err)
	}
	if got.ID != cards[0].ID {
		t.Errorf("claimed %s, want the unblocked first step %s", got.Ref, cards[0].Ref)
	}
}

func TestImportIsAllOrNothing(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	_, err := c.ImportCards(t.Context(), p.ID, b.ID, []ImportCard{
		{Title: "fine"},
		{Title: "bad", Labels: []string{"no-such-label"}},
	})
	if err == nil {
		t.Fatal("ImportCards succeeded, want a rejection on the unknown label")
	}
	cards, err := c.ListCards(t.Context(), b.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Errorf("%d cards written, want 0: the import is one transaction", len(cards))
	}
}

func TestImportRejectsUnknownBlocker(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	_, err := c.ImportCards(t.Context(), p.ID, b.ID, []ImportCard{
		{Title: "x", BlockedBy: []string{"nope"}},
	})
	var te *Error
	if err == nil {
		t.Fatal("want an error for an unresolvable blocker")
	}
	if !errors.As(err, &te) || te.Exit != 2 {
		t.Fatalf("err = %v, want a usage error", err)
	}
}
