package core

import (
	"context"
	"strings"

	"github.com/mtch3n/trellis/internal/address"
)

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
	entries, err := c.ListEntries(ctx, projectID, EntryFilter{})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	clusters := []DupeCluster{}
	for _, e := range entries {
		if seen[e.Slug] {
			continue
		}
		terms := significantTerms(e.Title)
		if len(terms) == 0 {
			continue
		}
		// An OR of the title's terms is an FTS5 expression, not something a
		// person typed, so it goes to MATCH composed rather than quoted whole.
		quoted := make([]string, 0, len(terms))
		for _, t := range terms {
			quoted = append(quoted, ftsPhrase(t))
		}
		hits, err := c.matchEntries(ctx, projectID, strings.Join(quoted, " OR "), 10)
		if err != nil {
			return nil, err
		}
		cluster := []string{e.Slug}
		for _, h := range hits {
			addr, err := address.Parse(h.Ref)
			if err != nil {
				return nil, err
			}
			slug := addr.Name
			if slug == e.Slug || seen[slug] {
				continue
			}
			if overlap(terms, significantTerms(h.Title)) >= 2 {
				cluster = append(cluster, slug)
				seen[slug] = true
			}
		}
		if len(cluster) > 1 {
			seen[e.Slug] = true
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
