package core

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// Note is an append-only record attached to a card. Notes do not bump card.version,
// so a displaced agent can leave a note on a card it no longer holds, or on a card
// owned by someone else. They are the durable channel for handing off what was learned.
type Note struct {
	ID        string `db:"id" json:"id"`
	CardID    string `db:"card_id" json:"card_id"`
	Actor     string `db:"actor" json:"actor"`
	BodyMD    string `db:"body_md" json:"body"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
}

// CreateNote appends a note to a card. This is always permitted, even if the caller
// does not own the card. The note is written to a separate table and never bumps card.version.
func (c *Core) CreateNote(ctx context.Context, cardID, body string) (Note, error) {
	var note Note
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
			Op: "note.create", EntityType: "note", EntityID: cardID, ProjectID: projectID,
			Fields: map[string]string{"body": body},
		}); err != nil {
			return err
		}

		noteID := NewCardID() // Reuse card ID generation; both are uuids v7
		note = Note{
			ID:        noteID,
			CardID:    cardID,
			Actor:     c.actor,
			BodyMD:    body,
			CreatedAt: now,
		}

		if _, err := tx.Exec(
			`INSERT INTO note (id, card_id, actor, body_md, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			note.ID, note.CardID, note.Actor, note.BodyMD, note.CreatedAt); err != nil {
			return err
		}

		// The note row is the source of truth; do not copy its body into the
		// append-only event stream as a second permanent blob.
		if err := c.recordEvent(tx, "note", note.ID, "created", "", "", ""); err != nil {
			return err
		}

		return nil
	})

	return note, err
}

// GetNotesByCard returns all notes on a card, ordered by created_at.
func (c *Core) GetNotesByCard(ctx context.Context, cardID string) ([]Note, error) {
	var notes []Note
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&notes, `SELECT * FROM note WHERE card_id = ? ORDER BY created_at`,
			cardID)
	})
	return notes, err
}
