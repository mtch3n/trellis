package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Column is one lane of a board. Columns hang off a board (column_.board_id),
// not a project: once boards are per-project-configurable, two boards in the
// same project may want different column sets.
type Column struct {
	ID       string `db:"id" json:"id"`
	BoardID  string `db:"board_id" json:"-"`
	Name     string `db:"name" json:"name"`
	Position int    `db:"position" json:"position"`
	IsDone   bool   `db:"is_done" json:"is_done"`
}

// defaultColumns is the vocabulary seeded on board creation.
// is_done is an explicit flag, never a name match: once columns are
// configurable, a terminal column might be called "shipped", so matching the
// string "done" would be wrong.
var defaultColumns = []struct {
	Name   string
	IsDone bool
}{
	{"backlog", false},
	{"in-progress", false},
	{"review", false},
	{"done", true},
}

func (c *Core) seedDefaultColumns(tx *sqlx.Tx, boardID string) error {
	columns := defaultColumns
	if len(c.defaultColumns) > 0 {
		columns = make([]struct {
			Name   string
			IsDone bool
		}, len(c.defaultColumns))
		for i, name := range c.defaultColumns {
			columns[i] = struct {
				Name   string
				IsDone bool
			}{Name: name, IsDone: i == len(c.defaultColumns)-1}
		}
	}
	for i, col := range columns {
		if _, err := tx.Exec(
			`INSERT INTO column_ (id, board_id, name, position, is_done) VALUES (?, ?, ?, ?, ?)`,
			NewID(), boardID, col.Name, i, col.IsDone); err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) ListColumns(ctx context.Context, boardID string) ([]Column, error) {
	var cols []Column
	err := c.db.SelectContext(ctx, &cols,
		`SELECT * FROM column_ WHERE board_id = ? ORDER BY position`, boardID)
	return cols, err
}

// ColumnByName looks up a column by its exact name within a board. Absence is
// ErrNotFound, listing every valid name: an agent choosing a column has only
// that message to work from.
func (c *Core) ColumnByName(tx *sqlx.Tx, boardID, name string) (Column, error) {
	var col Column
	err := tx.Get(&col,
		`SELECT * FROM column_ WHERE board_id = ? AND name = ?`, boardID, name)
	if err == nil {
		return col, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Column{}, err
	}

	var names []string
	if err := tx.Select(&names,
		`SELECT name FROM column_ WHERE board_id = ? ORDER BY position`, boardID); err != nil {
		return Column{}, err
	}
	return Column{}, ErrNotFound("column_not_found",
		fmt.Sprintf("no column %q (have: %s)", name, strings.Join(names, ", ")),
		"trellis column ls")
}

// FirstColumn returns the column with the lowest position, the destination
// for a newly created card. A board with none is a usage error: it means the
// board was never seeded, not that the caller guessed wrong.
func (c *Core) FirstColumn(tx *sqlx.Tx, boardID string) (Column, error) {
	var col Column
	err := tx.Get(&col,
		`SELECT * FROM column_ WHERE board_id = ? ORDER BY position LIMIT 1`, boardID)
	if errors.Is(err, sql.ErrNoRows) {
		return Column{}, ErrUsage("no_columns",
			"this board has no columns",
			"trellis column add backlog")
	}
	return col, err
}

func (c *Core) AddColumn(ctx context.Context, boardID, name, after string, done bool) (Column, error) {
	var out Column
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM column_ WHERE board_id = ? AND name = ?`, boardID, name); err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict("column_exists", fmt.Sprintf("column %q already exists", name), "trellis column ls")
		}
		pos := 0
		if err := tx.Get(&pos, `SELECT COALESCE(MAX(position), -1) + 1 FROM column_ WHERE board_id = ?`, boardID); err != nil {
			return err
		}
		if after != "" {
			var p int
			if err := tx.Get(&p, `SELECT position FROM column_ WHERE board_id = ? AND name = ?`, boardID, after); err != nil {
				return ErrNotFound("column_not_found", "no column "+after, "trellis column ls")
			}
			pos = p + 1
			if _, err := tx.Exec(`UPDATE column_ SET position = position + 1 WHERE board_id = ? AND position >= ?`, boardID, pos); err != nil {
				return err
			}
		}
		out = Column{ID: NewID(), BoardID: boardID, Name: name, Position: pos, IsDone: done}
		_, err := tx.Exec(`INSERT INTO column_ (id, board_id, name, position, is_done) VALUES (?, ?, ?, ?, ?)`, out.ID, boardID, name, pos, done)
		return err
	})
	return out, err
}

func (c *Core) RenameColumn(ctx context.Context, boardID, from, to string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var id string
		if err := tx.Get(&id, `SELECT id FROM column_ WHERE board_id = ? AND name = ?`, boardID, from); err != nil {
			return ErrNotFound("column_not_found", "no column "+from, "trellis column ls")
		}
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM column_ WHERE board_id = ? AND name = ?`, boardID, to); err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict("column_exists", fmt.Sprintf("column %q already exists", to), "trellis column ls")
		}
		_, err := tx.Exec(`UPDATE column_ SET name = ? WHERE id = ?`, to, id)
		return err
	})
}

func (c *Core) MoveColumn(ctx context.Context, boardID, name, after string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var id string
		if err := tx.Get(&id, `SELECT id FROM column_ WHERE board_id = ? AND name = ?`, boardID, name); err != nil {
			return ErrNotFound("column_not_found", "no column "+name, "trellis column ls")
		}
		var target int
		if after == "" {
			target = 0
		} else if err := tx.Get(&target, `SELECT position + 1 FROM column_ WHERE board_id = ? AND name = ?`, boardID, after); err != nil {
			return ErrNotFound("column_not_found", "no column "+after, "trellis column ls")
		}
		var old int
		if err := tx.Get(&old, `SELECT position FROM column_ WHERE id = ?`, id); err != nil {
			return err
		}
		if old < target {
			target--
		}
		if old == target {
			return nil
		}
		if old < target {
			_, _ = tx.Exec(`UPDATE column_ SET position = position - 1 WHERE board_id = ? AND position > ? AND position <= ?`, boardID, old, target)
		} else {
			_, _ = tx.Exec(`UPDATE column_ SET position = position + 1 WHERE board_id = ? AND position >= ? AND position < ?`, boardID, target, old)
		}
		_, err := tx.Exec(`UPDATE column_ SET position = ? WHERE id = ?`, target, id)
		return err
	})
}

func (c *Core) DeleteColumn(ctx context.Context, boardID, name, moveTo string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var id string
		if err := tx.Get(&id, `SELECT id FROM column_ WHERE board_id = ? AND name = ?`, boardID, name); err != nil {
			return ErrNotFound("column_not_found", "no column "+name, "trellis column ls")
		}
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM card WHERE column_id = ?`, id); err != nil {
			return err
		}
		if n > 0 {
			if moveTo == "" {
				return ErrConflict("column_not_empty", fmt.Sprintf("column %q has %d cards", name, n), "trellis column rm "+name+" --move-cards-to <name>")
			}
			var target string
			if err := tx.Get(&target, `SELECT id FROM column_ WHERE board_id = ? AND name = ?`, boardID, moveTo); err != nil {
				return ErrNotFound("column_not_found", "no column "+moveTo, "trellis column ls")
			}
			if target == id {
				return ErrUsage("same_column", "cards must move to another column", "trellis column ls")
			}
			if _, err := tx.Exec(`UPDATE card SET column_id = ?, version = version + 1 WHERE column_id = ?`, target, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`DELETE FROM column_ WHERE id = ?`, id)
		return err
	})
}
