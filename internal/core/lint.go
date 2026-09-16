package core

import (
	"cmp"
	"context"
	"database/sql"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// LintFinding is one problem with the vault. Lint reports; it never repairs.
type LintFinding struct {
	Kind string `json:"kind"` // stub, broken_anchor, orphan, missing_artifact, unknown_field, deep_directory, long_directory_name, similar_directory
	Doc  string `json:"doc"`
	Ref  string `json:"ref,omitempty"`
	Fix  string `json:"fix"`
}

// Lint reports stubs, broken anchors and orphans (§10). Each finding names what
// to do about it; none of them is an error, because a vault under construction
// is full of all three.
func (c *Core) Lint(ctx context.Context, projectID string) ([]LintFinding, error) {
	out := []LintFinding{}
	docs, err := c.ListKnowledge(ctx, projectID, KnowledgeFilter{})
	if err != nil {
		return nil, err
	}
	knownFields, err := c.knownExtraFields(ctx)
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

			var artifactStubs []string
			if err := tx.Select(&artifactStubs,
				`SELECT to_raw FROM link
				 WHERE from_type = 'doc' AND from_id = ? AND rel = 'artifact' AND to_id IS NULL
				 ORDER BY to_raw`, d.ID); err != nil {
				return err
			}
			for _, name := range artifactStubs {
				var matches int
				if err := tx.Get(&matches,
					`SELECT COUNT(*) FROM artifact WHERE project_id = ? AND name = ?`,
					d.ProjectID, name); err != nil {
					return err
				}
				f := LintFinding{Kind: "missing_artifact", Doc: d.Slug, Ref: name,
					Fix: "trellis artifact add <file>   # no artifact is named " + name}
				if matches > 1 {
					f.Fix = "trellis artifact ls   # " + strconv.Itoa(matches) +
						" artifacts are named " + name + "; remove the extra ones"
				}
				out = append(out, f)
			}

			raw, err := os.ReadFile(d.Path)
			if err != nil {
				return err
			}
			fm, _, err := splitDocFile(d.Path, raw)
			if err != nil {
				return err
			}
			var extraKeys []string
			for k := range fm.Extra {
				extraKeys = append(extraKeys, k)
			}
			slices.Sort(extraKeys)
			for _, k := range extraKeys {
				if !knownFields[k] {
					out = append(out, LintFinding{Kind: "unknown_field", Doc: d.Slug, Ref: k,
						Fix: "trellis knowledge template ls   # " + k + " is not in any template's required or choices"})
				}
			}

			var inbound int
			if err := tx.Get(&inbound,
				`SELECT COUNT(*) FROM link WHERE to_type = 'doc' AND to_id = ?`, d.ID); err != nil {
				return err
			}
			// Stubs count as outbound: an entry whose only link is broken is
			// reported as a stub, and reporting it as an orphan too would be
			// two findings for one fix. An attached artifact is not a connection
			// to another entry or card, so it does not count.
			var outbound int
			if err := tx.Get(&outbound,
				`SELECT COUNT(*) FROM link WHERE from_type = 'doc' AND from_id = ? AND rel != 'artifact'`,
				d.ID); err != nil {
				return err
			}
			if inbound == 0 && outbound == 0 {
				out = append(out, LintFinding{Kind: "orphan", Doc: d.Slug,
					Fix: "link it from a card or another entry, or remove it"})
			}

			if dirs := strings.Split(d.Slug, "/"); len(dirs) > 1 {
				dirs = dirs[:len(dirs)-1]
				if len(dirs) >= 3 {
					out = append(out, LintFinding{Kind: "deep_directory", Doc: d.Slug,
						Ref: strings.Join(dirs, "/"),
						Fix: "trellis knowledge mv " + d.Slug + " <a shallower path>   # depth is a design smell past two levels"})
				}
				for _, seg := range dirs {
					if len(seg) > 30 {
						out = append(out, LintFinding{Kind: "long_directory_name", Doc: d.Slug, Ref: seg,
							Fix: "trellis knowledge mv " + d.Slug + " <a shorter directory name>"})
					}
				}
			}
		}

		dirs, derr := c.projectDirectories(tx, projectID)
		if derr != nil {
			return derr
		}
		slices.Sort(dirs)
		for i, a := range dirs {
			for _, b := range dirs[i+1:] {
				if resembles(a, b) {
					out = append(out, LintFinding{Kind: "similar_directory", Ref: a + ", " + b,
						Fix: "trellis knowledge mv <an entry under one> <the other>   # or leave both if they mean different things"})
				}
			}
		}

		return nil
	})
	slices.SortStableFunc(out, func(a, b LintFinding) int { return cmp.Compare(a.Kind, b.Kind) })
	return out, err
}

// knownExtraFields is every field name any template on disk currently
// names, in required or choices. A frontmatter key outside this set, and
// outside the Frontmatter struct's own fields, is unrecognised no matter
// which template, if any, produced the document — a document does not
// remember which template created it. A template that fails to parse
// names nothing here; Lint reports the document's key regardless, which is
// the safer default when a template is broken.
func (c *Core) knownExtraFields(ctx context.Context) (map[string]bool, error) {
	dir, err := c.templatesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		t, err := loadTemplate(dir, name)
		if err != nil {
			continue
		}
		for _, f := range t.Required {
			known[f] = true
		}
		for f := range t.Choices {
			known[f] = true
		}
	}
	return known, nil
}
