package core

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
)

// syncEntryRelations rewrites everything derived from an entry's text: its
// wikilinks, its inline #tags and its frontmatter tags and labels. Derived data
// is replaced rather than merged, because the file is the record: a link deleted
// from the text must disappear from the graph.
func (c *Core) syncEntryRelations(tx *sqlx.Tx, entry *Entry, fm Frontmatter, body string) error {
	if _, err := tx.Exec(
		`DELETE FROM link WHERE from_type = 'entry' AND from_id = ? AND rel = 'wikilink'`, entry.ID); err != nil {
		return err
	}
	for _, ref := range ParseWikilinks(body) {
		toID, err := c.resolveEntryRef(tx, entry.ProjectID, ref)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('entry', ?, 'entry', ?, ?, ?, 'wikilink')`,
			entry.ID, toID, ref.Raw, nullIfEmpty(ref.Anchor)); err != nil {
			return err
		}
	}

	// Artifacts are named in the frontmatter and resolved by name within the
	// entry's project. Replaced wholesale, like wikilinks, because the file is
	// the record. The names are deduplicated here rather than by the UNIQUE
	// constraint, which cannot collapse rows whose anchor is NULL.
	if _, err := tx.Exec(
		`DELETE FROM link WHERE from_type = 'entry' AND from_id = ? AND rel = 'artifact'`, entry.ID); err != nil {
		return err
	}
	for _, name := range dedupeNames(fm.Artifacts) {
		toID, err := c.resolveArtifactName(tx, entry.ProjectID, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel)
			 VALUES ('entry', ?, 'artifact', ?, ?, 'artifact')`,
			entry.ID, toID, name); err != nil {
			return err
		}
	}

	// Tags come from both the frontmatter and the body; labels only from the
	// frontmatter, because a label is a controlled vocabulary and inventing one
	// mid-sentence is how vocabularies rot.
	if _, err := tx.Exec(`DELETE FROM entry_tag WHERE entry_id = ?`, entry.ID); err != nil {
		return err
	}
	tags := append(append([]string{}, fm.Tags...), ParseInlineTags(body)...)
	for _, name := range dedupe(tags) {
		tag, err := c.CreateOrGetTag(tx, entry.ProjectID, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO entry_tag (entry_id, tag_id) VALUES (?, ?)`, entry.ID, tag.ID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DELETE FROM entry_label WHERE entry_id = ?`, entry.ID); err != nil {
		return err
	}
	for _, name := range dedupe(fm.Labels) {
		label, err := c.getLabelTx(tx, entry.ProjectID, name)
		if err != nil {
			return ErrNotFound("label_not_found", "no label "+name+" in this project",
				`trellis label new `+name+` --description "..."`)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO entry_label (entry_id, label_id) VALUES (?, ?)`, entry.ID, label.ID); err != nil {
			return err
		}
	}
	return nil
}

// resolveEntryRef turns a reference into an entry id, or NULL for a stub. An
// unresolved link is listed by `knowledge lint`, never an error: writing a link
// to something not yet written is how a vault gets built.
//
// A relative reference resolves in the source's project first and in the
// vault second, the order loadEntry uses for a relative argument. An address
// resolves wherever it points, another project included: the link names its
// target exactly, and reading that project by name is already allowed. A
// project or entry that does not exist yet leaves a stub, which
// resolveEntryStubs fills in when the entry is created or escalated.
func (c *Core) resolveEntryRef(tx *sqlx.Tx, projectID string, ref Reference) (any, error) {
	var q string
	var args []any
	switch ref.ProjectKey {
	case "":
		q = `SELECT id FROM entry WHERE slug = ? AND (project_id = ? OR global = 1)
		     ORDER BY global LIMIT 1`
		args = []any{ref.Slug, projectID}
	case GlobalKey:
		q, args = `SELECT id FROM entry WHERE slug = ? AND global = 1`, []any{ref.Slug}
	default:
		q = `SELECT k.id FROM entry k JOIN project p ON p.id = k.project_id
		     WHERE k.slug = ? AND p.key = ? AND k.global = 0`
		args = []any{ref.Slug, ref.ProjectKey}
	}
	var id string
	err := tx.Get(&id, q, args...)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Bare-leaf fallback: only for an unqualified reference that does not
	// already name a directory. A unique match resolves; more than one stays
	// a stub, which Lint reports as ambiguous_link rather than stub. If
	// addresses removed Reference.ProjectKey entirely for a relative
	// target (rather than leaving it always ""), drop that half of the
	// condition — every non-absolute reference reaching this point is
	// unqualified by construction.
	if ref.ProjectKey == "" && !strings.Contains(ref.Slug, "/") {
		matches, err := entriesWithLeaf(tx, projectID, ref.Slug)
		if err != nil {
			return nil, err
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return nil, nil
}

// entriesWithLeaf lists the project's entries inside a directory whose last
// path segment is leaf.
func entriesWithLeaf(tx *sqlx.Tx, projectID, leaf string) ([]string, error) {
	ids := []string{}
	err := tx.Select(&ids, `SELECT id FROM entry
		WHERE project_id = ? AND substr(slug, -(length(?) + 1)) = '/' || ?`, projectID, leaf, leaf)
	return ids, err
}

// Backlink is one inbound reference to an entry.
type Backlink struct {
	FromType string `db:"from_type" json:"from_type"`
	Ref      string `db:"ref" json:"ref"`
	Title    string `db:"title" json:"title"`
	Anchor   string `db:"anchor" json:"anchor,omitempty"`
}

// Backlinks answers "what points here" for cards and entries alike.
func (c *Core) Backlinks(ctx context.Context, entryID string) ([]Backlink, error) {
	out := []Backlink{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&out,
			`SELECT 'entry' AS from_type, `+entryAddressSQL+` AS ref, k.title,
			        COALESCE(l.anchor, '') AS anchor
			 FROM link l JOIN entry k ON k.id = l.from_id
			 JOIN project p ON p.id = k.project_id
			 WHERE l.to_type = 'entry' AND l.to_id = ? AND l.from_type = 'entry'
			 UNION ALL
			 SELECT 'card', c.ref, c.title, COALESCE(l.anchor, '')
			 FROM link l JOIN card c ON c.id = l.from_id
			 WHERE l.to_type = 'entry' AND l.to_id = ? AND l.from_type = 'card'
			 ORDER BY ref`, entryID, entryID)
	})
	return out, err
}

// LinkCardToEntry is the structured card-to-entry relationship (§10.2):
//
//	trellis link XPSCTL-12 design#concurrency
//	trellis link XPSCTL-12 /OTHER/vault/runbook#rollback
//
// The target may be in another project; link rows carry no foreign key, and
// wikilinks cross projects too.
func (c *Core) LinkCardToEntry(ctx context.Context, projectID string, cardRef CardRef, target string) error {
	if t, _ := address.SplitAnchor(strings.TrimSpace(target)); strings.HasPrefix(strings.TrimSpace(t), "/") {
		if _, err := ParseAddress(strings.TrimSpace(t), address.CollectionVault); err != nil {
			return err
		}
	}
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card Card
		if err := c.loadCard(tx, projectID, cardRef, &card); err != nil {
			return err
		}
		ref := ParseReference(target)
		toID, err := c.resolveEntryRef(tx, projectID, ref)
		if err != nil {
			return err
		}
		if toID == nil {
			return ErrNotFound("knowledge_not_found", "no entry "+ref.Raw,
				`trellis knowledge new --title "..."`)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
			 VALUES ('card', ?, 'entry', ?, ?, ?, 'cites')`,
			card.ID, toID, ref.Raw, nullIfEmpty(ref.Anchor)); err != nil {
			return err
		}
		return c.recordEvent(tx, "card", card.ID, "linked", "cites", "", ref.Raw)
	})
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

// resolveEntryStubs backfills inbound links that were left dangling because the
// target did not exist when they were written. Creating an entry is what turns
// a stub into an edge: without this, a reference written ahead of its target —
// or one orphaned by a delete and then re-created — would stay a lint finding
// forever (§10.4).
func (c *Core) resolveEntryStubs(tx *sqlx.Tx, entry *Entry) error {
	type stub struct {
		FromType  string `db:"from_type"`
		FromID    string `db:"from_id"`
		ToRaw     string `db:"to_raw"`
		ProjectID string `db:"project_id"`
	}
	stubs := []stub{}
	if err := tx.Select(&stubs,
		`SELECT l.from_type, l.from_id, l.to_raw, k.project_id
		 FROM link l JOIN entry k ON k.id = l.from_id
		 WHERE l.to_type = 'entry' AND l.to_id IS NULL AND l.from_type = 'entry'
		 UNION ALL
		 SELECT l.from_type, l.from_id, l.to_raw, cd.project_id
		 FROM link l JOIN card cd ON cd.id = l.from_id
		 WHERE l.to_type = 'entry' AND l.to_id IS NULL AND l.from_type = 'card'`); err != nil {
		return err
	}
	for _, s := range stubs {
		if s.FromID == entry.ID {
			continue // an entry referring to itself before it existed
		}
		toID, err := c.resolveEntryRef(tx, s.ProjectID, ParseReference(s.ToRaw))
		if err != nil {
			return err
		}
		// Only the entry just created may claim a stub. Any other resolution
		// belongs to a link that was never dangling in the first place.
		if id, ok := toID.(string); !ok || id != entry.ID {
			continue
		}
		if _, err := tx.Exec(
			`UPDATE link SET to_id = ?
			 WHERE from_type = ? AND from_id = ? AND to_type = 'entry'
			   AND to_raw = ? AND to_id IS NULL`,
			entry.ID, s.FromType, s.FromID, s.ToRaw); err != nil {
			return err
		}
	}
	return nil
}

// resolveArtifactName returns the id of the project's artifact with this name,
// or nil — a stub — when there is none.
func (c *Core) resolveArtifactName(tx *sqlx.Tx, projectID, name string) (any, error) {
	var id string
	err := tx.Get(&id, `SELECT id FROM artifact WHERE project_id = ? AND name = ?`, projectID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return id, nil
}

// dedupeNames trims and deduplicates while keeping case and order. It must not
// lower-case, unlike dedupe: an artifact name keeps its extension's case
// ("photo.PNG"), and a lower-cased name would never resolve.
func dedupeNames(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// EntryLink is a link out of an entry. From and To are
// canonical addresses; To is nil for a stub.
type EntryLink struct {
	From   string  `json:"from"`
	To     *string `json:"to"`
	Raw    string  `json:"raw"`
	Anchor string  `json:"anchor"`
}

// EntryLinks lists the links out of a project's entries,
// including the ones it escalated to the vault, ordered by source and then raw
// target. A private entry's links are left out: where it links says what it
// is about. The flag is read from the files, not the mirror.
func (c *Core) EntryLinks(ctx context.Context, projectID string) ([]EntryLink, error) {
	links := []EntryLink{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var rows []struct {
			FromID string  `db:"from_id"`
			From   string  `db:"from_addr"`
			To     *string `db:"to_addr"`
			Raw    string  `db:"to_raw"`
			Anchor string  `db:"anchor"`
		}
		if err := tx.Select(&rows, `
			SELECT l.from_id,
			       '/' || CASE WHEN f.global = 1 THEN '`+GlobalKey+`' ELSE fp.key END || '/vault/' || f.slug AS from_addr,
			       CASE WHEN t.id IS NULL THEN NULL
			            ELSE '/' || CASE WHEN t.global = 1 THEN '`+GlobalKey+`' ELSE tp.key END || '/vault/' || t.slug
			       END AS to_addr,
			       l.to_raw, COALESCE(l.anchor, '') AS anchor
			FROM link l
			JOIN entry f ON f.id = l.from_id
			JOIN project fp ON fp.id = f.project_id
			LEFT JOIN entry t ON t.id = l.to_id
			LEFT JOIN project tp ON tp.id = t.project_id
			WHERE l.from_type = 'entry' AND l.to_type = 'entry' AND f.project_id = ?`, projectID); err != nil {
			return err
		}
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.FromID)
		}
		private, _, err := c.privateAfterRefresh(tx, slices.Compact(slices.Sorted(slices.Values(ids))))
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !private[r.FromID] {
				links = append(links, EntryLink{From: r.From, To: r.To, Raw: r.Raw, Anchor: r.Anchor})
			}
		}
		return nil
	})
	slices.SortFunc(links, func(a, b EntryLink) int {
		return cmp.Or(strings.Compare(a.From, b.From), strings.Compare(a.Raw, b.Raw))
	})
	return links, err
}
