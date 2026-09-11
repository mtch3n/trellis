package core

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

// syncDocRelations rewrites everything derived from a doc's text: its
// wikilinks, its inline #tags and its frontmatter tags and labels. Derived data
// is replaced rather than merged, because the file is the record: a link deleted
// from the text must disappear from the graph.
func (c *Core) syncDocRelations(tx *sqlx.Tx, doc *Knowledge, fm Frontmatter, body string) error {
	if _, err := tx.Exec(
		`DELETE FROM link WHERE from_type = 'doc' AND from_id = ? AND rel = 'wikilink'`, doc.ID); err != nil {
		return err
	}
	for _, ref := range ParseWikilinks(body) {
		toID, err := c.resolveDocRef(tx, doc.ProjectID, ref)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('doc', ?, 'doc', ?, ?, ?, 'wikilink')`,
			doc.ID, toID, ref.Raw, nullIfEmpty(ref.Anchor)); err != nil {
			return err
		}
	}

	// Tags come from both the frontmatter and the body; labels only from the
	// frontmatter, because a label is a controlled vocabulary and inventing one
	// mid-sentence is how vocabularies rot.
	if _, err := tx.Exec(`DELETE FROM knowledge_tag WHERE doc_id = ?`, doc.ID); err != nil {
		return err
	}
	tags := append(append([]string{}, fm.Tags...), ParseInlineTags(body)...)
	for _, name := range dedupe(tags) {
		tag, err := c.CreateOrGetTag(tx, doc.ProjectID, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO knowledge_tag (doc_id, tag_id) VALUES (?, ?)`, doc.ID, tag.ID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DELETE FROM knowledge_label WHERE doc_id = ?`, doc.ID); err != nil {
		return err
	}
	for _, name := range dedupe(fm.Labels) {
		label, err := c.getLabelTx(tx, doc.ProjectID, name)
		if err != nil {
			return ErrNotFound("label_not_found", "no label "+name+" in this project",
				`trellis label new `+name+` --description "..."`)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO knowledge_label (doc_id, label_id) VALUES (?, ?)`, doc.ID, label.ID); err != nil {
			return err
		}
	}
	return nil
}

// resolveDocRef turns a reference into a doc id, or NULL for a stub. An
// unresolved link is listed by `knowledge lint`, never an error: writing a link
// to something not yet written is how a vault gets built.
//
// A qualified reference to another project resolves only when that project is
// GLOBAL (§10.6.1): search discovers across projects, links never depend across
// them.
func (c *Core) resolveDocRef(tx *sqlx.Tx, projectID string, ref Reference) (any, error) {
	q := `SELECT id FROM knowledge WHERE slug = ? AND project_id = ?`
	args := []any{ref.Slug, projectID}
	switch ref.ProjectKey {
	case "":
	case GlobalKey:
		q = `SELECT id FROM knowledge WHERE slug = ? AND global = 1`
		args = []any{ref.Slug}
	default:
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return nil, err
		}
		if !strings.EqualFold(key, ref.ProjectKey) {
			return nil, nil // another project: a stub, deliberately unresolvable
		}
	}
	var id string
	err := tx.Get(&id, q, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return id, nil
}

// Backlink is one inbound reference to a doc.
type Backlink struct {
	FromType string `db:"from_type" json:"from_type"`
	Ref      string `db:"ref" json:"ref"`
	Title    string `db:"title" json:"title"`
	Anchor   string `db:"anchor" json:"anchor,omitempty"`
}

// Backlinks answers "what points here" for cards and docs alike.
func (c *Core) Backlinks(ctx context.Context, docID string) ([]Backlink, error) {
	out := []Backlink{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&out,
			`SELECT 'doc' AS from_type, p.key || '/' || k.slug AS ref, k.title,
			        COALESCE(l.anchor, '') AS anchor
			 FROM link l JOIN knowledge k ON k.id = l.from_id
			 JOIN project p ON p.id = k.project_id
			 WHERE l.to_type = 'doc' AND l.to_id = ? AND l.from_type = 'doc'
			 UNION ALL
			 SELECT 'card', p.key || '-' || c.seq, c.title, COALESCE(l.anchor, '')
			 FROM link l JOIN card c ON c.id = l.from_id
			 JOIN project p ON p.id = c.project_id
			 WHERE l.to_type = 'doc' AND l.to_id = ? AND l.from_type = 'card'
			 ORDER BY ref`, docID, docID)
	})
	return out, err
}

// LinkCardToDoc is the structured card-to-doc relationship (§10.2):
// trellis link XPSCTL-12 design#concurrency
func (c *Core) LinkCardToDoc(ctx context.Context, projectID string, cardRef CardRef, target string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card Card
		if err := c.loadCard(tx, projectID, cardRef, &card); err != nil {
			return err
		}
		slug, anchor, _ := strings.Cut(target, "#")
		ref := Reference{Raw: target, Slug: Slugify(slug), Anchor: Slugify(anchor)}
		if key, rest, ok := strings.Cut(slug, "/"); ok {
			ref.ProjectKey, ref.Slug = strings.ToUpper(key), Slugify(rest)
		}
		toID, err := c.resolveDocRef(tx, projectID, ref)
		if err != nil {
			return err
		}
		if toID == nil {
			return ErrNotFound("knowledge_not_found", "no knowledge entry "+slug,
				`trellis knowledge new --title "..."`)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('card', ?, 'doc', ?, ?, ?, 'documents')`,
			card.ID, toID, target, nullIfEmpty(ref.Anchor)); err != nil {
			return err
		}
		return c.recordEvent(tx, "card", card.ID, "linked", "documents", "", target)
	})
}

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

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// resolveDocStubs backfills inbound links that were left dangling because the
// target did not exist when they were written. Creating an entry is what turns
// a stub into an edge: without this, a reference written ahead of its target —
// or one orphaned by a delete and then re-created — would stay a lint finding
// forever (§10.4).
func (c *Core) resolveDocStubs(tx *sqlx.Tx, doc *Knowledge) error {
	type stub struct {
		FromType  string `db:"from_type"`
		FromID    string `db:"from_id"`
		ToRaw     string `db:"to_raw"`
		ProjectID string `db:"project_id"`
	}
	stubs := []stub{}
	if err := tx.Select(&stubs,
		`SELECT l.from_type, l.from_id, l.to_raw, k.project_id
		 FROM link l JOIN knowledge k ON k.id = l.from_id
		 WHERE l.to_type = 'doc' AND l.to_id IS NULL AND l.from_type = 'doc'
		 UNION ALL
		 SELECT l.from_type, l.from_id, l.to_raw, cd.project_id
		 FROM link l JOIN card cd ON cd.id = l.from_id
		 WHERE l.to_type = 'doc' AND l.to_id IS NULL AND l.from_type = 'card'`); err != nil {
		return err
	}
	for _, s := range stubs {
		if s.FromID == doc.ID {
			continue // a doc referring to itself before it existed
		}
		toID, err := c.resolveDocRef(tx, s.ProjectID, ParseReference(s.ToRaw))
		if err != nil {
			return err
		}
		// Only the entry just created may claim a stub. Any other resolution
		// belongs to a link that was never dangling in the first place.
		if id, ok := toID.(string); !ok || id != doc.ID {
			continue
		}
		if _, err := tx.Exec(
			`UPDATE link SET to_id = ?
			 WHERE from_type = ? AND from_id = ? AND to_type = 'doc'
			   AND to_raw = ? AND to_id IS NULL`,
			doc.ID, s.FromType, s.FromID, s.ToRaw); err != nil {
			return err
		}
	}
	return nil
}
