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
// someone deleted. Any other failure to read or parse the file is a real error
// and propagates.
// privateAfterRefresh returns a map of entry IDs to whether they should be treated
// as private for display purposes. Missing files are treated as private (content
// is withheld), but the caller may want to distinguish them using missingAfterRefresh.
func (c *Core) privateAfterRefresh(tx *sqlx.Tx, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args, err := sqlx.In(`SELECT * FROM knowledge WHERE id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	var docs []Knowledge
	if err := tx.Select(&docs, tx.Rebind(q), args...); err != nil {
		return nil, err
	}
	for i := range docs {
		if err := c.refreshFromFile(tx, &docs[i]); err != nil {
			var coreErr *Error
			if errors.As(err, &coreErr) && coreErr.Code == "file_missing" {
				out[docs[i].ID] = true
				continue
			}
			return nil, err
		}
		if docs[i].Private {
			out[docs[i].ID] = true
		}
	}
	return out, nil
}

// missingAfterRefresh returns a map of entry IDs to whether their files are missing.
// Missing files have their content withheld like private entries, but should be
// labeled differently in the UI.
func (c *Core) missingAfterRefresh(tx *sqlx.Tx, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args, err := sqlx.In(`SELECT * FROM knowledge WHERE id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	var docs []Knowledge
	if err := tx.Select(&docs, tx.Rebind(q), args...); err != nil {
		return nil, err
	}
	for i := range docs {
		if err := c.refreshFromFile(tx, &docs[i]); err != nil {
			var coreErr *Error
			if errors.As(err, &coreErr) && coreErr.Code == "file_missing" {
				out[docs[i].ID] = true
				continue
			}
			return nil, err
		}
	}
	return out, nil
}
