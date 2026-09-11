package core

import (
	"context"
	"strings"

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
			WHERE k.project_id = ? AND k.recap_hash IS NOT k.content_hash`, projectID)
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
		for i := range docs {
			if err := c.docView(tx, &docs[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return docs, err
}

// dupeCluster groups entries that share enough terminology to be worth a look.
type DupeCluster struct {
	Slugs []string `json:"slugs"`
	Terms string   `json:"shared_terms"`
}

// Dupes is the cheapest of the three layers in §10.9: FTS5 over each entry's
// title, which catches shared terminology and overlapping titles. SimHash and
// embeddings are deliberately not here — they are added when this layer is
// measured to miss, not before.
func (c *Core) Dupes(ctx context.Context, projectID string) ([]DupeCluster, error) {
	docs, err := c.ListKnowledge(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	clusters := []DupeCluster{}
	for _, d := range docs {
		if seen[d.Slug] {
			continue
		}
		terms := significantTerms(d.Title)
		if len(terms) == 0 {
			continue
		}
		// An OR of the title's terms is an FTS5 expression, not something a
		// person typed, so it goes to MATCH composed rather than quoted whole.
		quoted := make([]string, 0, len(terms))
		for _, t := range terms {
			quoted = append(quoted, ftsPhrase(t))
		}
		hits, err := c.matchKnowledge(ctx, projectID, strings.Join(quoted, " OR "), 10)
		if err != nil {
			return nil, err
		}
		cluster := []string{d.Slug}
		for _, h := range hits {
			slug := h.Ref[strings.Index(h.Ref, "/")+1:]
			if slug == d.Slug || seen[slug] {
				continue
			}
			if overlap(terms, significantTerms(h.Title)) >= 2 {
				cluster = append(cluster, slug)
				seen[slug] = true
			}
		}
		if len(cluster) > 1 {
			seen[d.Slug] = true
			clusters = append(clusters, DupeCluster{Slugs: cluster, Terms: strings.Join(terms, " ")})
		}
	}
	return clusters, nil
}

// significantTerms drops the words that make everything look similar.
func significantTerms(title string) []string {
	stop := map[string]bool{"the": true, "a": true, "an": true, "and": true, "or": true,
		"of": true, "for": true, "to": true, "in": true, "on": true, "with": true, "notes": true}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(title)) {
		w = strings.Trim(w, ".,:;()[]")
		if len(w) > 2 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

func overlap(a, b []string) int {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	n := 0
	for _, s := range b {
		if set[s] {
			n++
		}
	}
	return n
}
