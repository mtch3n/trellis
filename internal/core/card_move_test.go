package core

import (
	"errors"
	"testing"
)

func TestMoveCardRecordsBeforeAndAfter(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "movable"})
	if err != nil {
		t.Fatal(err)
	}

	moved, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{Seq: card.Seq}, "review")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	if moved.ColumnName != "review" {
		t.Errorf("ColumnName = %q, want review", moved.ColumnName)
	}
	if moved.Version != card.Version+1 {
		t.Errorf("Version = %d, want %d", moved.Version, card.Version+1)
	}

	var oldV, newV string
	err = c.db.QueryRow(
		`SELECT old_value, new_value FROM event
		  WHERE entity_id = ? AND action = 'moved' ORDER BY seq DESC LIMIT 1`,
		card.ID).Scan(&oldV, &newV)
	if err != nil {
		t.Fatalf("no moved event: %v", err)
	}
	if oldV != "backlog" || newV != "review" {
		t.Errorf("event recorded %q -> %q, want backlog -> review", oldV, newV)
	}
}
func TestMoveCardUnknownColumn(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"})

	_, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{Seq: card.Seq}, "shipped")
	te, ok := errors.AsType[*Error](err)
	if !ok || te.Exit != 3 {
		t.Fatalf("error = %v, want exit 3", err)
	}
}
func TestMoveCardBeforeReordersWithinColumn(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	one, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "one"})
	two, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "two"})
	three, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "three"})

	if _, err := c.MoveCardBefore(t.Context(), p.ID, b.ID, CardRef{Seq: three.Seq}, "backlog", one.Ref); err != nil {
		t.Fatalf("MoveCardBefore: %v", err)
	}
	cards, err := c.ListCards(t.Context(), b.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{three.Ref, one.Ref, two.Ref}
	if len(cards) != len(want) {
		t.Fatalf("got %d cards, want %d", len(cards), len(want))
	}
	for i, card := range cards {
		if card.Ref != want[i] {
			t.Errorf("cards[%d] = %s, want %s", i, card.Ref, want[i])
		}
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
