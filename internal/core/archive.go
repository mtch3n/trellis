package core

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"
)

// ArchiveCard hides a card from the default listing without deleting it.
// Archiving releases any claim: an archived card is not work in flight.
func (c *Core) ArchiveCard(ctx context.Context, projectID string, ref CardRef) (Card, error) {
	var card Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardClaim(card); err != nil {
			return err
		}
		if card.ArchivedAt != nil {
			return nil
		}
		now := c.clock.NowMS()
		if _, err := tx.Exec(
			`UPDATE card SET archived_at = ?, claimed_by = NULL, claim_until = NULL,
			                 version = version + 1, updated_at = ? WHERE id = ?`,
			now, now, card.ID); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "card", card.ID, "archived", "", "", ""); err != nil {
			return err
		}
		return c.loadCard(tx, projectID, ref, &card)
	})
	return card, err
}

// RestoreCard returns an archived or trashed card to the board.
func (c *Core) RestoreCard(ctx context.Context, projectID string, ref CardRef) (Card, error) {
	var card Card
	var trashID string
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			if e, ok := errors.AsType[*Error](err); ok && e.Code == "card_not_found" {
				it, terr := c.findTrashedCard(tx, projectID, ref)
				if terr != nil {
					return err
				}
				trashID = it.ID
				return nil
			}
			return err
		}
		if err := c.checkCardClaim(card); err != nil {
			return err
		}
		if card.ArchivedAt == nil {
			return nil
		}
		if _, err := tx.Exec(
			`UPDATE card SET archived_at = NULL, version = version + 1, updated_at = ? WHERE id = ?`,
			c.clock.NowMS(), card.ID); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "card", card.ID, "restored", "", "", ""); err != nil {
			return err
		}
		return c.loadCard(tx, projectID, ref, &card)
	})
	if err != nil || trashID == "" {
		return card, err
	}
	if _, err := c.restoreTrash(ctx, trashID, func(*sqlx.Tx, TrashItem) error { return nil }); err != nil {
		return Card{}, err
	}
	return c.GetCard(ctx, projectID, ref)
}
