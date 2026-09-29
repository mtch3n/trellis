package core

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// findTrashedCard finds the trashed card ref names.
func (c *Core) findTrashedCard(tx *sqlx.Tx, projectID string, ref CardRef) (TrashItem, error) {
	var it TrashItem
	var err error
	switch {
	case ref.UUID != "":
		err = tx.Get(&it, `SELECT * FROM trash WHERE kind = 'card' AND project_id = ? AND item_id = ?`, projectID, ref.UUID)
	case ref.Seq > 0:
		name := ref.qualified()
		if ref.ProjectKey == "" {
			key, kerr := projectKeyOf(tx, projectID)
			if kerr != nil {
				return it, kerr
			}
			name = key + "-" + itoa(ref.Seq)
		}
		err = tx.Get(&it, `SELECT * FROM trash WHERE kind = 'card' AND project_id = ? AND name = ?
			ORDER BY trashed_at DESC LIMIT 1`, projectID, name)
	default:
		return it, ErrUsage("bad_card_ref", "card reference is empty", "trellis card ls --trashed")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound("card_not_found", "no card "+ref.String()+" in this project or its trash",
			"trellis card ls --trashed")
	}
	return it, err
}

// trashedCard builds a card from its snapshot, marked with when it was
// trashed. Its column, labels and tags come from the snapshot too.
func trashedCard(tx *sqlx.Tx, it TrashItem) (Card, error) {
	var card Card
	snap, err := decodeSnapshot(it.Rows)
	if err != nil {
		return card, err
	}
	if err := scanRow(tx.Unsafe(), &card, it.rootRow(snap)); err != nil {
		return card, err
	}
	card.TrashedAt = &it.TrashedAt
	card.PriorityName = card.Priority.String()
	card.Labels, card.Tags = []string{}, []string{}
	for _, r := range snap.Rows {
		switch r.Table {
		case "column_":
			if r.Row["id"] == card.ColumnID {
				card.ColumnName, _ = r.Row["name"].(string)
			}
		case "card_label", "card_tag":
			table, fk := "label", "label_id"
			if r.Table == "card_tag" {
				table, fk = "tag", "tag_id"
			}
			var name string
			if err := tx.Get(&name, `SELECT name FROM `+table+` WHERE id = ?`, r.Row[fk]); err == nil {
				if table == "label" {
					card.Labels = append(card.Labels, name)
				} else {
					card.Tags = append(card.Tags, name)
				}
			}
		}
	}
	if card.ColumnName == "" {
		_ = tx.Get(&card.ColumnName, `SELECT name FROM column_ WHERE id = ?`, card.ColumnID)
	}
	return card, nil
}

// TrashedCard returns a card from the project's trash.
func (c *Core) TrashedCard(ctx context.Context, projectID string, ref CardRef) (Card, error) {
	var card Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		it, err := c.findTrashedCard(tx, projectID, ref)
		if err != nil {
			return err
		}
		card, err = trashedCard(tx, it)
		return err
	})
	return card, err
}

// TrashedCards lists the cards in the project's trash that were on boardID,
// or on any board when boardID is empty, newest first.
func (c *Core) TrashedCards(ctx context.Context, projectID, boardID string) ([]Card, error) {
	var out []Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var items []TrashItem
		if err := tx.Select(&items, `SELECT * FROM trash WHERE kind = 'card' AND project_id = ?
			ORDER BY trashed_at DESC, id DESC`, projectID); err != nil {
			return err
		}
		for _, it := range items {
			card, err := trashedCard(tx, it)
			if err != nil {
				return err
			}
			if boardID == "" || card.BoardID == boardID {
				out = append(out, card)
			}
		}
		return nil
	})
	return out, err
}

// nextCardSeq is the number a new card in projectID gets: above every live
// card and every card in the trash, so a ref never names two cards while the
// first can still come back.
func nextCardSeq(tx *sqlx.Tx, projectID string) (int64, error) {
	var seq int64
	err := tx.Get(&seq, `SELECT max(
		(SELECT COALESCE(MAX(seq), 0) FROM card WHERE project_id = ?),
		(SELECT COALESCE(MAX(seq), 0) FROM trash WHERE project_id = ?)) + 1`, projectID, projectID)
	return seq, err
}
