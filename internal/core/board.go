package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/jmoiron/sqlx"
)

// Board is a named lane of work within a project. A project always has at
// least one board (created alongside it); additional boards are opt-in.
type Board struct {
	ID        string `db:"id" json:"id"`
	ProjectID string `db:"project_id" json:"project_id"`
	Name      string `db:"name" json:"name"`
	Slug      string `db:"slug" json:"slug"`
	IsDefault bool   `db:"is_default" json:"is_default"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
}

// slugify lowercases name, collapses every run of non-letter-non-digit
// characters into a single "-", and trims leading/trailing "-". The slug is
// the KB directory name and a URL segment (/p/<KEY>/b/<slug>), and it never
// changes on rename, so it must be a stable, filesystem-and-URL-safe
// derivation of the name at creation time only.
//
// "Letter" and "digit" use unicode.IsLetter/IsDigit rather than an ASCII
// a-z0-9 test: an ASCII-only test would discard every non-Latin character,
// silently slugifying a name like "重構" to "". A name with no letters or
// digits at all (e.g. "!!!###") still has nothing to build a slug from, so
// it falls back to the literal "board"; createBoard's existing collision
// loop then disambiguates repeats of that fallback the same way it
// disambiguates repeated ordinary names, by appending "-2", "-3", and so on.
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "board"
	}
	return s
}

// createBoard inserts a board row, slugifying name and disambiguating
// collisions within the project by appending "-2", "-3", and so on. It records
// the creation event and optionally seeds default columns, all in the caller's
// transaction.
func (c *Core) createBoard(tx *sqlx.Tx, projectID, name string, isDefault, seedColumns bool) (Board, error) {
	base := slugify(name)
	slug := base
	for n := 2; ; n++ {
		var taken int
		if err := tx.Get(&taken,
			`SELECT count(*) FROM board WHERE project_id = ? AND slug = ?`, projectID, slug); err != nil {
			return Board{}, err
		}
		if taken == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}

	b := Board{
		ID:        NewCardID(),
		ProjectID: projectID,
		Name:      name,
		Slug:      slug,
		IsDefault: isDefault,
		CreatedAt: c.clock.NowMS(),
	}
	if _, err := tx.Exec(
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		b.ID, b.ProjectID, b.Name, b.Slug, b.IsDefault, b.CreatedAt); err != nil {
		return Board{}, err
	}
	if err := c.recordEvent(tx, "board", b.ID, "created", "", "", b.Name); err != nil {
		return Board{}, err
	}
	if seedColumns {
		if err := c.seedDefaultColumns(tx, b.ID); err != nil {
			return Board{}, err
		}
	}
	return b, nil
}

func (c *Core) ListBoards(ctx context.Context, projectID string) ([]Board, error) {
	var boards []Board
	err := c.db.SelectContext(ctx, &boards,
		`SELECT * FROM board WHERE project_id = ? ORDER BY created_at`, projectID)
	return boards, err
}

// BoardCardCounts returns a map from board ID to card count for the project.
func (c *Core) BoardCardCounts(ctx context.Context, projectID string) (map[string]int, error) {
	type result struct {
		BoardID string `db:"board_id"`
		Count   int    `db:"count"`
	}
	var rows []result
	err := c.db.SelectContext(ctx, &rows,
		`SELECT board_id, COUNT(*) as count FROM card WHERE project_id = ? GROUP BY board_id`, projectID)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, r := range rows {
		counts[r.BoardID] = r.Count
	}
	return counts, nil
}

// SelectBoard resolves which board an operation acts on.
//
// requested non-empty selects by name; a miss is ErrNotFound listing the
// available boards. Otherwise: a project with exactly one board has an
// implicit default. Among several boards, the one marked is_default wins.
// With several boards and none marked default, guessing would silently act
// on the wrong one, so this returns ErrUsage instead — and names the
// available boards in the message, because an agent recovering from the
// error has only that message to work with.
func (c *Core) SelectBoard(ctx context.Context, projectID, requested string) (Board, error) {
	boards, err := c.ListBoards(ctx, projectID)
	if err != nil {
		return Board{}, err
	}

	if requested != "" {
		for _, b := range boards {
			if b.Name == requested {
				return b, nil
			}
		}
		return Board{}, ErrNotFound("unknown_board",
			fmt.Sprintf("no board %q (have: %s)", requested, strings.Join(boardNames(boards), ", ")),
			"trellis board ls")
	}

	if len(boards) == 1 {
		return boards[0], nil
	}
	for _, b := range boards {
		if b.IsDefault {
			return b, nil
		}
	}

	return Board{}, ErrUsage("no_default_board",
		fmt.Sprintf("project has %d boards and no default: %s", len(boards), strings.Join(boardNames(boards), ", ")),
		"trellis board default <name>")
}

func boardNames(boards []Board) []string {
	names := make([]string, len(boards))
	for i, b := range boards {
		names[i] = b.Name
	}
	return names
}

// CreateBoard adds a board to an existing project. It is never the default --
// the first board of a project is created by createProject, which marks that
// one. seedColumns controls whether the board is seeded with the default column
// set (backlog, in-progress, review, done).
func (c *Core) CreateBoard(ctx context.Context, projectID, name string, seedColumns bool) (Board, error) {
	var board Board
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		board, err = c.createBoard(tx, projectID, name, false, seedColumns)
		return err
	})
	return board, err
}

// SetDefaultBoard makes one board the project's default, clearing the flag on
// every other board in the same transaction. A project has exactly zero or one
// default board; that invariant lives here rather than in a command handler so
// every caller -- CLI, and the web UI in P3 -- is bound by it. Returns
// ErrNotFound listing the available boards when the name does not match.
func (c *Core) SetDefaultBoard(ctx context.Context, projectID, name string) (Board, error) {
	var board Board
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Find the board by name
		boards, err := c.listBoardsTx(tx, projectID)
		if err != nil {
			return err
		}

		var found bool
		for _, b := range boards {
			if b.Name == name {
				board = b
				found = true
				break
			}
		}
		if !found {
			return ErrNotFound("unknown_board",
				fmt.Sprintf("no board %q (have: %s)", name, strings.Join(boardNames(boards), ", ")),
				"trellis board ls")
		}

		// Clear is_default on all other boards
		if _, err := tx.Exec(
			`UPDATE board SET is_default = 0 WHERE project_id = ? AND id != ?`,
			projectID, board.ID); err != nil {
			return err
		}

		// Set is_default on the target board
		if _, err := tx.Exec(
			`UPDATE board SET is_default = 1 WHERE id = ?`,
			board.ID); err != nil {
			return err
		}

		// Record event
		return c.recordEvent(tx, "board", board.ID, "default", "", "", "")
	})
	return board, err
}

// RenameBoard changes a board's display name while preserving its URL slug.
func (c *Core) RenameBoard(ctx context.Context, projectID, from, to string) (Board, error) {
	var board Board
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		board, err = c.boardByName(tx, projectID, from)
		if err != nil {
			return err
		}
		var existing string
		if err := tx.Get(&existing, `SELECT id FROM board WHERE project_id = ? AND name = ?`, projectID, to); err == nil {
			return ErrConflict("board_exists", fmt.Sprintf("board %q already exists", to), "trellis board ls")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(`UPDATE board SET name = ? WHERE id = ?`, to, board.ID); err != nil {
			return err
		}
		old := board.Name
		board.Name = to
		return c.recordEvent(tx, "board", board.ID, "renamed", "name", old, to)
	})
	return board, err
}

// DeleteBoard removes a board. Non-empty boards require force; a project must
// retain one board. Deleting the default promotes the oldest remaining board.
func (c *Core) DeleteBoard(ctx context.Context, projectID, name string, force bool) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		board, err := c.boardByName(tx, projectID, name)
		if err != nil {
			return err
		}
		var count, boards int
		if err := tx.Get(&count, `SELECT COUNT(*) FROM card WHERE board_id = ?`, board.ID); err != nil {
			return err
		}
		if count > 0 && !force {
			return ErrConflict("board_not_empty", fmt.Sprintf("board %q has %d cards", name, count), "trellis board rm "+name+" --force")
		}
		if err := tx.Get(&boards, `SELECT COUNT(*) FROM board WHERE project_id = ?`, projectID); err != nil {
			return err
		}
		if boards <= 1 {
			return ErrConflict("last_board", "a project must retain one board", "trellis board new --name <name>")
		}
		if force {
			if _, err := tx.Exec(`DELETE FROM link WHERE (from_type = 'card' AND from_id IN (SELECT id FROM card WHERE board_id = ?)) OR (to_type = 'card' AND to_id IN (SELECT id FROM card WHERE board_id = ?))`, board.ID, board.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM board WHERE id = ?`, board.ID); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "board", board.ID, "deleted", "", board.Name, ""); err != nil {
			return err
		}
		if board.IsDefault {
			var next Board
			if err := tx.Get(&next, `SELECT * FROM board WHERE project_id = ? ORDER BY created_at LIMIT 1`, projectID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE board SET is_default = 1 WHERE id = ?`, next.ID); err != nil {
				return err
			}
			if err := c.recordEvent(tx, "board", next.ID, "default", "", "", ""); err != nil {
				return err
			}
		}
		return nil
	})
}

// listBoardsTx is like ListBoards but operates within a transaction
func (c *Core) listBoardsTx(tx *sqlx.Tx, projectID string) ([]Board, error) {
	var boards []Board
	err := tx.Select(&boards,
		`SELECT * FROM board WHERE project_id = ? ORDER BY created_at`, projectID)
	return boards, err
}

// boardByName resolves a board within a transaction, for callers that already
// hold one.
func (c *Core) boardByName(tx *sqlx.Tx, projectID, name string) (Board, error) {
	var b Board
	err := tx.Get(&b, `SELECT * FROM board WHERE project_id = ? AND name = ?`, projectID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound("board_not_found", "no board "+name+" in this project", "trellis board ls")
	}
	return b, err
}

// BoardBySlug finds a board by slug, which is how a pin names one.
func (c *Core) BoardBySlug(ctx context.Context, projectID, slug string) (Board, error) {
	return boardBySlug(ctx, c.db, projectID, slug)
}

// boardBySlug works on the database or inside a caller's transaction. A miss
// lists the slugs that do exist, because a pin can only be fixed by naming one
// of them or by creating the board.
func boardBySlug(ctx context.Context, q sqlx.QueryerContext, projectID, slug string) (Board, error) {
	var b Board
	err := sqlx.GetContext(ctx, q, &b, `SELECT * FROM board WHERE project_id = ? AND slug = ?`, projectID, slug)
	if !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	var slugs []string
	if err := sqlx.SelectContext(ctx, q, &slugs,
		`SELECT slug FROM board WHERE project_id = ? ORDER BY created_at`, projectID); err != nil {
		return b, err
	}
	return b, ErrNotFound("unknown_board",
		fmt.Sprintf("no board with slug %q (have: %s)", slug, strings.Join(slugs, ", ")),
		"trellis board new --name <name>")
}
