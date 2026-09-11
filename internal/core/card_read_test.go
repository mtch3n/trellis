package core

import (
	"errors"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestGetCardBySeqAndUUID(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	made, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "findable"})
	if err != nil {
		t.Fatal(err)
	}

	for _, ref := range []CardRef{{Seq: made.Seq}, {UUID: made.ID}, ParseCardRef("XPSCTL-1")} {
		got, err := c.GetCard(t.Context(), p.ID, ref)
		if err != nil {
			t.Fatalf("GetCard(%+v): %v", ref, err)
		}
		if got.ID != made.ID {
			t.Errorf("GetCard(%+v).ID = %q, want %q", ref, got.ID, made.ID)
		}
	}
}

func TestGetCardMissingIsExit3(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	_, err := c.GetCard(t.Context(), p.ID, CardRef{Seq: 99})

	te, ok := errors.AsType[*Error](err)
	if !ok || te.Exit != 3 {
		t.Fatalf("error = %v, want a *core.Error with exit 3", err)
	}
}

func TestListCardsOrdersByColumnThenPriority(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	mk := func(title, col string, prio Priority) {
		prioPtr := prio
		if _, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: title, Column: col, Priority: &prioPtr}); err != nil {
			t.Fatal(err)
		}
	}
	mk("low backlog", "backlog", PriorityLow)
	mk("urgent backlog", "backlog", PriorityUrgent)
	mk("normal review", "review", PriorityNormal)

	got, err := c.ListCards(ctx, b.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"urgent backlog", "low backlog", "normal review"}
	if len(got) != len(want) {
		t.Fatalf("got %d cards, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Title != want[i] {
			t.Errorf("card %d = %q, want %q", i, got[i].Title, want[i])
		}
	}
}

func TestListCardsFiltersByColumn(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()
	if _, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "a", Column: "backlog"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "b", Column: "review"}); err != nil {
		t.Fatal(err)
	}

	got, err := c.ListCards(ctx, b.ID, CardFilter{Column: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "b" {
		t.Errorf("filtered list = %+v, want only card b", got)
	}
}

func TestListCardsBoardBoundary(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b1 := seededBoard(t, c, p)
	// Create a second board with a different name
	var b2 Board
	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		var err error
		b2, err = c.createBoard(tx, p.ID, "board2", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("createBoard: %v", err)
	}
	ctx := t.Context()

	// Create a card on each board
	if _, err := c.CreateCard(ctx, p.ID, b1.ID, NewCard{Title: "board1 card"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCard(ctx, p.ID, b2.ID, NewCard{Title: "board2 card"}); err != nil {
		t.Fatal(err)
	}

	// List cards from b1 should only see b1's card
	got, err := c.ListCards(ctx, b1.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ListCards(b1) returned %d cards, want 1", len(got))
	}
	if got[0].Title != "board1 card" {
		t.Errorf("ListCards(b1) returned %q, want 'board1 card'", got[0].Title)
	}

	// List cards from b2 should only see b2's card
	got, err = c.ListCards(ctx, b2.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ListCards(b2) returned %d cards, want 1", len(got))
	}
	if got[0].Title != "board2 card" {
		t.Errorf("ListCards(b2) returned %q, want 'board2 card'", got[0].Title)
	}
}
