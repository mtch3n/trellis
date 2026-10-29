package platform

import (
	"context"
	"database/sql"
	"errors"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type ColumnCount struct {
	Column string `db:"name" json:"column"`
	Count  int    `db:"count" json:"count"`
}

type BoardCounts struct {
	Board   string        `json:"board"`
	Owner   string        `json:"owner"`
	Scope   string        `json:"scope"`
	Columns []ColumnCount `json:"columns"`
}

type Brief struct {
	Project string        `json:"project"`
	Boards  []BoardCounts `json:"boards"`
	Claimed []Card        `json:"claimed"`
	Next    *Card         `json:"next,omitempty"`
	Pins    []Pin         `json:"pins"`
	Notices []Notice      `json:"notices"`
}

func (p *Platform) Brief(ctx context.Context, pr Principal, project string) (Brief, error) {
	brief := Brief{Project: project, Boards: []BoardCounts{}, Claimed: []Card{}, Pins: []Pin{}, Notices: []Notice{}}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		brief.Project = proj.Key
		boards, err := visibleBoards(tx, pr, proj)
		if err != nil {
			return err
		}
		visible := map[uuid.UUID]bool{}
		for _, b := range boards {
			visible[b.ID] = true
			counts := BoardCounts{Board: b.Name, Owner: b.Owner, Scope: b.Scope, Columns: []ColumnCount{}}
			if err := tx.Select(&counts.Columns, `SELECT col.name, count(c.id) AS count
				FROM board_column col LEFT JOIN card c ON c.column_id = col.id
				WHERE col.board_id = $1 GROUP BY col.id, col.name, col.position ORDER BY col.position`, b.ID); err != nil {
				return err
			}
			brief.Boards = append(brief.Boards, counts)
		}
		if brief.Pins, err = briefPins(tx, pr, proj, boards); err != nil {
			return err
		}
		if pr.IsSystem() {
			return nil
		}
		if brief.Notices, err = openNotices(tx, pr, proj); err != nil {
			return err
		}
		var claimed []cardRow
		if err := tx.Select(&claimed, cardSelect+` WHERE c.project_id = $1 AND c.claimed_member_id = $2 AND c.claim_until > now()
			ORDER BY c.number`, proj.ID, pr.MemberID); err != nil {
			return err
		}
		for _, r := range claimed {
			if visible[r.BoardID] {
				brief.Claimed = append(brief.Claimed, r.Card)
			}
		}
		brief.Next, err = nextCard(tx, pr, proj)
		return err
	})
	return brief, err
}

func nextCard(tx *sqlx.Tx, pr Principal, proj projectAccess) (*Card, error) {
	b, err := findBoard(tx, pr, proj, "", OpRead, false)
	if IsCode(err, "board_not_found") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r cardRow
	err = tx.Get(&r, cardSelect+` WHERE c.board_id = $1 AND NOT col.done
		  AND (c.claim_until IS NULL OR c.claim_until <= now())
		  AND NOT EXISTS (
		    SELECT 1 FROM blocker bl
		    JOIN card bc ON bc.id = bl.blocker_id
		    JOIN board_column bcol ON bcol.id = bc.column_id
		    WHERE bl.card_id = c.id AND NOT bcol.done)
		ORDER BY col.position DESC, c.position
		LIMIT 1`, b.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r.Card, nil
}
