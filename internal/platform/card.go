package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Card struct {
	ID         uuid.UUID  `db:"id" json:"id"`
	ProjectID  uuid.UUID  `db:"project_id" json:"-"`
	BoardID    uuid.UUID  `db:"board_id" json:"board_id"`
	Board      string     `db:"board" json:"board"`
	Ref        string     `db:"ref" json:"ref"`
	Number     int64      `db:"number" json:"-"`
	Title      string     `db:"title" json:"title"`
	Body       string     `db:"body" json:"body"`
	Column     string     `db:"column" json:"column"`
	Done       bool       `db:"done" json:"done"`
	Position   float64    `db:"position" json:"-"`
	Version    int64      `db:"version" json:"version"`
	ClaimedBy  *string    `db:"claimed_by" json:"claimed_by,omitempty"`
	ClaimUntil *time.Time `db:"claim_until" json:"claim_until,omitempty"`
	CreatedBy  string     `db:"created_by" json:"created_by"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at" json:"updated_at"`
	BlockedBy  []string   `db:"-" json:"blocked_by,omitempty"`
	Comments   []Comment  `db:"-" json:"comments,omitempty"`
	Events     []Event    `db:"-" json:"events,omitempty"`
	Artifacts  []Artifact `db:"-" json:"artifacts,omitempty"`
}

type NewCard struct {
	Board  string
	Title  string
	Body   string
	Column string
}

type CardEdit struct {
	Title     *string
	Body      *string
	IfVersion int64
}

type cardRow struct {
	Card
	OwnerID   uuid.UUID `db:"owner_id"`
	Scope     string    `db:"scope"`
	ClaimLive bool      `db:"claim_live"`
}

func (r cardRow) target() Target {
	return Target{OwnerID: r.OwnerID, Scope: r.Scope}
}

const cardSelect = `SELECT c.id, c.project_id, c.board_id, b.name AS board, c.ref, c.number, c.title, c.body,
	col.name AS "column", col.done, c.position, c.version, c.claimed_by, c.claim_until, c.created_by,
	c.created_at, c.updated_at, b.owner_id, b.scope, COALESCE(c.claim_until > now(), false) AS claim_live
	FROM card c JOIN board b ON b.id = c.board_id JOIN board_column col ON col.id = c.column_id`

func errCardNotFound(ref string) *Error {
	return ErrNotFound("card_not_found", "no card "+ref+" in this project", "card_list")
}

func loadCard(tx *sqlx.Tx, pr Principal, proj projectAccess, ref string, op Op, lock bool) (cardRow, error) {
	ref = strings.ToUpper(strings.TrimSpace(ref))
	q := cardSelect + ` WHERE c.project_id = $1 AND c.ref = $2`
	if lock {
		q += ` FOR UPDATE OF c`
	}
	var r cardRow
	err := tx.Get(&r, q, proj.ID, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errCardNotFound(ref)
	}
	if err != nil {
		return r, err
	}
	return r, proj.decide(pr, r.target(), op, errCardNotFound(ref))
}

func getCard(tx *sqlx.Tx, id uuid.UUID) (Card, error) {
	var r cardRow
	err := tx.Get(&r, cardSelect+` WHERE c.id = $1`, id)
	return r.Card, err
}

func checkClaim(r cardRow, pr Principal) error {
	if r.ClaimedBy == nil || *r.ClaimedBy == pr.Actor() || !r.ClaimLive {
		return nil
	}
	return &Error{
		Kind:   KindConflict,
		Code:   "contention",
		Msg:    fmt.Sprintf("%s is claimed by %s until %s", r.Ref, *r.ClaimedBy, r.ClaimUntil.Format(time.RFC3339)),
		Fix:    "card_claim next for another card, or card_claim claim with steal and a reason",
		Detail: map[string]any{"claimed_by": *r.ClaimedBy, "claim_until": *r.ClaimUntil},
	}
}

type column struct {
	ID   uuid.UUID `db:"id"`
	Name string    `db:"name"`
	Done bool      `db:"done"`
}

func findColumn(tx *sqlx.Tx, boardID uuid.UUID, name string) (column, error) {
	var cols []column
	if err := tx.Select(&cols, `SELECT id, name, done FROM board_column WHERE board_id = $1 ORDER BY position`, boardID); err != nil {
		return column{}, err
	}
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		if name == "" || c.Name == name {
			return c, nil
		}
		names = append(names, c.Name)
	}
	return column{}, ErrNotFound("column_not_found", "no column "+name+" on this board",
		"use one of: "+strings.Join(names, ", "))
}

func (p *Platform) CreateCard(ctx context.Context, pr Principal, project string, in NewCard) (Card, error) {
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		board, err := findBoard(tx, pr, proj, in.Board, OpWrite, false)
		if err != nil {
			return err
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return ErrUsage("title_required", "a card needs a title", "card_write new with a title")
		}
		col, err := findColumn(tx, board.ID, in.Column)
		if err != nil {
			return err
		}
		var n int64
		if err := tx.Get(&n, `UPDATE project SET card_counter = card_counter + 1 WHERE id = $1 RETURNING card_counter`, proj.ID); err != nil {
			return err
		}
		ref := proj.Key + "-" + strconv.FormatInt(n, 10)
		var id uuid.UUID
		if err := tx.Get(&id, `INSERT INTO card (project_id, board_id, column_id, number, ref, title, body, position, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7,
			        COALESCE((SELECT max(position) FROM card WHERE column_id = $3), 0) + 1, $8)
			RETURNING id`,
			proj.ID, board.ID, col.ID, n, ref, title, in.Body, pr.Actor()); err != nil {
			return err
		}
		if err := recordEvent(tx, pr, proj, board.ID, &id, "card.created", map[string]any{"ref": ref}); err != nil {
			return err
		}
		card, err = getCard(tx, id)
		return err
	})
	return card, err
}

func (p *Platform) ShowCard(ctx context.Context, pr Principal, project, ref string) (Card, error) {
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		r, err := loadCard(tx, pr, proj, ref, OpRead, false)
		if err != nil {
			return err
		}
		card = r.Card
		if card.BlockedBy, err = visibleBlockers(tx, pr, proj, card.ID); err != nil {
			return err
		}
		if card.Comments, err = cardComments(tx, card.ID); err != nil {
			return err
		}
		if card.Artifacts, err = cardArtifacts(tx, pr, proj, card.ID); err != nil {
			return err
		}
		card.Events, err = cardEvents(tx, card.ID)
		return err
	})
	return card, err
}

func visibleBlockers(tx *sqlx.Tx, pr Principal, proj projectAccess, cardID uuid.UUID) ([]string, error) {
	var rows []struct {
		Ref     string    `db:"ref"`
		OwnerID uuid.UUID `db:"owner_id"`
		Scope   string    `db:"scope"`
	}
	if err := tx.Select(&rows, `SELECT c.ref, b.owner_id, b.scope FROM blocker bl
		JOIN card c ON c.id = bl.blocker_id JOIN board b ON b.id = c.board_id
		WHERE bl.card_id = $1 ORDER BY c.number`, cardID); err != nil {
		return nil, err
	}
	refs := []string{}
	for _, r := range rows {
		if proj.verdict(pr, Target{OwnerID: r.OwnerID, Scope: r.Scope}, OpRead) == Allow {
			refs = append(refs, r.Ref)
		}
	}
	return refs, nil
}

func (p *Platform) ListCards(ctx context.Context, pr Principal, project, board string) ([]Card, error) {
	cards := []Card{}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		b, err := findBoard(tx, pr, proj, board, OpRead, false)
		if err != nil {
			return err
		}
		var rows []cardRow
		if err := tx.Select(&rows, cardSelect+` WHERE c.board_id = $1 ORDER BY col.position, c.position`, b.ID); err != nil {
			return err
		}
		for _, r := range rows {
			cards = append(cards, r.Card)
		}
		return nil
	})
	return cards, err
}

func (p *Platform) EditCard(ctx context.Context, pr Principal, project, ref string, e CardEdit) (Card, error) {
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		r, err := loadCard(tx, pr, proj, ref, OpWrite, true)
		if err != nil {
			return err
		}
		if err := checkClaim(r, pr); err != nil {
			return err
		}
		if e.Title == nil && e.Body == nil {
			return ErrUsage("nothing_to_edit", "an edit needs a title or a body", "card_write edit with a title or a body")
		}
		if e.IfVersion == 0 {
			return ErrUsage("version_required", "an edit replaces the whole field and needs the version you read",
				fmt.Sprintf("card_show %s, then pass if_version %d", r.Ref, r.Version))
		}
		if e.IfVersion != r.Version {
			return ErrConflict("conflict", fmt.Sprintf("%s changed since you read it (you: v%d, now: v%d)", r.Ref, e.IfVersion, r.Version),
				"card_show "+r.Ref)
		}
		var fields []string
		if e.Title != nil {
			t := strings.TrimSpace(*e.Title)
			if t == "" {
				return ErrUsage("title_required", "a card needs a title", "card_write edit with a title")
			}
			e.Title = &t
			fields = append(fields, "title")
		}
		if e.Body != nil {
			fields = append(fields, "body")
		}
		if _, err := tx.Exec(`UPDATE card SET title = COALESCE($2, title), body = COALESCE($3, body),
			version = version + 1, updated_at = now() WHERE id = $1`, r.ID, e.Title, e.Body); err != nil {
			return err
		}
		if err := recordEvent(tx, pr, proj, r.BoardID, &r.ID, "card.edited", map[string]any{"fields": fields}); err != nil {
			return err
		}
		card, err = getCard(tx, r.ID)
		return err
	})
	return card, err
}

func (p *Platform) MoveCard(ctx context.Context, pr Principal, project, ref, to string) (Card, error) {
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		r, err := loadCard(tx, pr, proj, ref, OpWrite, true)
		if err != nil {
			return err
		}
		if err := checkClaim(r, pr); err != nil {
			return err
		}
		if strings.TrimSpace(to) == "" {
			return ErrUsage("column_required", "a move needs a column", "card_write move with a column")
		}
		col, err := findColumn(tx, r.BoardID, to)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE card SET column_id = $2,
			position = COALESCE((SELECT max(position) FROM card WHERE column_id = $2), 0) + 1,
			claimed_by = CASE WHEN $3 THEN NULL ELSE claimed_by END,
			claimed_member_id = CASE WHEN $3 THEN NULL ELSE claimed_member_id END,
			claim_until = CASE WHEN $3 THEN NULL ELSE claim_until END,
			version = version + 1, updated_at = now() WHERE id = $1`, r.ID, col.ID, col.Done); err != nil {
			return err
		}
		if err := recordEvent(tx, pr, proj, r.BoardID, &r.ID, "card.moved", map[string]any{"from": r.Column, "to": col.Name}); err != nil {
			return err
		}
		card, err = getCard(tx, r.ID)
		return err
	})
	return card, err
}

func (p *Platform) BlockCard(ctx context.Context, pr Principal, project, ref, blocker string) (Card, error) {
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		r, err := loadCard(tx, pr, proj, ref, OpWrite, true)
		if err != nil {
			return err
		}
		b, err := loadCard(tx, pr, proj, blocker, OpRead, false)
		if err != nil {
			return err
		}
		if b.ID == r.ID {
			return ErrUsage("self_block", r.Ref+" cannot block itself", "card_write block with another card")
		}
		if _, err := tx.Exec(`INSERT INTO blocker (card_id, blocker_id, member_id, actor) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, r.ID, b.ID, pr.MemberID, pr.Actor()); err != nil {
			return err
		}
		if err := recordEvent(tx, pr, proj, r.BoardID, &r.ID, "card.blocked", map[string]any{"blocker": b.Ref}); err != nil {
			return err
		}
		card, err = getCard(tx, r.ID)
		return err
	})
	return card, err
}
