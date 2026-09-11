package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// Blocker is one card standing in the way of another.
type Blocker struct {
	Ref   string `db:"ref" json:"ref"`
	Title string `db:"title" json:"title"`
	Done  bool   `db:"done" json:"done"`
}

// BlockCard records that card `ref` cannot proceed until `blockerRef` is done.
// `card next --claim` already skips cards with an unfinished blocker (§8.3).
func (c *Core) BlockCard(ctx context.Context, projectID string, ref, blockerRef CardRef) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card, blocker Card
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardOwner(card); err != nil {
			return err
		}
		if err := c.loadCard(tx, projectID, blockerRef, &blocker); err != nil {
			return err
		}
		if card.ID == blocker.ID {
			return ErrUsage("self_block", "a card cannot block itself",
				"trellis card block "+card.Ref+" --by <other-card>")
		}
		var cycle int
		if err := tx.Get(&cycle, `WITH RECURSIVE reach(id) AS (
			SELECT to_id FROM link WHERE from_id = ? AND rel = 'blocked_by'
			UNION SELECT l.to_id FROM link l JOIN reach r ON l.from_id = r.id WHERE l.rel = 'blocked_by'
		) SELECT COUNT(*) FROM reach WHERE id = ?`, blocker.ID, card.ID); err != nil {
			return err
		}
		if cycle > 0 {
			return ErrUsage("blocked_cycle", "blocked_by would create a cycle", "trellis card show "+card.Ref)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('card', ?, 'card', ?, ?, NULL, 'blocked_by')`,
			card.ID, blocker.ID, blocker.Ref); err != nil {
			return err
		}
		return c.recordEvent(tx, "card", card.ID, "blocked", "blocked_by", "", blocker.Ref)
	})
}

// UnblockCard removes a blocked_by link.
func (c *Core) UnblockCard(ctx context.Context, projectID string, ref, blockerRef CardRef) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card, blocker Card
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardOwner(card); err != nil {
			return err
		}
		if err := c.loadCard(tx, projectID, blockerRef, &blocker); err != nil {
			return err
		}
		res, err := tx.Exec(
			`DELETE FROM link WHERE from_type = 'card' AND from_id = ?
			   AND to_type = 'card' AND to_id = ? AND rel = 'blocked_by'`,
			card.ID, blocker.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound("not_blocked", card.Ref+" is not blocked by "+blocker.Ref,
				"trellis card show "+card.Ref)
		}
		return c.recordEvent(tx, "card", card.ID, "unblocked", "blocked_by", blocker.Ref, "")
	})
}

// Blockers lists the cards blocking this one, done ones included so the agent
// can see why a card became claimable.
func (c *Core) Blockers(ctx context.Context, cardID string) ([]Blocker, error) {
	out := []Blocker{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&out,
			`SELECT p.key || '-' || b.seq AS ref, b.title,
			        (col.is_done = 1) AS done
			 FROM link l
			 JOIN card b ON b.id = l.to_id
			 JOIN project p ON p.id = b.project_id
			 JOIN column_ col ON col.id = b.column_id
			 WHERE l.from_id = ? AND l.rel = 'blocked_by'
			 ORDER BY done, b.seq`, cardID)
	})
	return out, err
}

// Backup writes a consistent copy of the database with VACUUM INTO (§5).
// Copying the live file instead can capture a torn WAL.
func (c *Core) Backup(ctx context.Context, dest string) error {
	_, err := c.db.ExecContext(ctx, `VACUUM INTO ?`, dest)
	return err
}
