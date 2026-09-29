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

func TestTrash_TRASH_C10_restore_brings_a_card_back_whole(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	if _, err := c.CreateLabel(ctx, p.ID, "bug", ""); err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "keep me", Labels: []string{"bug"}, Tags: []string{"urgent"}})
	if err != nil {
		t.Fatal(err)
	}
	blocker, _ := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "blocker"})
	waiter, _ := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "waiter"})
	if _, err := c.CreateComment(ctx, card.ID, "a note"); err != nil {
		t.Fatal(err)
	}
	if err := c.BlockCard(ctx, p.ID, CardRef{Seq: card.Seq}, CardRef{Seq: blocker.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.BlockCard(ctx, p.ID, CardRef{Seq: waiter.Seq}, CardRef{Seq: card.Seq}); err != nil {
		t.Fatal(err)
	}
	title := "keep me, edited"
	if _, err := c.EditCard(ctx, p.ID, CardRef{Seq: card.Seq}, CardEdit{Title: &title, IfVersion: &card.Version}); err != nil {
		t.Fatal(err)
	}

	if err := c.DeleteCard(ctx, p.ID, CardRef{Seq: card.Seq}); err != nil {
		t.Fatal(err)
	}
	if bl, _ := c.Blockers(ctx, waiter.ID); len(bl) != 0 {
		t.Errorf("a trashed card still blocks: %v", bl)
	}
	got, err := c.RestoreCard(ctx, p.ID, CardRef{Seq: card.Seq})
	if err != nil {
		t.Fatalf("RestoreCard: %v", err)
	}

	if got.Title != title || got.ColumnName == "" || len(got.Labels) != 1 || len(got.Tags) != 1 {
		t.Errorf("restored card = %+v", got)
	}
	if comments, _ := c.GetCommentsByCard(ctx, card.ID); len(comments) != 1 {
		t.Errorf("comments = %v, want 1", comments)
	}
	if bl, _ := c.Blockers(ctx, card.ID); len(bl) != 1 {
		t.Errorf("its blockers = %v, want 1", bl)
	}
	if bl, _ := c.Blockers(ctx, waiter.ID); len(bl) != 1 {
		t.Errorf("what it blocks = %v, want 1", bl)
	}
	if revs, _ := c.ListCardRevisions(ctx, p.ID, CardRef{Seq: card.Seq}); len(revs) == 0 {
		t.Errorf("history lost")
	}

	if _, err := c.ArchiveCard(ctx, p.ID, CardRef{Seq: blocker.Seq}); err != nil {
		t.Fatal(err)
	}
	back, err := c.RestoreCard(ctx, p.ID, CardRef{Seq: blocker.Seq})
	if err != nil || back.ArchivedAt != nil {
		t.Errorf("archived restore = %+v, %v", back, err)
	}
}
