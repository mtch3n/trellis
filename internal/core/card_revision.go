package core

import (
	"github.com/jmoiron/sqlx"
)

// captureCardRevision retains a card's title and body at version, unless
// capture is disabled. The insert is idempotent (INSERT OR IGNORE) so
// callers may capture a version that might already have a row -- the one a
// card is about to leave, which an earlier edit's own "after" capture may
// already have written -- without checking first.
func (c *Core) captureCardRevision(tx *sqlx.Tx, cardID string, version int64, title, body string) error {
	if c.historyKeep == 0 {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO card_revision (card_id, version, title, body_md, actor, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		cardID, version, title, body, c.actor, c.clock.NowMS()); err != nil {
		return err
	}
	return trimCardRevisions(tx, cardID, c.historyKeep)
}

// cardHasRevision reports whether a card has any retained revision at all.
// EditCard's "before" capture is gated on this rather than on the card's
// current version, so a version bump with no content change (a move, a lease
// change) between two edits does not get invented into a revision of its own.
func (c *Core) cardHasRevision(tx *sqlx.Tx, cardID string) (bool, error) {
	var exists bool
	err := tx.Get(&exists, `SELECT EXISTS(SELECT 1 FROM card_revision WHERE card_id = ?)`, cardID)
	return exists, err
}

// trimCardRevisions removes a card's oldest revisions until at most keep
// remain, ordered by version.
func trimCardRevisions(tx *sqlx.Tx, cardID string, keep int) error {
	var versions []int64
	if err := tx.Select(&versions,
		`SELECT version FROM card_revision WHERE card_id = ? ORDER BY version DESC`, cardID); err != nil {
		return err
	}
	if len(versions) <= keep {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM card_revision WHERE card_id = ? AND version <= ?`, cardID, versions[keep])
	return err
}
