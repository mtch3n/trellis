package core

import "testing"

func TestTrash_TRASH_C31_a_trashed_cards_number_is_not_reused(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "one"})
	top, _ := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "two"})
	if err := c.DeleteCard(ctx, p.ID, CardRef{Seq: top.Seq}); err != nil {
		t.Fatal(err)
	}

	next, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "three"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Seq != top.Seq+1 {
		t.Errorf("new card seq = %d, want %d", next.Seq, top.Seq+1)
	}
	back, err := c.RestoreCard(ctx, p.ID, CardRef{Seq: top.Seq})
	if err != nil || back.Ref != top.Ref {
		t.Errorf("restore = %q, %v; want %s", back.Ref, err, top.Ref)
	}
}
