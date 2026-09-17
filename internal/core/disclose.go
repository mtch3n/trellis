package core

import (
	"errors"

	"github.com/jmoiron/sqlx"
)

// privateAfterRefresh re-reads the named entries from disk and reports which of
// them the file says are private.
//
// It exists because the private column is a mirror and is one read stale after
// a file changes. Recall and the pin list put text in front of a model without
// being asked, and the cold listing feeds `knowledge ls --cold`, whose JSON an
// agent reads. Deciding from the mirror on any of them means a document can be
// disclosed after its author marked it private and before anything happened to
// refresh the row.
//
// Recall and the pin list pass a bounded set: the hits recall actually
// returns, o.Limit of them (five by default), not the larger candidate pool it
// ranks over; pins are curated by hand. The cold listing passes every entry it
// lists, which is one file read each, the same as plain `knowledge ls`.
//
// A file that is gone cannot have its disclosure status confirmed, so it is
// treated as private rather than disclosed or failed: the id comes back marked
// private, and the caller does not error out over an unrelated vault file
// someone deleted. It is also reported in missing, so a listing can say the
// file is gone rather than call the entry private. Any other failure to read
// or parse the file is a real error and propagates.
func (c *Core) privateAfterRefresh(tx *sqlx.Tx, ids []string) (private, missing map[string]bool, err error) {
	private, missing = map[string]bool{}, map[string]bool{}
	if len(ids) == 0 {
		return private, missing, nil
	}
	q, args, err := sqlx.In(`SELECT * FROM entry WHERE id IN (?)`, ids)
	if err != nil {
		return nil, nil, err
	}
	var entries []Entry
	if err := tx.Select(&entries, tx.Rebind(q), args...); err != nil {
		return nil, nil, err
	}
	for i := range entries {
		if err := c.refreshFromFile(tx, &entries[i]); err != nil {
			var coreErr *Error
			if errors.As(err, &coreErr) && coreErr.Code == "file_missing" {
				private[entries[i].ID] = true
				missing[entries[i].ID] = true
				continue
			}
			return nil, nil, err
		}
		if entries[i].Private {
			private[entries[i].ID] = true
		}
	}
	return private, missing, nil
}
