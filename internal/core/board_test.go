package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestCreateBoardSlugifiesAndSeedsColumns(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	var b Board
	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		var err error
		b, err = c.createBoard(tx, p.ID, "Main Board!", true, true)
		return err
	})
	if err != nil {
		t.Fatalf("createBoard: %v", err)
	}
	if b.Slug != "main-board" {
		t.Errorf("Slug = %q, want %q", b.Slug, "main-board")
	}
	if b.ProjectID != p.ID {
		t.Errorf("ProjectID = %q, want %q", b.ProjectID, p.ID)
	}
	if !b.IsDefault {
		t.Error("IsDefault should be true")
	}
	if b.CreatedAt != c.clock.NowMS() {
		t.Errorf("CreatedAt = %d, want %d (from the injected clock)", b.CreatedAt, c.clock.NowMS())
	}

	cols, err := c.ListColumns(t.Context(), b.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) != 4 {
		t.Fatalf("got %d columns after createBoard, want 4 (the default set)", len(cols))
	}

	var action string
	if err := c.db.Get(&action,
		"SELECT action FROM event WHERE entity_type='board' AND entity_id=? ORDER BY seq DESC LIMIT 1", b.ID); err != nil {
		t.Fatal(err)
	}
	if action != "created" {
		t.Errorf("last board event = %q, want created", action)
	}
}

func TestSlugifyCollapsesAndTrims(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Main Board!", "main-board"},
		{"  Sprint 42  ", "sprint-42"},
		{"---weird***name---", "weird-name"},
		{"UPPER", "upper"},
	}
	for _, tc := range tests {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSlugifyRetainsUnicodeLettersAndDigits covers this machine owner's real
// naming convention: board names in Chinese are not an edge case here. An
// ASCII-only a-z0-9 test would silently discard every non-Latin letter,
// slugifying "重構" to "" and colliding every such board onto the -2/-3
// suffix chain with no relation to what the user typed.
func TestSlugifyRetainsUnicodeLettersAndDigits(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"重構", "重構"},
		{"日本語 Board", "日本語-board"},
	}
	for _, tc := range tests {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSlugifyAllSymbolsFallsBackToBoard covers a name with no letters or
// digits at all: it must not collapse to "", which would produce the URL
// segment /p/<KEY>/b/ and make every such board indistinguishable from
// createBoard's ordinary slug-collision suffixing.
func TestSlugifyAllSymbolsFallsBackToBoard(t *testing.T) {
	got := slugify("!!!###")
	if got == "" {
		t.Fatal("slugify(\"!!!###\") = \"\", want a non-empty fallback")
	}
	if got != "board" {
		t.Errorf("slugify(\"!!!###\") = %q, want the literal fallback %q", got, "board")
	}
}

// TestCreateBoardAllSymbolNamesGetDistinctSlugs exercises the fallback
// through createBoard's existing DB-aware collision loop: two boards whose
// names both slugify to the "board" fallback must still land on two
// distinct, non-empty slugs, the same way two boards named "Sprint" would.
func TestCreateBoardAllSymbolNamesGetDistinctSlugs(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		first, err := c.createBoard(tx, p.ID, "!!!###", false, true)
		if err != nil {
			return err
		}
		second, err := c.createBoard(tx, p.ID, "@@@", false, true)
		if err != nil {
			return err
		}
		if first.Slug == "" || second.Slug == "" {
			t.Fatalf("got empty slug(s): first=%q second=%q", first.Slug, second.Slug)
		}
		if first.Slug == second.Slug {
			t.Errorf("two all-symbol board names collided on the same slug %q", first.Slug)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestCreateBoardSlugCollisionAppendsSuffix(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		first, err := c.createBoard(tx, p.ID, "Sprint", false, true)
		if err != nil {
			return err
		}
		if first.Slug != "sprint" {
			t.Errorf("first Slug = %q, want %q", first.Slug, "sprint")
		}

		second, err := c.createBoard(tx, p.ID, "Sprint!!", false, true)
		if err != nil {
			return err
		}
		if second.Slug != "sprint-2" {
			t.Errorf("second Slug = %q, want %q", second.Slug, "sprint-2")
		}

		third, err := c.createBoard(tx, p.ID, "SPRINT", false, true)
		if err != nil {
			return err
		}
		if third.Slug != "sprint-3" {
			t.Errorf("third Slug = %q, want %q", third.Slug, "sprint-3")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestListBoards(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if _, err := c.createBoard(tx, p.ID, "alpha", true, true); err != nil {
			return err
		}
		_, err := c.createBoard(tx, p.ID, "beta", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	boards, err := c.ListBoards(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("ListBoards: %v", err)
	}
	if len(boards) != 2 {
		t.Fatalf("got %d boards, want 2", len(boards))
	}
}

func TestSelectBoardByRequestedName(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if _, err := c.createBoard(tx, p.ID, "alpha", true, true); err != nil {
			return err
		}
		_, err := c.createBoard(tx, p.ID, "beta", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	b, err := c.SelectBoard(t.Context(), p.ID, "beta")
	if err != nil {
		t.Fatalf("SelectBoard: %v", err)
	}
	if b.Name != "beta" {
		t.Errorf("Name = %q, want beta", b.Name)
	}
}

func TestSelectBoardUnknownNameListsAvailable(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if _, err := c.createBoard(tx, p.ID, "alpha", true, true); err != nil {
			return err
		}
		_, err := c.createBoard(tx, p.ID, "beta", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	_, err = c.SelectBoard(t.Context(), p.ID, "nope")
	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error = %v, want a *core.Error", err)
	}
	if te.Exit != 3 {
		t.Errorf("Exit = %d, want 3 (ErrNotFound)", te.Exit)
	}
	if !strings.Contains(te.Msg, "alpha") || !strings.Contains(te.Msg, "beta") {
		t.Errorf("message %q should list the available boards", te.Msg)
	}
}

func TestSelectBoardSingleBoardIsImplicitDefault(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		_, err := c.createBoard(tx, p.ID, "solo", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	b, err := c.SelectBoard(t.Context(), p.ID, "")
	if err != nil {
		t.Fatalf("SelectBoard: %v", err)
	}
	if b.Name != "solo" {
		t.Errorf("Name = %q, want solo", b.Name)
	}
}

func TestSelectBoardReturnsDefaultAmongSeveral(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if _, err := c.createBoard(tx, p.ID, "alpha", false, true); err != nil {
			return err
		}
		_, err := c.createBoard(tx, p.ID, "beta", true, true)
		return err
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	b, err := c.SelectBoard(t.Context(), p.ID, "")
	if err != nil {
		t.Fatalf("SelectBoard: %v", err)
	}
	if b.Name != "beta" {
		t.Errorf("Name = %q, want beta (the one marked is_default)", b.Name)
	}
}

// TestSelectBoardNoDefaultAmongSeveralIsError is the design point the brief
// insists on: with several boards and no default, SelectBoard must error
// rather than guess, and the error message must name the available boards
// because an agent recovering from it has only the message to work with.
func TestSelectBoardNoDefaultAmongSeveralIsError(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if _, err := c.createBoard(tx, p.ID, "alpha", false, true); err != nil {
			return err
		}
		_, err := c.createBoard(tx, p.ID, "beta", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	_, err = c.SelectBoard(t.Context(), p.ID, "")
	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error = %v, want a *core.Error", err)
	}
	if te.Exit != 2 {
		t.Errorf("Exit = %d, want 2 (ErrUsage)", te.Exit)
	}
	if !strings.Contains(te.Msg, "alpha") || !strings.Contains(te.Msg, "beta") {
		t.Errorf("message %q must name both available boards", te.Msg)
	}
}

// TestSetDefaultBoardEnforcesOneDefault verifies that the invariant is
// enforced: exactly one board in a project has is_default = 1 after each
// SetDefaultBoard call. This is the guarantee the UI will depend on.
func TestSetDefaultBoardEnforcesOneDefault(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	// Create three boards: first is initially default
	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if _, err := c.createBoard(tx, p.ID, "alpha", true, true); err != nil {
			return err
		}
		if _, err := c.createBoard(tx, p.ID, "beta", false, true); err != nil {
			return err
		}
		_, err := c.createBoard(tx, p.ID, "gamma", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Set each board as default in turn and verify the invariant
	for _, boardName := range []string{"beta", "gamma", "alpha"} {
		_, err := c.SetDefaultBoard(t.Context(), p.ID, boardName)
		if err != nil {
			t.Fatalf("SetDefaultBoard(%q): %v", boardName, err)
		}

		// Verify exactly one board has is_default = 1
		var count int
		if err := c.db.Get(&count, `SELECT COUNT(*) FROM board WHERE project_id = ? AND is_default = 1`, p.ID); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("after SetDefaultBoard(%q), default count = %d, want 1", boardName, count)
		}

		// Verify the right board is default
		var defaultName string
		if err := c.db.Get(&defaultName, `SELECT name FROM board WHERE project_id = ? AND is_default = 1`, p.ID); err != nil {
			t.Fatal(err)
		}
		if defaultName != boardName {
			t.Errorf("after SetDefaultBoard(%q), default board = %q", boardName, defaultName)
		}
	}
}

// TestSetDefaultBoardUnknownNameIsNotFound tests that SetDefaultBoard returns
// ErrNotFound when the board name does not match, listing available boards.
func TestSetDefaultBoardUnknownNameIsNotFound(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if _, err := c.createBoard(tx, p.ID, "alpha", true, true); err != nil {
			return err
		}
		_, err := c.createBoard(tx, p.ID, "beta", false, true)
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = c.SetDefaultBoard(t.Context(), p.ID, "nope")
	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error = %v, want a *core.Error", err)
	}
	if te.Exit != 3 {
		t.Errorf("Exit = %d, want 3 (ErrNotFound)", te.Exit)
	}
	if !strings.Contains(te.Msg, "alpha") || !strings.Contains(te.Msg, "beta") {
		t.Errorf("message %q should list available boards", te.Msg)
	}
}
