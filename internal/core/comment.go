package core

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// Comment is an append-only record attached to a card. Comments do not bump card.version,
// so a displaced agent can leave a comment on a card it no longer holds, or on a card
// owned by someone else. They are the durable channel for handing off what was learned.
type Comment struct {
	ID        string `db:"id" json:"id"`
	CardID    string `db:"card_id" json:"card_id"`
	Actor     string `db:"actor" json:"actor"`
	BodyMD    string `db:"body_md" json:"body"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
}

// CreateComment appends a comment to a card. This is always permitted, even if the caller
// does not own the card. The comment is written to a separate table and never bumps card.version.
// Nor does it renew the caller's claim: RenewClaim and the claimant's own edit do that.
func (c *Core) CreateComment(ctx context.Context, cardID, body string) (Comment, error) {
	var comment Comment
	now := c.clock.NowMS()

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Verify the card exists.
		var projectID string
		if err := tx.Get(&projectID, `SELECT project_id FROM card WHERE id = ?`, cardID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("card_not_found", "card not found", "")
			}
			return err
		}

		if err := c.checkWrite(ctx, ProposedWrite{
			Op: "comment.create", Entity: "comment", EntityID: cardID, ProjectID: projectID,
			Fields: map[string]string{"body": body},
		}); err != nil {
			return err
		}

		commentID := NewID()
		comment = Comment{
			ID:        commentID,
			CardID:    cardID,
			Actor:     c.actor,
			BodyMD:    body,
			CreatedAt: now,
		}

		if _, err := tx.Exec(
			`INSERT INTO comment (id, card_id, actor, body_md, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			comment.ID, comment.CardID, comment.Actor, comment.BodyMD, comment.CreatedAt); err != nil {
			return err
		}

		// The comment row is the source of truth; do not copy its body into the
		// append-only event stream as a second permanent blob.
		if err := c.recordEvent(tx, "comment", comment.ID, "created", "", "", ""); err != nil {
			return err
		}

		return nil
	})

	return comment, err
}

// GetCommentsByCard returns all comments on a card, ordered by created_at.
func (c *Core) GetCommentsByCard(ctx context.Context, cardID string) ([]Comment, error) {
	var comments []Comment
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&comments, `SELECT * FROM comment WHERE card_id = ? ORDER BY created_at, id`,
			cardID)
	})
	return comments, err
}
