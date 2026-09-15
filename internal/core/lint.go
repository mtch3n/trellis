package core

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

// LintFinding is one problem with the vault. Lint reports; it never repairs.
type LintFinding struct {
	Kind string `json:"kind"` // stub, broken_anchor, orphan
	Doc  string `json:"doc"`
	Ref  string `json:"ref,omitempty"`
	Fix  string `json:"fix"`
}

// Lint reports stubs, broken anchors and orphans (§10). Each finding names what
// to do about it; none of them is an error, because a vault under construction
// is full of all three.
func (c *Core) Lint(ctx context.Context, projectID string) ([]LintFinding, error) {
	out := []LintFinding{}
	docs, err := c.ListKnowledge(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	anchorsBySlug := map[string]map[string]bool{}
	for _, d := range docs {
		set := map[string]bool{}
		for _, a := range HeadingAnchors(d.BodyMD) {
			set[a] = true
		}
		anchorsBySlug[d.Slug] = set
	}

	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		for _, d := range docs {
			var rows []struct {
				ToRaw  string         `db:"to_raw"`
				ToID   sql.NullString `db:"to_id"`
				Anchor sql.NullString `db:"anchor"`
			}
			if err := tx.Select(&rows,
				`SELECT to_raw, to_id, anchor FROM link
				 WHERE from_type = 'doc' AND from_id = ? AND rel = 'wikilink' ORDER BY to_raw`, d.ID); err != nil {
				return err
			}
			for _, r := range rows {
				if !r.ToID.Valid {
					out = append(out, LintFinding{Kind: "stub", Doc: d.Slug, Ref: r.ToRaw,
						Fix: `trellis knowledge new --title "` + r.ToRaw + `"`})
					continue
				}
				if !r.Anchor.Valid || r.Anchor.String == "" {
					continue
				}
				slug, _, _ := strings.Cut(r.ToRaw, "#")
				if _, rest, ok := strings.Cut(slug, "/"); ok {
					slug = rest
				}
				if set, known := anchorsBySlug[Slugify(slug)]; known && !set[r.Anchor.String] {
					out = append(out, LintFinding{Kind: "broken_anchor", Doc: d.Slug, Ref: r.ToRaw,
						Fix: "trellis knowledge show " + Slugify(slug) + "   # check its headings"})
				}
			}

			var inbound int
			if err := tx.Get(&inbound,
				`SELECT COUNT(*) FROM link WHERE to_type = 'doc' AND to_id = ?`, d.ID); err != nil {
				return err
			}
			// Stubs count as outbound: an entry whose only link is broken is
			// reported as a stub, and reporting it as an orphan too would be
			// two findings for one fix.
			var outbound int
			if err := tx.Get(&outbound,
				`SELECT COUNT(*) FROM link WHERE from_type = 'doc' AND from_id = ?`,
				d.ID); err != nil {
				return err
			}
			if inbound == 0 && outbound == 0 {
				out = append(out, LintFinding{Kind: "orphan", Doc: d.Slug,
					Fix: "link it from a card or another entry, or remove it"})
			}
		}
		return nil
	})
	slices.SortStableFunc(out, func(a, b LintFinding) int { return cmp.Compare(a.Kind, b.Kind) })
	return out, err
}
