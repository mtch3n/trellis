package platform

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Pin struct {
	Slug      string    `db:"slug" json:"slug"`
	Title     string    `db:"title" json:"title"`
	Recap     string    `db:"recap" json:"recap"`
	Board     *string   `db:"board" json:"board,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type pinEntry struct {
	ID      uuid.UUID `db:"id"`
	Slug    string    `db:"slug"`
	Title   string    `db:"title"`
	Summary string    `db:"summary"`
	Body    string    `db:"body"`
	Private bool      `db:"private"`
}

const pinVisibleSQL = `e.deleted_at IS NULL AND (
	(e.scope = 'personal' AND e.project_id = $1 AND ($3 OR e.owner_id = $2))
	OR (e.scope = 'project' AND e.project_id = $1)
	OR (e.scope = 'global' AND e.org_id = $4))`

func pinVisibleArgs(pr Principal, proj projectAccess) []any {
	return []any{proj.ID, pr.MemberID, pr.IsSystem(), proj.OrgID}
}

func errEntryNotFound(ref string) *Error {
	return ErrNotFound("entry_not_found", "no entry "+ref+" in this project", "vault_list")
}

func findPinEntry(tx *sqlx.Tx, pr Principal, proj projectAccess, ref string) (pinEntry, error) {
	ref = strings.TrimSpace(ref)
	match := `e.slug = $5`
	var key any = ref
	if id, err := uuid.Parse(ref); err == nil {
		match, key = `e.id = $5`, id
	}
	var e pinEntry
	err := tx.Get(&e, `SELECT e.id, e.slug, e.title, e.summary, e.body, e.private FROM entry e
		WHERE `+match+` AND `+pinVisibleSQL+`
		ORDER BY CASE e.scope WHEN 'personal' THEN 0 WHEN 'project' THEN 1 ELSE 2 END LIMIT 1`,
		append(pinVisibleArgs(pr, proj), key)...)
	if errors.Is(err, sql.ErrNoRows) {
		return e, errEntryNotFound(ref)
	}
	return e, err
}

func firstParagraph(body string) string {
	for block := range strings.SplitSeq(strings.TrimSpace(body), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" || strings.HasPrefix(block, "#") || strings.HasPrefix(block, "<!--") {
			continue
		}
		return strings.Join(strings.Fields(block), " ")
	}
	return ""
}

func pinEvent(tx *sqlx.Tx, pr Principal, proj projectAccess, entryID uuid.UUID, kind string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO event (org_id, project_id, entry_id, kind, actor, member_id, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		proj.OrgID, proj.ID, entryID, kind, pr.Actor(), pr.MemberID, raw)
	return err
}

func (p *Platform) PinEntry(ctx context.Context, pr Principal, project, ref, recap string) (Pin, error) {
	var pin Pin
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		e, err := findPinEntry(tx, pr, proj, ref)
		if err != nil {
			return err
		}
		var stored *string
		if !e.Private {
			text := cmp.Or(strings.TrimSpace(recap), e.Summary, firstParagraph(e.Body))
			if text == "" {
				return ErrUsage("no_recap", "this entry has no summary to fall back on",
					"vault_share pin "+e.Slug+" with a recap an agent can act on")
			}
			stored = &text
		}
		if err := tx.Get(&pin.CreatedAt, `INSERT INTO pin (project_id, entry_id, recap, member_id, actor)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (project_id, entry_id, board_id) DO UPDATE SET recap = excluded.recap,
				member_id = excluded.member_id, actor = excluded.actor, created_at = now()
			RETURNING created_at`, proj.ID, e.ID, stored, pr.MemberID, pr.Actor()); err != nil {
			return err
		}
		pin.Slug, pin.Title = e.Slug, e.Title
		if stored != nil {
			pin.Recap = *stored
		}
		return pinEvent(tx, pr, proj, e.ID, "entry.pinned", map[string]any{"recap": pin.Recap})
	})
	return pin, err
}

func (p *Platform) UnpinEntry(ctx context.Context, pr Principal, project, ref string) error {
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		e, err := findPinEntry(tx, pr, proj, ref)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM pin WHERE project_id = $1 AND entry_id = $2 AND board_id IS NULL`, proj.ID, e.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound("not_pinned", e.Slug+" is not pinned in this project", "board_brief")
		}
		return pinEvent(tx, pr, proj, e.ID, "entry.unpinned", nil)
	})
}

func briefPins(tx *sqlx.Tx, pr Principal, proj projectAccess, boards []Board) ([]Pin, error) {
	var rows []struct {
		Pin
		BoardID *uuid.UUID `db:"board_id"`
	}
	if err := tx.Select(&rows, `SELECT e.slug, e.title, COALESCE(p.recap, e.summary) AS recap,
		b.name AS board, p.board_id, p.created_at
		FROM pin p JOIN entry e ON e.id = p.entry_id LEFT JOIN board b ON b.id = p.board_id
		WHERE p.project_id = $1 AND NOT e.private AND (e.verify_by IS NULL OR e.verify_by > now())
		  AND `+pinVisibleSQL+`
		ORDER BY p.created_at DESC, e.slug`, pinVisibleArgs(pr, proj)...); err != nil {
		return nil, err
	}
	visible := map[uuid.UUID]bool{}
	for _, b := range boards {
		visible[b.ID] = true
	}
	pins := []Pin{}
	for _, r := range rows {
		if r.BoardID == nil || visible[*r.BoardID] {
			pins = append(pins, r.Pin)
		}
	}
	return pins, nil
}
