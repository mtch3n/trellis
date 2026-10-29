package platform

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

const (
	ScopePersonal = "personal"
	ScopeProject  = "project"
)

type Board struct {
	ID        uuid.UUID `db:"id" json:"id"`
	ProjectID uuid.UUID `db:"project_id" json:"-"`
	OwnerID   uuid.UUID `db:"owner_id" json:"-"`
	Owner     string    `db:"owner" json:"owner"`
	Name      string    `db:"name" json:"name"`
	Scope     string    `db:"scope" json:"scope"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

func (b Board) target() Target {
	return Target{OwnerID: b.OwnerID, Scope: b.Scope}
}

var defaultColumns = []struct {
	name string
	done bool
}{
	{"backlog", false},
	{"in-progress", false},
	{"review", false},
	{"done", true},
}

const boardSelect = `SELECT b.id, b.project_id, b.owner_id, m.handle AS owner, b.name, b.scope, b.created_at, b.updated_at
	FROM board b JOIN member m ON m.id = b.owner_id`

func errBoardNotFound(name string) *Error {
	return ErrNotFound("board_not_found", "no board "+name+" in this project", "board_list")
}

func createBoard(tx *sqlx.Tx, projectID, ownerID uuid.UUID, name string) (Board, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Board{}, ErrUsage("name_required", "a board needs a name", "board_write new with a name")
	}
	var id uuid.UUID
	err := tx.Get(&id, `INSERT INTO board (project_id, owner_id, name) VALUES ($1, $2, $3) RETURNING id`,
		projectID, ownerID, name)
	if isUniqueViolation(err) {
		return Board{}, ErrConflict("board_exists", "you already have a board "+name+" in this project", "board_list")
	}
	if err != nil {
		return Board{}, err
	}
	for i, c := range defaultColumns {
		if _, err := tx.Exec(`INSERT INTO board_column (board_id, name, position, done) VALUES ($1, $2, $3, $4)`,
			id, c.name, i, c.done); err != nil {
			return Board{}, err
		}
	}
	var b Board
	err = tx.Get(&b, boardSelect+` WHERE b.id = $1`, id)
	return b, err
}

func isUniqueViolation(err error) bool {
	e, ok := errors.AsType[*pgconn.PgError](err)
	return ok && e.Code == "23505"
}

func visibleBoards(tx *sqlx.Tx, pr Principal, proj projectAccess) ([]Board, error) {
	var all []Board
	if err := tx.Select(&all, boardSelect+` WHERE b.project_id = $1 ORDER BY b.name, m.handle`, proj.ID); err != nil {
		return nil, err
	}
	boards := []Board{}
	for _, b := range all {
		if proj.verdict(pr, b.target(), OpRead) == Allow {
			boards = append(boards, b)
		}
	}
	return boards, nil
}

func findBoard(tx *sqlx.Tx, pr Principal, proj projectAccess, name string, op Op, lock bool) (Board, error) {
	q, args := boardSelect+` WHERE b.project_id = $1 AND (b.name = $2 OR b.id::text = $2)`, []any{proj.ID, name}
	if name == "" {
		q, args = boardSelect+` WHERE b.project_id = $1 AND b.owner_id = $2 AND b.name = $3`, []any{proj.ID, pr.MemberID, pr.Handle}
	}
	if lock {
		q += ` FOR UPDATE OF b`
	}
	var all []Board
	if err := tx.Select(&all, q, args...); err != nil {
		return Board{}, err
	}
	label := cmp.Or(name, pr.Handle)
	var seen []Board
	for _, b := range all {
		if proj.verdict(pr, b.target(), OpRead) != Allow {
			continue
		}
		if b.OwnerID == pr.MemberID {
			seen = []Board{b}
			break
		}
		seen = append(seen, b)
	}
	switch len(seen) {
	case 0:
		return Board{}, errBoardNotFound(label)
	case 1:
		return seen[0], proj.decide(pr, seen[0].target(), op, errBoardNotFound(label))
	default:
		return Board{}, ErrUsage("ambiguous_board", "more than one board you see is named "+name, "board_list, then name the board by its id")
	}
}

func (p *Platform) ListBoards(ctx context.Context, pr Principal, project string) ([]Board, error) {
	var boards []Board
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		boards, err = visibleBoards(tx, pr, proj)
		return err
	})
	return boards, err
}

func (p *Platform) NewBoard(ctx context.Context, pr Principal, project, name string) (Board, error) {
	var b Board
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		if b, err = createBoard(tx, proj.ID, pr.MemberID, name); err != nil {
			return err
		}
		return recordEvent(tx, pr, proj, b.ID, nil, "board.created", map[string]any{"name": b.Name})
	})
	return b, err
}

func (p *Platform) RenameBoard(ctx context.Context, pr Principal, project, board, name string) (Board, error) {
	name = strings.TrimSpace(name)
	return p.changeBoard(ctx, pr, project, board, func(tx *sqlx.Tx, b Board) (string, map[string]any, error) {
		if name == "" {
			return "", nil, ErrUsage("name_required", "a board needs a name", "board_write rename with a name")
		}
		_, err := tx.Exec(`UPDATE board SET name = $2, updated_at = now() WHERE id = $1`, b.ID, name)
		if isUniqueViolation(err) {
			return "", nil, ErrConflict("board_exists", "you already have a board "+name+" in this project", "board_list")
		}
		return "board.renamed", map[string]any{"from": b.Name, "to": name}, err
	})
}

func (p *Platform) ShareBoard(ctx context.Context, pr Principal, project, board string) (Board, error) {
	return p.setScope(ctx, pr, project, board, ScopeProject, "board.shared")
}

func (p *Platform) UnshareBoard(ctx context.Context, pr Principal, project, board string) (Board, error) {
	return p.setScope(ctx, pr, project, board, ScopePersonal, "board.unshared")
}

func (p *Platform) setScope(ctx context.Context, pr Principal, project, board, scope, kind string) (Board, error) {
	return p.changeBoard(ctx, pr, project, board, func(tx *sqlx.Tx, b Board) (string, map[string]any, error) {
		_, err := tx.Exec(`UPDATE board SET scope = $2, updated_at = now() WHERE id = $1`, b.ID, scope)
		return kind, nil, err
	})
}

func (p *Platform) changeBoard(ctx context.Context, pr Principal, project, board string,
	change func(tx *sqlx.Tx, b Board) (string, map[string]any, error)) (Board, error) {
	var b Board
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if b, err = findBoard(tx, pr, proj, board, OpOwn, true); err != nil {
			return err
		}
		kind, data, err := change(tx, b)
		if err != nil {
			return err
		}
		if err := recordEvent(tx, pr, proj, b.ID, nil, kind, data); err != nil {
			return err
		}
		err = tx.Get(&b, boardSelect+` WHERE b.id = $1`, b.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return errBoardNotFound(board)
		}
		return err
	})
	return b, err
}
