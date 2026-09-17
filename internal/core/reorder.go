package core

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// MoveCardBefore moves a card to a column and places it immediately before
// beforeRef. An empty beforeRef appends it to the destination column. Ranks
// are fixed-width lexical fractions, so ordering remains deterministic across
// processes and does not depend on UUID monotonicity. Re-ranking the affected
// column is deliberately transactional; it is the safe fallback for legacy
// UUID ranks created before drag ordering existed.
func (c *Core) MoveCardBefore(ctx context.Context, projectID, boardID string, ref CardRef, column, beforeRef string) (Card, error) {
	var card Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardClaim(card); err != nil {
			return err
		}
		to, err := c.ColumnByName(tx, boardID, column)
		if err != nil {
			return err
		}

		var beforeID string
		if beforeRef != "" {
			var before Card
			if err := c.loadCard(tx, projectID, ParseCardRef(beforeRef), &before); err != nil {
				return err
			}
			if before.BoardID != boardID || before.ColumnID != to.ID {
				return ErrUsage("invalid_reorder_target", "the reorder target must be in the destination column", "trellis card ls")
			}
			beforeID = before.ID
		}

		var ids []string
		if err := tx.Select(&ids, `SELECT id FROM card WHERE board_id = ? AND column_id = ? AND archived_at IS NULL ORDER BY rank`, boardID, to.ID); err != nil {
			return err
		}
		ordered := make([]string, 0, len(ids))
		for _, id := range ids {
			if id != card.ID {
				ordered = append(ordered, id)
			}
		}
		insertAt := len(ordered)
		if beforeID != "" {
			insertAt = -1
			for i, id := range ordered {
				if id == beforeID {
					insertAt = i
					break
				}
			}
			if insertAt < 0 {
				return ErrNotFound("reorder_target_not_found", "reorder target is no longer on the board", "trellis card ls")
			}
		}
		ordered = append(ordered, "")
		copy(ordered[insertAt+1:], ordered[insertAt:])
		ordered[insertAt] = card.ID

		oldColumn := card.ColumnName
		oldRank := card.Rank
		now := c.clock.NowMS()
		for i, id := range ordered {
			rank := fmt.Sprintf("%020d", (i+1)*1_000_000)
			if id == card.ID {
				claimUpdate := ""
				if to.IsDone {
					claimUpdate = ", claimed_by = NULL, claim_until = NULL"
				}
				if _, err := tx.Exec(`UPDATE card SET board_id = ?, column_id = ?, rank = ?, version = version + 1, updated_at = ?`+claimUpdate+` WHERE id = ?`, boardID, to.ID, rank, now, card.ID); err != nil {
					return err
				}
				card.ColumnID, card.ColumnName, card.Rank, card.Version, card.UpdatedAt = to.ID, to.Name, rank, card.Version+1, now
				if to.IsDone {
					card.ClaimedBy = nil
					card.ClaimUntil = nil
				}
			} else if _, err := tx.Exec(`UPDATE card SET rank = ? WHERE id = ?`, rank, id); err != nil {
				return err
			}
		}
		if oldColumn != to.Name {
			if err := c.recordEvent(tx, "card", card.ID, "moved", "column", oldColumn, to.Name); err != nil {
				return err
			}
		}
		if oldRank != card.Rank || oldColumn != to.Name {
			if err := c.recordEvent(tx, "card", card.ID, "reordered", "rank", oldRank, card.Rank); err != nil {
				return err
			}
		}
		return c.cardView(tx, &card)
	})
	return card, err
}
