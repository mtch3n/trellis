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

func TestTrash_TRASH_C30_restore_survives_a_deleted_label_and_column(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	if _, err := c.CreateLabel(ctx, p.ID, "gone", ""); err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "survivor", Column: "review", Labels: []string{"gone"}})
	if err != nil {
		t.Fatal(err)
	}
	c.CreateComment(ctx, card.ID, "still here")
	title := "survivor, edited"
	c.EditCard(ctx, p.ID, CardRef{Seq: card.Seq}, CardEdit{Title: &title, IfVersion: &card.Version})
	if err := c.DeleteCard(ctx, p.ID, CardRef{Seq: card.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteLabel(ctx, p.ID, "gone"); err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}
	if err := c.DeleteColumn(ctx, b.ID, "review", ""); err != nil {
		t.Fatalf("DeleteColumn: %v", err)
	}

	got, err := c.RestoreCard(ctx, p.ID, CardRef{Seq: card.Seq})
	if err != nil {
		t.Fatalf("RestoreCard: %v", err)
	}
	var first string
	c.db.Get(&first, `SELECT name FROM column_ WHERE board_id = ? ORDER BY position LIMIT 1`, b.ID)
	if got.ColumnName != first || len(got.Labels) != 0 {
		t.Errorf("restored into %q with labels %v; want %q and none", got.ColumnName, got.Labels, first)
	}
	if comments, _ := c.GetCommentsByCard(ctx, card.ID); len(comments) != 1 {
		t.Errorf("comments = %d, want 1", len(comments))
	}
	if revs, _ := c.ListCardRevisions(ctx, p.ID, CardRef{Seq: card.Seq}); len(revs) == 0 {
		t.Errorf("history lost")
	}
}
