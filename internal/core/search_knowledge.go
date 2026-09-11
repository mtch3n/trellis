package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// ListSearchKnowledge returns the documents visible to a project search:
// entries owned by the project plus globally escalated entries. It refreshes
// file-backed rows using the same source-of-truth rules as ListKnowledge.
func (c *Core) ListSearchKnowledge(ctx context.Context, projectID string) ([]Knowledge, error) {
	docs := []Knowledge{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Select(&docs, `SELECT * FROM knowledge WHERE project_id = ? OR global = 1 ORDER BY updated_at DESC`, projectID); err != nil {
			return err
		}
		for i := range docs {
			if err := c.refreshFromFile(tx, &docs[i]); err != nil {
				return err
			}
			if err := c.docView(tx, &docs[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return docs, err
}
