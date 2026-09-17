package ui

import (
	"net/http"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// A board can be renamed, made the one a project opens on, and removed —
// except the last one, which a project always keeps.
func TestBoardLifecycle(t *testing.T) {
	f := newCardFixture(t, "BOARDS")

	if rec := f.request(http.MethodPost, "/api/p/BOARDS/boards", `{"name":"second board"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body)
	}
	renamed := f.request(http.MethodPatch, "/api/p/BOARDS/b/second-board", `{"name":"planning"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", renamed.Code, renamed.Body)
	}
	// The slug is what a `.trellis` marker names, so it survives a rename;
	// only the name a person reads changes.
	if board := decode[boardInfo](t, renamed); board.Name != "planning" || board.Slug != "second-board" {
		t.Errorf("renamed board = %+v", board)
	}

	promoted := f.request(http.MethodPatch, "/api/p/BOARDS/b/second-board", `{"is_default":true}`)
	if promoted.Code != http.StatusOK {
		t.Fatalf("set default = %d: %s", promoted.Code, promoted.Body)
	}
	if board := decode[boardInfo](t, promoted); !board.IsDefault {
		t.Errorf("board is not the default: %+v", board)
	}

	// The board holding the fixture's card needs force; without it, nothing changes.
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/default", ""); rec.Code != http.StatusConflict {
		t.Errorf("delete a board with cards = %d, want 409: %s", rec.Code, rec.Body)
	}
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/default?force=1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("forced delete = %d: %s", rec.Code, rec.Body)
	}
	// A project also holds the board it was created with. Once that goes too,
	// one is left, and a project always has somewhere to put a card.
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/boards", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete the project's own empty board = %d: %s", rec.Code, rec.Body)
	}
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/second-board?force=1", ""); rec.Code != http.StatusConflict {
		t.Errorf("delete the last board = %d, want 409: %s", rec.Code, rec.Body)
	}
}

// Columns are added, renamed, reordered and removed, and a column with cards
// in it only goes when the request says where the cards go.
func TestColumnLifecycle(t *testing.T) {
	f := newCardFixture(t, "COLS")
	columns := func() []core.Column {
		return decode[[]core.Column](t, f.request(http.MethodGet, "/api/p/COLS/b/default/columns", ""))
	}
	names := func() []string {
		var out []string
		for _, column := range columns() {
			out = append(out, column.Name)
		}
		return out
	}
	seeded := names()
	if len(seeded) == 0 {
		t.Fatal("a new board should have seeded columns")
	}

	rec := f.request(http.MethodPost, "/api/p/COLS/b/default/columns", `{"name":"blocked","after":"backlog"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", rec.Code, rec.Body)
	}
	if added := decode[core.Column](t, rec); added.Name != "blocked" {
		t.Errorf("added column = %+v", added)
	}
	if got := names(); got[1] != "blocked" {
		t.Errorf("columns = %v, want blocked second", got)
	}

	if rec := f.request(http.MethodPatch, "/api/p/COLS/b/default/columns/blocked", `{"name":"waiting"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", rec.Code, rec.Body)
	}
	// Moving is "after this one", so an empty target makes it first.
	if rec := f.request(http.MethodPatch, "/api/p/COLS/b/default/columns/waiting", `{"after":""}`); rec.Code != http.StatusOK {
		t.Fatalf("move = %d: %s", rec.Code, rec.Body)
	}
	if got := names(); got[0] != "waiting" {
		t.Errorf("columns = %v, want waiting first", got)
	}

	if rec := f.request(http.MethodDelete, "/api/p/COLS/b/default/columns/waiting", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete an empty column = %d: %s", rec.Code, rec.Body)
	}

	// The fixture's card sits in the first seeded column, so that one cannot
	// go without somewhere for the card to land.
	filled := seeded[0]
	if rec := f.request(http.MethodDelete, "/api/p/COLS/b/default/columns/"+filled, ""); rec.Code != http.StatusConflict {
		t.Errorf("delete a column with cards = %d, want 409: %s", rec.Code, rec.Body)
	}
	if rec := f.request(http.MethodDelete, "/api/p/COLS/b/default/columns/"+filled+"?move_cards_to="+seeded[1], ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete with move_cards_to = %d: %s", rec.Code, rec.Body)
	}
	live := decode[[]columnCardsInfo](t, f.request(http.MethodGet, "/api/p/COLS/b/default/cards", ""))
	moved := 0
	for _, column := range live {
		if column.Name == seeded[1] {
			moved = len(column.Cards)
		}
	}
	if moved != 1 {
		t.Errorf("the card did not move with the column: %+v", live)
	}
}
