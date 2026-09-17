package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// RecallUptake is how often an identifier recall put in front of an agent was
// then opened, split by the path the entry arrived through.
//
// Injected and never opened is noise, and it was paid for on cache write plus
// every later read in that session. Injected and then opened is a hit. The
// ratio is the only thing separating a useful entry from a placeholder that
// needs no human to label it.
type RecallUptake struct {
	Provenance string `db:"provenance" json:"provenance"`
	Injected   int    `db:"injected" json:"injected"`
	Opened     int    `db:"opened" json:"opened"`
}

// RecallUptake pairs injections with the reads that followed them.
//
// The pairing key is `actor`. The hook derives a stable TRELLIS_AGENT per
// harness session, so every event from one session shares one, and `seq` is a
// monotonic autoincrement, which makes "afterwards" exact without consulting a
// clock. No extra table is needed for any of it.
//
// Cards are left out. They carry no provenance, so they have no column in the
// comparison this exists to make.
//
// This measures association, not causation: an agent may have opened an entry
// it would have found anyway. It separates "was reached for" from "was
// ignored", which is the question currently unanswerable, not the whole one.
func (c *Core) RecallUptake(ctx context.Context, projectID string) ([]RecallUptake, error) {
	rows := []RecallUptake{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&rows, `
			SELECT k.provenance AS provenance,
			       COUNT(*) AS injected,
			       SUM(CASE WHEN EXISTS (
			             SELECT 1 FROM event r
			             WHERE r.actor = i.actor
			               AND r.entity_id = i.entity_id
			               AND r.action = 'read'
			               AND r.seq > i.seq
			           ) THEN 1 ELSE 0 END) AS opened
			FROM event i
			JOIN entry k ON k.id = i.entity_id
			WHERE i.action = 'injected'
			  AND i.entity_type = 'entry'
			  AND (k.project_id = ? OR k.global = 1)
			GROUP BY k.provenance
			ORDER BY k.provenance`, projectID)
	})
	return rows, err
}
