package core

import (
	"testing"
)

func TestBlockedCardIsNotClaimedUntilBlockerIsDone(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	blocker, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "first"})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BlockCard(t.Context(), p.ID, CardRef{Seq: blocked.Seq}, CardRef{Seq: blocker.Seq}); err != nil {
		t.Fatalf("BlockCard: %v", err)
	}

	list, err := c.Blockers(t.Context(), blocked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Ref != blocker.Ref || list[0].Done {
		t.Fatalf("Blockers = %+v, want one unfinished %s", list, blocker.Ref)
	}

	// Only the unblocked card is claimable.
	got, err := c.ClaimNextCard(t.Context(), b.ID, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != blocker.ID {
		t.Fatalf("claimed %v, want the unblocked %s", got, blocker.Ref)
	}
	next, err := c.ClaimNextCard(t.Context(), b.ID, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Errorf("claimed %s, want nothing: the only card left is blocked", next.Ref)
	}

	if err := c.UnblockCard(t.Context(), p.ID, CardRef{Seq: blocked.Seq}, CardRef{Seq: blocker.Seq}); err != nil {
		t.Fatalf("UnblockCard: %v", err)
	}
	after, err := c.ClaimNextCard(t.Context(), b.ID, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if after == nil || after.ID != blocked.ID {
		t.Errorf("after unblock claimed %v, want %s", after, blocked.Ref)
	}
}
func TestBlockCardRejectsSelf(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.BlockCard(t.Context(), p.ID, CardRef{Seq: card.Seq}, CardRef{Seq: card.Seq})
	if err == nil {
		t.Fatal("BlockCard(self) succeeded, want a usage error")
	}
}
