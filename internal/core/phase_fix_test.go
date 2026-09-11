package core

import (
	"errors"
	"testing"
)

func TestLeasedCardRejectsOtherWritesButAllowsNotes(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "leased"})
	if err != nil {
		t.Fatal(err)
	}
	owner := New(c.db, FixedClock{MS: 1000}, "owner")
	if _, err := owner.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}
	other := New(c.db, FixedClock{MS: 1001}, "other")
	priority := PriorityUrgent
	if _, err := other.EditCard(t.Context(), p.ID, CardRef{Seq: card.Seq}, CardEdit{Priority: &priority}); err == nil {
		t.Fatal("other actor edited an actively leased card")
	} else if e, ok := errors.AsType[*Error](err); !ok || e.Exit != 4 {
		t.Fatalf("edit error = %v, want conflict", err)
	}
	if _, err := other.CreateNote(t.Context(), card.ID, "handoff"); err != nil {
		t.Fatalf("note exception: %v", err)
	}
}

func TestMoveCardAcrossBoardsPreservesReference(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	source := seededBoard(t, c, p)
	target, err := c.CreateBoard(t.Context(), p.ID, "target", true)
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(t.Context(), p.ID, source.ID, NewCard{Title: "move"})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := c.MoveCard(t.Context(), p.ID, target.ID, CardRef{Seq: card.Seq}, "done")
	if err != nil {
		t.Fatal(err)
	}
	if moved.BoardID != target.ID || moved.Ref != card.Ref {
		t.Fatalf("moved card = %+v, board/ref not preserved", moved)
	}
}

func TestBlockedByCycleRejected(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"})
	if err := c.BlockCard(t.Context(), p.ID, CardRef{Seq: a.Seq}, CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.BlockCard(t.Context(), p.ID, CardRef{Seq: d.Seq}, CardRef{Seq: a.Seq}); err == nil {
		t.Fatal("blocked_by cycle accepted")
	}
}

func TestBoardDeletePromotesOldestAndColumnLifecycle(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	first := seededBoard(t, c, p)
	second, err := c.CreateBoard(t.Context(), p.ID, "second", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteBoard(t.Context(), p.ID, first.Name, false); err != nil {
		t.Fatal(err)
	}
	boards, err := c.ListBoards(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 1 || !boards[0].IsDefault || boards[0].ID != second.ID {
		t.Fatalf("boards after delete = %+v", boards)
	}
	col, err := c.AddColumn(t.Context(), second.ID, "qa", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RenameColumn(t.Context(), second.ID, "qa", "verify"); err != nil {
		t.Fatal(err)
	}
	if err := c.MoveColumn(t.Context(), second.ID, "verify", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteColumn(t.Context(), second.ID, "verify", ""); err != nil {
		t.Fatal(err)
	}
	_ = col
}
