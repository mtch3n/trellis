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

// CardRelation represents a card's relation to another card.
type CardRelation struct {
	Rel    string `db:"rel" json:"rel"`
	Ref    string `db:"ref" json:"ref"`
	Title  string `db:"title" json:"title"`
	Column string `db:"column" json:"column"`
	Done   bool   `db:"done" json:"done"`
}

// RelateCards creates a relation between two cards. For blocked_by, it delegates to BlockCard
// which includes cycle checking. For other relations, it records a direct link.
// For relates_to (symmetric), if the inverse relation exists, this is a no-op.
func (c *Core) RelateCards(ctx context.Context, projectID string, ref CardRef, rel string, other CardRef) error {
	// Validate rel
	validRels := map[string]bool{
		"blocked_by":    true,
		"blocks":        true,
		"resolved_by":   true,
		"resolves":      true,
		"duplicate_of":  true,
		"duplicated_by": true,
		"relates_to":    true,
	}
	if !validRels[rel] {
		return ErrUsage("unknown_relation", "unknown relation type: "+rel, "trellis card relate "+ref.String()+" <rel> <card>")
	}

	// Handle blocked_by/blocks through BlockCard for cycle checking
	if rel == "blocked_by" {
		return c.BlockCard(ctx, projectID, ref, other)
	}
	if rel == "blocks" {
		return c.BlockCard(ctx, projectID, other, ref)
	}

	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card, otherCard Card
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardOwner(card); err != nil {
			return err
		}
		if err := c.loadCard(tx, projectID, other, &otherCard); err != nil {
			return err
		}
		if card.ID == otherCard.ID {
			return ErrUsage("self_relation", "a card cannot relate to itself",
				"trellis card relate "+card.Ref+" <rel> <other-card>")
		}

		// For relates_to, check if relation exists in either direction (symmetric, stored once)
		storedRel := rel
		fromID := card.ID
		toID := otherCard.ID
		if rel == "relates_to" {
			var count int
			// Check both directions
			if err := tx.Get(&count, `
				SELECT COUNT(*) FROM link
				WHERE from_type='card' AND to_type='card'
				AND ((from_id=? AND to_id=?) OR (from_id=? AND to_id=?))
				AND rel='relates_to'`,
				fromID, toID, toID, fromID); err != nil {
				return err
			}
			if count > 0 {
				// Relation already exists in either direction, this is a no-op
				return nil
			}
		}

		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('card', ?, 'card', ?, ?, NULL, ?)`,
			fromID, toID, otherCard.Ref, storedRel); err != nil {
			return err
		}
		return c.recordEvent(tx, "card", card.ID, "related", storedRel, "", otherCard.Ref)
	})
}

// UnrelateCards removes a relation between two cards. For blocked_by, it delegates to UnblockCard.
func (c *Core) UnrelateCards(ctx context.Context, projectID string, ref CardRef, rel string, other CardRef) error {
	// Handle blocked_by/blocks through UnblockCard
	if rel == "blocked_by" {
		return c.UnblockCard(ctx, projectID, ref, other)
	}
	if rel == "blocks" {
		return c.UnblockCard(ctx, projectID, other, ref)
	}

	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card, otherCard Card
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardOwner(card); err != nil {
			return err
		}
		if err := c.loadCard(tx, projectID, other, &otherCard); err != nil {
			return err
		}

		storedRel := rel
		fromID := card.ID
		toID := otherCard.ID

		// For relates_to, also try the inverse direction
		if rel == "relates_to" {
			res, err := tx.Exec(
				`DELETE FROM link WHERE from_type='card' AND to_type='card'
				 AND from_id=? AND to_id=? AND rel='relates_to'`,
				fromID, toID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				// Try inverse
				res, err = tx.Exec(
					`DELETE FROM link WHERE from_type='card' AND to_type='card'
					 AND from_id=? AND to_id=? AND rel='relates_to'`,
					toID, fromID)
				if err != nil {
					return err
				}
				n, _ = res.RowsAffected()
				if n == 0 {
					return ErrNotFound("not_related", card.Ref+" is not related to "+otherCard.Ref,
						"trellis card show "+card.Ref)
				}
			}
		} else {
			res, err := tx.Exec(
				`DELETE FROM link WHERE from_type='card' AND to_type='card'
				 AND from_id=? AND to_id=? AND rel=?`,
				fromID, toID, storedRel)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return ErrNotFound("not_related", card.Ref+" does not have relation "+rel+" to "+otherCard.Ref,
					"trellis card show "+card.Ref)
			}
		}

		return c.recordEvent(tx, "card", card.ID, "unrelated", storedRel, otherCard.Ref, "")
	})
}

// CardRelations lists all relations of a card (outgoing with stored rel, incoming with inverse).
// Sorted by rel then ref.
func (c *Core) CardRelations(ctx context.Context, cardID string) ([]CardRelation, error) {
	out := []CardRelation{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Query outgoing relations with stored rel
		err := tx.Select(&out, `
			SELECT l.rel, p.key || '-' || b.seq AS ref, b.title, col.name AS column, (col.is_done = 1) AS done
			FROM link l
			JOIN card b ON b.id = l.to_id
			JOIN project p ON p.id = b.project_id
			JOIN column_ col ON col.id = b.column_id
			WHERE l.from_id = ? AND l.from_type = 'card' AND l.to_type = 'card'
			ORDER BY l.rel, ref`, cardID)
		if err != nil {
			return err
		}

		// Append incoming relations with inverse rel
		incoming := []CardRelation{}
		err = tx.Select(&incoming, `
			SELECT
				CASE l.rel
					WHEN 'blocked_by' THEN 'blocks'
					WHEN 'resolved_by' THEN 'resolves'
					WHEN 'duplicate_of' THEN 'duplicated_by'
					WHEN 'relates_to' THEN 'relates_to'
					ELSE l.rel
				END AS rel,
				p.key || '-' || b.seq AS ref, b.title, col.name AS column, (col.is_done = 1) AS done
			FROM link l
			JOIN card b ON b.id = l.from_id
			JOIN project p ON p.id = b.project_id
			JOIN column_ col ON col.id = b.column_id
			WHERE l.to_id = ? AND l.from_type = 'card' AND l.to_type = 'card'
			ORDER BY l.rel, ref`, cardID)
		if err != nil {
			return err
		}

		// Append all incoming relations (relates_to appears symmetrically from both directions)
		out = append(out, incoming...)

		// Re-sort by rel then ref
		return nil
	})
	if err != nil {
		return out, err
	}

	// Sort the results
	sortCardRelations(out)
	return out, nil
}

func sortCardRelations(rels []CardRelation) {
	// Bubble sort by rel then ref (simple for now)
	for i := 0; i < len(rels); i++ {
		for j := i + 1; j < len(rels); j++ {
			if rels[j].Rel < rels[i].Rel || (rels[j].Rel == rels[i].Rel && rels[j].Ref < rels[i].Ref) {
				rels[i], rels[j] = rels[j], rels[i]
			}
		}
	}
}
