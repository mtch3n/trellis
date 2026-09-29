package core

import (
	"strings"
	"testing"
)

func TestTrash_TRASH_C12_a_card_on_a_trashed_board_waits_for_the_board(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	seededBoard(t, c, p)
	side, err := c.CreateBoard(ctx, p.ID, "side", true)
	if err != nil {
		t.Fatal(err)
	}
	card, _ := c.CreateCard(ctx, p.ID, side.ID, NewCard{Title: "alone"})
	if err := c.DeleteCard(ctx, p.ID, CardRef{Seq: card.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteBoard(ctx, p.ID, "side", false); err != nil {
		t.Fatal(err)
	}

	_, err = c.RestoreCard(ctx, p.ID, CardRef{Seq: card.Seq})

	e, ok := err.(*Error)
	if !ok || e.Exit != 4 || e.Fix != "trellis board restore side" {
		t.Fatalf("restore = %#v, want exit 4 with fix trellis board restore side", err)
	}
	var trashed int
	c.db.Get(&trashed, `SELECT count(*) FROM trash WHERE item_id IN (?, ?)`, card.ID, side.ID)
	if trashed != 2 {
		t.Errorf("trash rows = %d, want the card and the board still there", trashed)
	}
}

func TestTrash_TRASH_C20_restoring_a_board_brings_back_only_what_it_took(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	seededBoard(t, c, p)
	side, _ := c.CreateBoard(ctx, p.ID, "side", true)
	earlier, _ := c.CreateCard(ctx, p.ID, side.ID, NewCard{Title: "earlier"})
	with, _ := c.CreateCard(ctx, p.ID, side.ID, NewCard{Title: "with the board"})
	if err := c.DeleteCard(ctx, p.ID, CardRef{Seq: earlier.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteBoard(ctx, p.ID, "side", true); err != nil {
		t.Fatal(err)
	}

	if _, err := c.RestoreBoard(ctx, p.ID, "side"); err != nil {
		t.Fatalf("RestoreBoard: %v", err)
	}

	if _, err := c.GetCard(ctx, p.ID, CardRef{Seq: with.Seq}); err != nil {
		t.Errorf("the board's card did not come back: %v", err)
	}
	if _, err := c.GetCard(ctx, p.ID, CardRef{Seq: earlier.Seq}); exitOf(err) != 3 {
		t.Errorf("the earlier-trashed card came back: %v", err)
	}
	if _, err := c.TrashedCard(ctx, p.ID, CardRef{Seq: earlier.Seq}); err != nil {
		t.Errorf("the earlier-trashed card left the trash: %v", err)
	}
}

func TestTrash_TRASH_C22_a_trashed_projects_key_is_reserved_a_boards_name_is_not(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	seededBoard(t, c, p)
	if _, err := c.CreateBoard(ctx, p.ID, "ops", true); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteBoard(ctx, p.ID, "ops", false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "ops", true); err != nil {
		t.Errorf("a new board named like a trashed one: %v", err)
	}

	other := seededProject2(t, c)
	seededBoard(t, c, other)
	if err := c.DeleteProject(ctx, other.Key); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateProject(ctx, other.Key, false)
	e, ok := err.(*Error)
	if !ok || e.Exit != 4 || e.Fix != "trellis project restore "+other.Key {
		t.Errorf("CreateProject = %#v, want exit 4 with fix trellis project restore %s", err, other.Key)
	}
}

func TestTrash_TRASH_C24_claims_still_refuse_a_trash(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "busy"})
	if _, err := c.WithActor("agent:other").ClaimCard(ctx, card.ID, 0, false, ""); err != nil {
		t.Fatal(err)
	}

	if err := c.DeleteCard(ctx, p.ID, CardRef{Seq: card.Seq}); exitOf(err) != 4 {
		t.Errorf("DeleteCard on a claimed card = %v, want exit 4", err)
	}
	err := c.DeleteProject(ctx, p.Key)
	if e, ok := err.(*Error); !ok || e.Code != "project_has_claims" {
		t.Errorf("DeleteProject with a claim = %v, want project_has_claims", err)
	}
	var trashed int
	c.db.Get(&trashed, `SELECT count(*) FROM trash`)
	if trashed != 0 {
		t.Errorf("trash rows = %d, want 0", trashed)
	}
}

func TestTrash_TRASH_C25_the_last_live_board_stays(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()
	p := seededProject(t, c)
	seededBoard(t, c, p)
	c.CreateBoard(ctx, p.ID, "side", true)
	if err := c.DeleteBoard(ctx, p.ID, "side", false); err != nil {
		t.Fatal(err)
	}

	err := c.DeleteBoard(ctx, p.ID, "main", false)

	if e, ok := err.(*Error); !ok || e.Code != "last_board" || e.Exit != 4 {
		t.Errorf("DeleteBoard of the last live board = %v, want exit 4 last_board", err)
	}
}

func TestTrash_TRASH_C32_restoring_a_project_not_in_the_trash_is_not_found(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	seededBoard(t, c, p)

	_, err := c.RestoreProject(t.Context(), "NOPE")

	if exitOf(err) != 3 || !strings.Contains(err.Error(), "NOPE") {
		t.Errorf("RestoreProject = %v, want exit 3 naming NOPE", err)
	}
	if n := count(t, c, `SELECT count(*) FROM project`); n != 1 {
		t.Errorf("projects = %d, want 1", n)
	}
}
