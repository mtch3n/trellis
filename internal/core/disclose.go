package core

import "github.com/jmoiron/sqlx"

// privateAfterRefresh re-reads the named entries from disk and reports which of
// them the file says are private.
//
// It exists because the private column is a mirror and is one read stale after
// a file changes, and because the two callers — recall and the pin list — are
// the paths that put text in front of a model without being asked. Deciding
// from the mirror there means a document can be disclosed after its author
// marked it private and before anything happened to refresh the row.
//
// Reading the files is affordable because both callers are bounded: a recall
// returns RecallOpts.Limit hits, five by default, and pins are curated by hand.
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
			return nil, err
		}
		if docs[i].Private {
			out[docs[i].ID] = true
		}
	}
	return out, nil
}
