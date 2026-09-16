package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// ReadWindowDays is the window the read counters use.
const ReadWindowDays = 30

// recordRead notes that an entry was actually read. §10.7 sources this from
// `invocation`, but the invocation log deliberately stores no positional
// arguments — the slug is exactly what it drops — so the read is recorded here
// instead, where it is exact rather than parsed back out of a command line.
func (c *Core) recordRead(tx *sqlx.Tx, docID string) error {
	return c.recordEvent(tx, "knowledge", docID, "read", "", "", "")
}

// HealthLine is one row of the housekeeping report. Every line names the
// command that acts on it: a score would hide the reason, and the reason is the
// only actionable part (§10.10).
type HealthLine struct {
	What  string `json:"what"`
	Count int    `json:"count"`
	Fix   string `json:"fix"`
}

// Health counts what a human would want to look at before deciding to tidy.
// Detection is free and runs on demand; action is always human (§10.10).
func (c *Core) Health(ctx context.Context, projectID string) ([]HealthLine, error) {
	findings, err := c.Lint(ctx, projectID)
	if err != nil {
		return nil, err
	}
	byKind := map[string]int{}
	for _, f := range findings {
		byKind[f.Kind]++
	}

	var cold, stale, total int
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&total, `SELECT COUNT(*) FROM knowledge WHERE project_id = ?`, projectID); err != nil {
			return err
		}
		if err := tx.Get(&cold, `
			SELECT COUNT(*) FROM knowledge k
			WHERE k.project_id = ?
			  AND NOT EXISTS (
			    SELECT 1 FROM event e
			    WHERE e.entity_type = 'knowledge' AND e.entity_id = k.id
			      AND e.action = 'read' AND e.ts > ?)`,
			projectID, c.clock.NowMS()-readWindowMS()); err != nil {
			return err
		}
		return tx.Get(&stale, `
			SELECT COUNT(*) FROM pin p JOIN knowledge k ON k.id = p.knowledge_id
			WHERE k.project_id = ? AND k.recap_hash IS NOT NULL
			  AND k.recap_hash IS NOT k.content_hash`, projectID)
	})
	if err != nil {
		return nil, err
	}

	return []HealthLine{
		{What: "entries", Count: total, Fix: "trellis knowledge ls"},
		{What: "never read in " + itoa(ReadWindowDays) + "d", Count: cold, Fix: "trellis knowledge ls --cold"},
		{What: "stale pinned recaps", Count: stale, Fix: "trellis knowledge pins --stale"},
		{What: "orphans (no links)", Count: byKind["orphan"], Fix: "trellis knowledge lint"},
		{What: "stubs", Count: byKind["stub"], Fix: "trellis knowledge lint"},
		{What: "broken anchors", Count: byKind["broken_anchor"], Fix: "trellis knowledge lint"},
	}, nil
}
func readWindowMS() int64 { return int64(ReadWindowDays) * 24 * 60 * 60 * 1000 }

// ColdKnowledge lists entries nothing has read inside the window. Cold is a
// candidate for review, never for automatic deletion.
//
// Private comes from the files, not the mirror, so a caller that withholds a
// private entry's content decides on what the author last wrote. An entry
// whose file is gone is reported private: its status cannot be confirmed, and
// one missing file must not fail the whole listing.
func (c *Core) ColdKnowledge(ctx context.Context, projectID string) ([]Knowledge, error) {
	docs := []Knowledge{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Select(&docs, `
			SELECT k.* FROM knowledge k
			WHERE k.project_id = ?
			  AND NOT EXISTS (
			    SELECT 1 FROM event e
			    WHERE e.entity_type = 'knowledge' AND e.entity_id = k.id
			      AND e.action = 'read' AND e.ts > ?)
			ORDER BY k.updated_at`, projectID, c.clock.NowMS()-readWindowMS()); err != nil {
			return err
		}
		ids := make([]string, 0, len(docs))
		for _, d := range docs {
			ids = append(ids, d.ID)
		}
		private, err := c.privateAfterRefresh(tx, ids)
		if err != nil {
			return err
		}
		for i := range docs {
			docs[i].Private = private[docs[i].ID]
			if err := c.docView(tx, &docs[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return docs, err
}
