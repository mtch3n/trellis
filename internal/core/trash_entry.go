package core

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// trashedEntry builds an entry from its snapshot and its file in the trash,
// marked with when it was trashed.
func (c *Core) trashedEntry(tx *sqlx.Tx, it TrashItem, withBody bool) (Entry, error) {
	var e Entry
	snap, err := decodeSnapshot(it.Rows)
	if err != nil {
		return e, err
	}
	if err := scanRow(tx.Unsafe(), &e, it.rootRow(snap)); err != nil {
		return e, err
	}
	e.Ref = EntryAddress(it.ProjectKey, e.Global, e.Slug)
	e.TrashedAt = &it.TrashedAt
	e.Fields = map[string]any{}
	if withBody {
		if _, body, err := SplitFrontmatter(c.trashedEntryBody(it)); err == nil {
			e.BodyMD = body
		}
	}
	return e, nil
}

// TrashedEntry returns an entry from the project's trash by slug.
func (c *Core) TrashedEntry(ctx context.Context, projectID, slug string) (Entry, error) {
	var e Entry
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		key, err := projectKeyOf(tx, projectID)
		if err != nil {
			return err
		}
		d, err := readEntryArg(slug, key)
		if err != nil {
			return err
		}
		it, err := findTrash(tx, projectID, TrashEntry, d.slug)
		if err != nil {
			return err
		}
		e, err = c.trashedEntry(tx, it, true)
		return err
	})
	return e, err
}

// TrashedEntries lists the entries in a project's trash, newest first.
func (c *Core) TrashedEntries(ctx context.Context, projectID string) ([]Entry, error) {
	var out []Entry
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var items []TrashItem
		if err := tx.Select(&items, `SELECT * FROM trash WHERE kind = 'entry' AND project_id = ?
			ORDER BY trashed_at DESC, id DESC`, projectID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, it := range items {
			e, err := c.trashedEntry(tx, it, false)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}
