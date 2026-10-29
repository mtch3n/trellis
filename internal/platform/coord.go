package platform

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Notice struct {
	ID        uuid.UUID `db:"id" json:"id"`
	Card      string    `db:"card" json:"card"`
	Title     string    `db:"title" json:"title"`
	With      string    `db:"with" json:"with"`
	Words     []string  `db:"-" json:"words"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type noticeRow struct {
	Notice
	WordList string `db:"words"`
}

const coordActor = "system:coord"

const coordClose = `UPDATE coord_notice n SET state = 'closed', ended_at = now()
	WHERE n.state = 'open' AND EXISTS (
	  SELECT 1 FROM card c JOIN board_column col ON col.id = c.column_id
	  WHERE c.id IN (n.card_id, n.other_card_id)
	    AND (col.done OR c.archived_at IS NOT NULL OR c.deleted_at IS NOT NULL))`

const coordNotice = `WITH open_card AS (
	  SELECT c.id, c.project_id, b.owner_id, b.scope, c.title || ' ' || c.body AS text,
	         tsvector_to_array(to_tsvector('english', c.title || ' ' || c.body)) AS lex
	  FROM card c JOIN board b ON b.id = c.board_id JOIN board_column col ON col.id = c.column_id
	  WHERE NOT col.done AND c.archived_at IS NULL AND c.deleted_at IS NULL
	), pair AS (
	  SELECT a.project_id, a.id AS a_id, a.owner_id AS a_owner, a.text AS a_text,
	         x.id AS b_id, x.owner_id AS b_owner, x.text AS b_text,
	         ARRAY(SELECT unnest(a.lex) INTERSECT SELECT unnest(x.lex)) AS shared,
	         least(cardinality(a.lex), cardinality(x.lex)) AS smaller
	  FROM open_card a JOIN open_card x
	    ON x.project_id = a.project_id AND x.owner_id <> a.owner_id AND a.id < x.id
	  WHERE a.scope = 'personal' OR x.scope = 'personal'
	), hit AS (
	  SELECT * FROM pair WHERE cardinality(shared) >= 3 AND cardinality(shared) * 10 >= smaller * 3
	), side AS (
	  SELECT project_id, a_owner AS member_id, a_id AS card_id, b_owner AS other_member_id, b_id AS other_card_id, a_text AS text, shared FROM hit
	  UNION ALL
	  SELECT project_id, b_owner, b_id, a_owner, a_id, b_text, shared FROM hit
	)
	INSERT INTO coord_notice (project_id, member_id, card_id, other_member_id, other_card_id, words, created_by)
	SELECT project_id, member_id, card_id, other_member_id, other_card_id,
	       ARRAY(SELECT DISTINCT lower(w) FROM regexp_split_to_table(text, '[^[:alnum:]]+') w
	             WHERE tsvector_to_array(to_tsvector('english', w)) && shared ORDER BY 1),
	       $1
	FROM side
	ON CONFLICT (card_id, other_card_id) DO NOTHING`

func (p *Platform) RunCoordinator(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := p.Coordinate(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("coordinate cards", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (p *Platform) Coordinate(ctx context.Context) (int, error) {
	var written int64
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(coordClose); err != nil {
			return err
		}
		res, err := tx.Exec(coordNotice, coordActor)
		if err != nil {
			return err
		}
		written, err = res.RowsAffected()
		return err
	})
	return int(written), err
}

func (p *Platform) Notices(ctx context.Context, pr Principal, project string) ([]Notice, error) {
	var notices []Notice
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		notices, err = openNotices(tx, pr, proj)
		return err
	})
	return notices, err
}

func openNotices(tx *sqlx.Tx, pr Principal, proj projectAccess) ([]Notice, error) {
	var rows []noticeRow
	if err := tx.Select(&rows, `SELECT n.id, c.ref AS card, c.title, m.handle AS "with",
		array_to_string(n.words, ' ') AS words, n.created_at
		FROM coord_notice n JOIN card c ON c.id = n.card_id JOIN member m ON m.id = n.other_member_id
		WHERE n.project_id = $1 AND n.member_id = $2 AND n.state = 'open'
		ORDER BY n.created_at, n.id`, proj.ID, pr.MemberID); err != nil {
		return nil, err
	}
	notices := make([]Notice, len(rows))
	for i, r := range rows {
		notices[i] = r.Notice
		notices[i].Words = strings.Fields(r.WordList)
	}
	return notices, nil
}
