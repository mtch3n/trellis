package core

import (
	"errors"

	"github.com/jmoiron/sqlx"
)

// privateAfterRefresh re-reads the named entries from disk and reports which of
// them the file says are private.
//
// It exists because the private column is a mirror and is one read stale after
// a file changes, and because the two callers — recall and the pin list — are
// the paths that put text in front of a model without being asked. Deciding
// from the mirror there means a document can be disclosed after its author
// marked it private and before anything happened to refresh the row.
//
// Reading the files is affordable because both callers pass a bounded set: the
// hits recall actually returns, o.Limit of them (five by default), not the
// larger candidate pool it ranks over; pins are curated by hand.
//
// A file that is gone cannot have its disclosure status confirmed, so it is
// treated as private rather than disclosed or failed: the id comes back marked
// private, and the caller's recall does not error out over an unrelated vault
// file someone deleted. Any other failure to read or parse the file is a real
// error and propagates.
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
