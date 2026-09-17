package core

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

// CardRelation is one card's relation to another, read from the first card's
// side: {Rel: "resolved_by", Ref: "X-2"} means X-2 resolves this card.
type CardRelation struct {
	Rel    string `db:"rel" json:"rel"`
	Ref    string `db:"ref" json:"ref"`
	Title  string `db:"title" json:"title"`
	Column string `db:"column" json:"column"`
	Done   bool   `db:"done" json:"done"`
}

// A relation is stored once, in the direction of its canonical name, as a
// card-to-card link. The other name is how the far card reads it.
var inverseRelation = map[string]string{
	"blocked_by":   "blocks",
	"resolved_by":  "resolves",
	"duplicate_of": "duplicated_by",
	"relates_to":   "relates_to",
}

// storedRelation maps any relation name, with dashes or underscores, to the
// name it is stored under, and reports whether the two cards swap places.
func storedRelation(rel string) (stored string, flip bool, err error) {
	rel = strings.ReplaceAll(strings.TrimSpace(rel), "-", "_")
	if _, ok := inverseRelation[rel]; ok {
		return rel, false, nil
	}
	for canonical, inverse := range inverseRelation {
		if rel == inverse {
			return canonical, true, nil
		}
	}
	return "", false, ErrUsage("unknown_relation", "unknown relation "+rel+
		"; use blocked-by, blocks, resolved-by, resolves, duplicate-of, duplicated-by or relates-to",
		"trellis card relate <card> resolved-by <other-card>")
}

// RelateCards records that card ref stands in relation rel to card other.
// Blocking goes through BlockCard, which refuses cycles. Relating two cards
// again is a no-op, and relates_to is one relation whichever side states it.
func (c *Core) RelateCards(ctx context.Context, projectID string, ref CardRef, rel string, other CardRef) error {
	stored, flip, err := storedRelation(rel)
	if err != nil {
		return err
	}
	if stored == "blocked_by" {
		if flip {
			return c.BlockCard(ctx, projectID, other, ref)
		}
		return c.BlockCard(ctx, projectID, ref, other)
	}
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		card, target, err := c.relationCards(tx, projectID, ref, other)
		if err != nil {
			return err
		}
		if card.ID == target.ID {
			return ErrUsage("self_relation", "a card cannot relate to itself",
				"trellis card relate "+card.Ref+" "+rel+" <other-card>")
		}
		from, to := card, target
		if flip {
			from, to = target, card
		}
		if stored == "relates_to" {
			var n int
			if err := tx.Get(&n, `SELECT COUNT(*) FROM link
				WHERE from_type = 'card' AND to_type = 'card' AND rel = 'relates_to'
				  AND from_id = ? AND to_id = ?`, to.ID, from.ID); err != nil {
				return err
			}
			if n > 0 {
				return nil
			}
		}
		res, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('card', ?, 'card', ?, ?, NULL, ?)`,
			from.ID, to.ID, to.Ref, stored)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return c.recordEvent(tx, "card", card.ID, "related", relationName(stored, flip), "", target.Ref)
	})
}

// UnrelateCards removes a relation RelateCards recorded, named from either
// side.
func (c *Core) UnrelateCards(ctx context.Context, projectID string, ref CardRef, rel string, other CardRef) error {
	stored, flip, err := storedRelation(rel)
	if err != nil {
		return err
	}
	if stored == "blocked_by" {
		if flip {
			return c.UnblockCard(ctx, projectID, other, ref)
		}
		return c.UnblockCard(ctx, projectID, ref, other)
	}
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		card, target, err := c.relationCards(tx, projectID, ref, other)
		if err != nil {
			return err
		}
		from, to := card, target
		if flip {
			from, to = target, card
		}
		q := `DELETE FROM link WHERE from_type = 'card' AND to_type = 'card' AND rel = ?
			AND ((from_id = ? AND to_id = ?)`
		args := []any{stored, from.ID, to.ID}
		if stored == "relates_to" {
			q += ` OR (from_id = ? AND to_id = ?)`
			args = append(args, to.ID, from.ID)
		}
		res, err := tx.Exec(q+`)`, args...)
		if err != nil {
			return err
		}
		name := relationName(stored, flip)
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound("not_related", card.Ref+" is not "+name+" "+target.Ref,
				"trellis card show "+card.Ref)
		}
		return c.recordEvent(tx, "card", card.ID, "unrelated", name, target.Ref, "")
	})
}

// relationCards loads both ends of a relation. The caller must own the card
// it is changing, as with blocking.
func (c *Core) relationCards(tx *sqlx.Tx, projectID string, ref, other CardRef) (card, target Card, err error) {
	if err = c.loadCard(tx, projectID, ref, &card); err != nil {
		return
	}
	if err = c.checkCardOwner(card); err != nil {
		return
	}
	err = c.loadCard(tx, projectID, other, &target)
	return
}

func relationName(stored string, flip bool) string {
	if flip {
		return inverseRelation[stored]
	}
	return stored
}

// CardRelations lists a card's relations from its own side, blockers
// included, sorted by relation and then ref.
func (c *Core) CardRelations(ctx context.Context, cardID string) ([]CardRelation, error) {
	var rows []struct {
		CardRelation
		Incoming bool `db:"incoming"`
	}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&rows, `
			SELECT l.rel, o.ref, o.title, col.name AS column, (col.is_done = 1) AS done, 0 AS incoming
			FROM link l
			JOIN card o ON o.id = l.to_id
			JOIN column_ col ON col.id = o.column_id
			WHERE l.from_type = 'card' AND l.to_type = 'card' AND l.from_id = ?
			  AND l.rel IN ('blocked_by', 'resolved_by', 'duplicate_of', 'relates_to')
			UNION ALL
			SELECT l.rel, o.ref, o.title, col.name, (col.is_done = 1), 1
			FROM link l
			JOIN card o ON o.id = l.from_id
			JOIN column_ col ON col.id = o.column_id
			WHERE l.from_type = 'card' AND l.to_type = 'card' AND l.to_id = ?
			  AND l.rel IN ('blocked_by', 'resolved_by', 'duplicate_of', 'relates_to')`,
			cardID, cardID)
	})
	if err != nil {
		return nil, err
	}
	out := make([]CardRelation, 0, len(rows))
	for _, r := range rows {
		r.Rel = relationName(r.Rel, r.Incoming)
		out = append(out, r.CardRelation)
	}
	slices.SortFunc(out, func(a, b CardRelation) int {
		return cmp.Or(strings.Compare(a.Rel, b.Rel), strings.Compare(a.Ref, b.Ref))
	})
	return out, nil
}
