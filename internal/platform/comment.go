package platform

import (
	"context"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Comment struct {
	ID        uuid.UUID `db:"id" json:"id"`
	CardID    uuid.UUID `db:"card_id" json:"-"`
	MemberID  uuid.UUID `db:"member_id" json:"-"`
	Actor     string    `db:"actor" json:"actor"`
	Body      string    `db:"body" json:"body"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

func (p *Platform) CommentCard(ctx context.Context, pr Principal, project, ref, body string) (Comment, error) {
	var c Comment
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		card, err := loadCard(tx, pr, proj, ref, OpWrite, false)
		if err != nil {
			return err
		}
		if strings.TrimSpace(body) == "" {
			return ErrUsage("body_required", "a comment needs a body", "card_write comment with a body")
		}
		if c, err = insertComment(tx, pr, card.ID, body); err != nil {
			return err
		}
		return recordEvent(tx, pr, proj, card.BoardID, &card.ID, "card.commented", nil)
	})
	return c, err
}

func insertComment(tx *sqlx.Tx, pr Principal, cardID uuid.UUID, body string) (Comment, error) {
	var c Comment
	err := tx.Get(&c, `INSERT INTO comment (card_id, member_id, actor, body) VALUES ($1, $2, $3, $4)
		RETURNING id, card_id, member_id, actor, body, created_at`, cardID, pr.MemberID, pr.Actor(), body)
	return c, err
}

func cardComments(tx *sqlx.Tx, cardID uuid.UUID) ([]Comment, error) {
	comments := []Comment{}
	err := tx.Select(&comments, `SELECT id, card_id, member_id, actor, body, created_at FROM comment
		WHERE card_id = $1 ORDER BY created_at, id`, cardID)
	return comments, err
}
