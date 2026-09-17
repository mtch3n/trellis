package core

import (
	"testing"
)

func TestArchiveHidesCardAndReleasesClaim(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "stale work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	got, err := c.ArchiveCard(t.Context(), p.ID, CardRef{Seq: card.Seq})
	if err != nil {
		t.Fatalf("ArchiveCard: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Error("ArchivedAt = nil, want a timestamp")
	}
	if got.ClaimedBy != nil {
		t.Errorf("ClaimedBy = %v, want nil: archiving releases the claim", *got.ClaimedBy)
	}

	cards, err := c.ListCards(t.Context(), b.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Errorf("default listing returned %d cards, want 0", len(cards))
	}
	cards, err = c.ListCards(t.Context(), b.ID, CardFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Errorf("--archived listing returned %d cards, want 1", len(cards))
	}

	if _, err := c.UnarchiveCard(t.Context(), p.ID, CardRef{Seq: card.Seq}); err != nil {
		t.Fatalf("UnarchiveCard: %v", err)
	}
	cards, _ = c.ListCards(t.Context(), b.ID, CardFilter{})
	if len(cards) != 1 {
		t.Errorf("after unarchive: %d cards, want 1", len(cards))
	}
}
