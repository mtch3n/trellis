package core

import (
	"errors"
	"testing"
)

func exitOf(err error) int {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Exit
	}
	return 0
}

func TestTrash_TRASH_C3_removing_a_trashed_card_again_is_not_found(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "mistake"})
	if err := c.DeleteCard(t.Context(), p.ID, CardRef{Seq: card.Seq}); err != nil {
		t.Fatal(err)
	}
	var first int64
	if err := c.db.Get(&first, `SELECT trashed_at FROM trash WHERE item_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}

	c.clock = FixedClock{MS: first + 60_000}
	err := c.DeleteCard(t.Context(), p.ID, CardRef{Seq: card.Seq})

	if exitOf(err) != 3 {
		t.Fatalf("second rm: %v, want exit 3", err)
	}
	var rows int
	var at int64
	if err := c.db.Get(&rows, `SELECT count(*) FROM trash WHERE item_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.db.Get(&at, `SELECT trashed_at FROM trash WHERE item_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || at != first {
		t.Errorf("trash rows = %d, trashed_at = %d; want 1 row still at %d", rows, at, first)
	}
}
