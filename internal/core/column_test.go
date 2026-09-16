package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

// seededProject inserts a project row DIRECTLY rather than calling
// createProject. These tests need a project to exist; they do not need
// createProject to be correct. Going through it would make this task's tests
// fail for the wrong reason whenever Task 7 has a defect, and would couple two
// tasks that have no design relationship.
func seededProject(t *testing.T, c *Core) Project {
	t.Helper()
	p := Project{ID: NewCardID(), Key: "XPSCTL", Name: "XPSCTL", CreatedAt: c.clock.NowMS()}
	_, err := c.db.Exec(
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.Key, p.Name, p.CreatedAt)
	if err != nil {
		t.Fatalf("seeding project: %v", err)
	}
	return p
}

// seededProject2 is seededProject's twin, for tests that need two distinct
// projects in the same run (e.g. cross-project scoping checks).
func seededProject2(t *testing.T, c *Core) Project {
	t.Helper()
	p := Project{ID: NewCardID(), Key: "OTHERPROJ", Name: "OTHERPROJ", CreatedAt: c.clock.NowMS()}
	_, err := c.db.Exec(
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.Key, p.Name, p.CreatedAt)
	if err != nil {
		t.Fatalf("seeding project: %v", err)
	}
	return p
}

// seededBoard creates a board (and its default columns) for p via the real
// createBoard implementation. Columns hang off a board, not a project
// (column_.board_id references board.id, not project.id — see
// internal/store/migrations/0001_init.sql), so column tests need a board to
// exist before they can seed or list anything.
func seededBoard(t *testing.T, c *Core, p Project) Board {
	t.Helper()
	var b Board
	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		var err error
		b, err = c.createBoard(tx, p.ID, "main", true, true)
		return err
	})
	if err != nil {
		t.Fatalf("createBoard: %v", err)
	}
	return b
}

func TestDefaultColumnsSeeded(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	cols, err := c.ListColumns(t.Context(), b.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}

	want := []string{"backlog", "in-progress", "review", "done"}
	if len(cols) != len(want) {
		t.Fatalf("got %d columns, want %d: %+v", len(cols), len(want), cols)
	}
	for i, name := range want {
		if cols[i].Name != name {
			t.Errorf("column %d = %q, want %q", i, cols[i].Name, name)
		}
		if cols[i].Position != i {
			t.Errorf("column %q position = %d, want %d", name, cols[i].Position, i)
		}
	}
	if !cols[3].IsDone {
		t.Error("the last default column must be marked is_done")
	}
	for _, col := range cols[:3] {
		if col.IsDone {
			t.Errorf("column %q must not be marked is_done", col.Name)
		}
	}
}

func TestColumnByNameUnknownListsValidNames(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		_, err := c.ColumnByName(tx, b.ID, "shipped")
		return err
	})
	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error = %v, want a *core.Error", err)
	}
	if te.Exit != 3 {
		t.Errorf("Exit = %d, want 3", te.Exit)
	}
	if !strings.Contains(te.Msg, "backlog") {
		t.Errorf("message %q should list the valid column names", te.Msg)
	}
}

func TestColumnByNameFound(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		col, err := c.ColumnByName(tx, b.ID, "review")
		if err != nil {
			return err
		}
		if col.Name != "review" || col.Position != 2 || col.IsDone {
			t.Errorf("ColumnByName(review) = %+v, want {review, 2, false}", col)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestFirstColumnIsLowestPosition(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		col, err := c.FirstColumn(tx, b.ID)
		if err != nil {
			return err
		}
		if col.Name != "backlog" || col.Position != 0 {
			t.Errorf("FirstColumn = %+v, want backlog at position 0", col)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestFirstColumnErrUsageWhenBoardHasNone(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)

	// A board with no columns at all: insert directly, bypassing createBoard's
	// seeding, so FirstColumn has nothing to return.
	boardID := NewCardID()
	_, err := c.db.Exec(
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		boardID, p.ID, "empty", "empty", 0, c.clock.NowMS())
	if err != nil {
		t.Fatalf("seeding board: %v", err)
	}

	err = c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		_, err := c.FirstColumn(tx, boardID)
		return err
	})
	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error = %v, want a *core.Error", err)
	}
	if te.Exit != 2 {
		t.Errorf("Exit = %d, want 2 (ErrUsage)", te.Exit)
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		in   string
		want Priority
		ok   bool
	}{
		{"urgent", PriorityUrgent, true},
		{"high", PriorityHigh, true},
		{"normal", PriorityNormal, true},
		{"low", PriorityLow, true},
		{"URGENT", PriorityUrgent, true},
		{"p0", PriorityUrgent, true},
		{"p3", PriorityLow, true},
		{"critical", 0, false},
		{"", 0, false},
	}
	for _, tc := range tests {
		got, err := ParsePriority(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("ParsePriority(%q) error = %v, want ok=%v", tc.in, err, tc.ok)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("ParsePriority(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPriorityRoundTrips(t *testing.T) {
	for _, p := range []Priority{PriorityUrgent, PriorityHigh, PriorityNormal, PriorityLow} {
		got, err := ParsePriority(p.String())
		if err != nil || got != p {
			t.Errorf("round trip of %v failed: %v, %v", p, got, err)
		}
	}
}
