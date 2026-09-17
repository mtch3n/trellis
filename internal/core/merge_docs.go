package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/mtch3n/trellis/internal/vpath"
)

type docRow struct {
	ID     string `db:"id"`
	Slug   string `db:"slug"`
	Path   string `db:"path"`
	Global bool   `db:"global"`
}

// docMove is what happens to one SRC document: it moves under slug, or, when
// into is set, it collapses into DST's identical entry.
type docMove struct {
	doc        docRow
	slug       string
	into       string
	intoGlobal bool
}

// planDocs decides every SRC document's fate before anything changes.
// Identical bytes under one slug collapse; different content is a conflict,
// renamed only on request and never for a vault entry.
func (m *merger) planDocs() error {
	var src, dst []docRow
	if err := m.tx.Select(&src,
		`SELECT id, slug, path, global FROM knowledge WHERE project_id = ? ORDER BY slug`, m.src.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&dst,
		`SELECT id, slug, path, global FROM knowledge WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	bySlug, taken := map[string]docRow{}, map[string]bool{}
	for _, d := range dst {
		bySlug[d.Slug], taken[d.Slug] = d, true
	}
	for _, s := range src {
		taken[s.Slug] = true
	}
	out := &m.plan.Knowledge
	dir := filepath.Join(m.root, "projects", m.dst.Key, "knowledge")
	// movable reports whether s can move under slug, recording a conflict
	// when its file is gone or an untracked file already holds the
	// destination. The plan must see what the apply would trip over.
	movable := func(s docRow, slug string) bool {
		if _, err := os.Lstat(s.Path); err != nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Slug, Reason: "its file is missing: " + s.Path})
			return false
		}
		if s.Global {
			return true
		}
		dest := filepath.Join(dir, slug+".md")
		if _, err := os.Lstat(dest); err == nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Slug, Reason: "a file with no entry is already at " + dest})
			return false
		}
		return true
	}
	for _, s := range src {
		m.docPath[s.ID], m.origPath[s.ID], m.fromSrc[s.ID] = s.Path, s.Path, true
		d, clash := bySlug[s.Slug]
		if !clash {
			if movable(s, s.Slug) {
				m.docMoves = append(m.docMoves, docMove{doc: s, slug: s.Slug})
			}
			continue
		}
		srcHash, err := fileHash(s.Path)
		if err != nil {
			return err
		}
		dstHash, err := fileHash(d.Path)
		if err != nil {
			return err
		}
		switch {
		case srcHash == dstHash && !s.Global:
			m.docMoves = append(m.docMoves, docMove{doc: s, slug: s.Slug, into: d.ID, intoGlobal: d.Global})
			out.Collapsed = append(out.Collapsed, s.Slug)
		case s.Global || !m.opts.RenameConflicts:
			out.Conflicts = append(out.Conflicts,
				MergeConflict{Name: s.Slug, SrcHash: srcHash, DstHash: dstHash, Vault: s.Global})
		default:
			slug := freeName(s.Slug+"-"+Slugify(m.src.Key), taken)
			taken[slug] = true
			if movable(s, slug) {
				m.docMoves = append(m.docMoves, docMove{doc: s, slug: slug})
				out.Renamed = append(out.Renamed, Rename{From: s.Slug, To: slug})
			}
		}
	}
	return nil
}

// moveDocs carries out planDocs. A project entry's file moves into DST's
// vault directory; a vault entry stays where it is and only changes origin.
func (m *merger) moveDocs() error {
	dir := filepath.Join(m.root, "projects", m.dst.Key, "knowledge")
	for _, mv := range m.docMoves {
		d := mv.doc
		old := DocAddress(m.src.Key, d.Global, d.Slug)
		if mv.into != "" {
			if err := m.collapseDoc(d, mv.into); err != nil {
				return err
			}
			m.addr[old] = DocAddress(m.dst.Key, mv.intoGlobal, mv.slug)
			continue
		}
		path := d.Path
		if !d.Global {
			path = filepath.Join(dir, mv.slug+".md")
		}
		if _, err := m.tx.Exec(`UPDATE knowledge SET project_id = ?, slug = ?, path = ? WHERE id = ?`,
			m.dst.ID, mv.slug, path, d.ID); err != nil {
			return err
		}
		if path != d.Path {
			// The entry's revisions move with it, one file at a time, so the
			// backup holds each and a failure puts each back.
			revs, err := revisionFiles(d.Path)
			if err != nil {
				return err
			}
			for _, f := range append([]string{d.Path}, revs...) {
				if err := m.touch(f); err != nil {
					return err
				}
			}
			if m.apply {
				if err := m.stage.move(d.Path, path); err != nil {
					return err
				}
				for _, f := range revs {
					if err := m.stage.move(f, filepath.Join(revisionDir(path), filepath.Base(f))); err != nil {
						return err
					}
				}
				m.docPath[d.ID] = path
			}
		}
		if !d.Global {
			m.addr[old] = DocAddress(m.dst.Key, false, mv.slug)
		}
		if mv.slug != d.Slug {
			m.renamed[d.Slug] = mv.slug
		}
		m.plan.Knowledge.Moved++
	}
	return nil
}

// collapseDoc folds a SRC entry into DST's identical one: whatever pointed at
// SRC's row now points at DST's. SRC's file stays in SRC's directory, which
// is kept with the backup after the commit.
func (m *merger) collapseDoc(d docRow, into string) error {
	// One actor's two nominations cannot both survive the unique key: keep
	// DST's row and append SRC's reason to it, so no evidence is lost.
	if _, err := m.tx.Exec(
		`UPDATE nomination AS dn
		 SET reason = dn.reason || char(10) || sn.reason, created_at = min(dn.created_at, sn.created_at)
		 FROM nomination AS sn
		 WHERE sn.knowledge_id = ? AND dn.knowledge_id = ? AND dn.actor = sn.actor AND dn.reason <> sn.reason`,
		d.ID, into); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE link SET to_id = ? WHERE to_type = 'doc' AND to_id = ?`,
		`UPDATE OR IGNORE pin SET knowledge_id = ? WHERE knowledge_id = ?`,
		`UPDATE OR IGNORE nomination SET knowledge_id = ? WHERE knowledge_id = ?`,
	} {
		if _, err := m.tx.Exec(q, into, d.ID); err != nil {
			return err
		}
	}
	if _, err := m.tx.Exec(`DELETE FROM link WHERE from_type = 'doc' AND from_id = ?`, d.ID); err != nil {
		return err
	}
	if _, err := m.tx.Exec(`DELETE FROM knowledge WHERE id = ?`, d.ID); err != nil {
		return err
	}
	delete(m.fromSrc, d.ID)
	return m.c.recordEvent(m.tx, "knowledge", d.ID, "collapsed", "into", "", into)
}

// references rewrites every link that named a SRC document by address, in
// any project, and every relative link from a SRC document to one that was
// renamed. Then it resolves the stubs the merge satisfied.
func (m *merger) references() error {
	if len(m.addr) > 0 {
		prefix := "/" + m.src.Key + "/"
		ids, err := m.docsCiting(m.src.Key)
		if err != nil {
			return err
		}
		if len(m.renamed) > 0 {
			for id := range m.fromSrc {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range slices.Compact(ids) {
			if err := m.rewriteDoc(id); err != nil {
				return err
			}
		}
		if err := m.rewriteCardTargets(prefix); err != nil {
			return err
		}
	}
	return m.resolveStubs()
}

// docsCiting lists every document whose file, as it is on disk now, holds a
// wikilink into project key. The files are the source of truth; link rows lag
// behind an edit made outside Trellis until that document is next read.
func (m *merger) docsCiting(key string) ([]string, error) {
	var docs []struct {
		ID   string `db:"id"`
		Path string `db:"path"`
	}
	if err := m.tx.Select(&docs, `SELECT id, path FROM knowledge ORDER BY id`); err != nil {
		return nil, err
	}
	var ids []string
	for _, d := range docs {
		path := d.Path
		if p, ok := m.docPath[d.ID]; ok {
			path = p
		}
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			m.plan.Warnings = append(m.plan.Warnings, "not searched for links: "+path+" is missing")
			continue
		}
		if err != nil {
			return nil, err
		}
		_, body, _ := SplitFrontmatter(string(raw))
		if slices.ContainsFunc(ParseWikilinks(body), func(r Reference) bool { return r.ProjectKey == key }) {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// rewriteDoc rewrites one document's links through RewriteWikilinks, then
// reloads its row so the link table follows the new text.
func (m *merger) rewriteDoc(id string) error {
	var d struct {
		Key    string `db:"key"`
		Slug   string `db:"slug"`
		Path   string `db:"path"`
		Global bool   `db:"global"`
	}
	if err := m.tx.Get(&d,
		`SELECT p.key, k.slug, k.path, k.global FROM knowledge k JOIN project p ON p.id = k.project_id
		 WHERE k.id = ?`, id); err != nil {
		return err
	}
	current, original := d.Path, d.Path
	if p, ok := m.docPath[id]; ok {
		current, original = p, m.origPath[id]
	}
	raw, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	fromSrc := m.fromSrc[id]
	text := RewriteWikilinks(string(raw), func(ref Reference) (string, bool) {
		_, anchor := vpath.SplitAnchor(ref.Raw)
		if anchor != "" {
			anchor = "#" + anchor
		}
		switch {
		case ref.ProjectKey == m.src.Key:
			to, ok := m.addr[DocAddress(m.src.Key, false, ref.Slug)]
			return to + anchor, ok
		case ref.ProjectKey == "" && fromSrc:
			to, ok := m.renamed[ref.Slug]
			return to + anchor, ok
		}
		return "", false
	})
	if text == string(raw) {
		return nil
	}
	m.plan.DocumentsRewritten = append(m.plan.DocumentsRewritten, DocAddress(d.Key, d.Global, d.Slug))
	if err := m.touch(original); err != nil {
		return err
	}
	if !m.apply {
		return nil
	}
	// A rewrite is a Trellis write: the text it replaces is kept as a
	// revision, like any edit's.
	var doc Knowledge
	if err := m.tx.Get(&doc, `SELECT * FROM knowledge WHERE id = ?`, id); err != nil {
		return err
	}
	if err := m.c.refreshFromFile(m.tx, &doc); err != nil {
		return err
	}
	rev, keep, err := m.c.revisionToKeep(current, doc.Version, raw)
	if err != nil {
		return err
	}
	if keep {
		if err := m.stage.create(rev, raw); err != nil {
			return err
		}
	}
	if err := m.stage.rewrite(current, []byte(text)); err != nil {
		return err
	}
	return m.c.refreshFromFile(m.tx, &doc)
}

// rewriteCardTargets updates `trellis link` targets that named a SRC document
// by address. Their text is all the link keeps of what was typed.
func (m *merger) rewriteCardTargets(prefix string) error {
	var links []struct {
		FromID string `db:"from_id"`
		ToRaw  string `db:"to_raw"`
	}
	if err := m.tx.Select(&links,
		`SELECT from_id, to_raw FROM link
		 WHERE from_type = 'card' AND rel = 'documents' AND upper(substr(to_raw, 1, ?)) = ?`,
		utf8.RuneCountInString(prefix), prefix); err != nil {
		return err
	}
	for _, l := range links {
		ref := ParseReference(l.ToRaw)
		to, ok := m.addr[DocAddress(m.src.Key, false, ref.Slug)]
		if ref.ProjectKey != m.src.Key || !ok {
			continue
		}
		if _, anchor := vpath.SplitAnchor(l.ToRaw); anchor != "" {
			to += "#" + anchor
		}
		if _, err := m.tx.Exec(
			`UPDATE OR IGNORE link SET to_raw = ?
			 WHERE from_type = 'card' AND from_id = ? AND rel = 'documents' AND to_raw = ?`,
			to, l.FromID, l.ToRaw); err != nil {
			return err
		}
	}
	return nil
}

// resolveStubs points dangling links at whatever they name now. SRC's entries
// arrived under DST, so a link from DST that waited for one of them resolves,
// and so does an address to DST written anywhere before its entry arrived.
func (m *merger) resolveStubs() error {
	prefix := "/" + m.dst.Key + "/"
	n := utf8.RuneCountInString(prefix)
	var stubs []struct {
		FromType  string `db:"from_type"`
		FromID    string `db:"from_id"`
		ToRaw     string `db:"to_raw"`
		ProjectID string `db:"project_id"`
	}
	if err := m.tx.Select(&stubs,
		`SELECT l.from_type, l.from_id, l.to_raw, k.project_id
		 FROM link l JOIN knowledge k ON k.id = l.from_id
		 WHERE l.from_type = 'doc' AND l.to_type = 'doc' AND l.to_id IS NULL
		   AND (k.project_id = ? OR upper(substr(l.to_raw, 1, ?)) = ?)
		 UNION ALL
		 SELECT l.from_type, l.from_id, l.to_raw, cd.project_id
		 FROM link l JOIN card cd ON cd.id = l.from_id
		 WHERE l.from_type = 'card' AND l.to_type = 'doc' AND l.to_id IS NULL
		   AND (cd.project_id = ? OR upper(substr(l.to_raw, 1, ?)) = ?)`,
		m.dst.ID, n, prefix, m.dst.ID, n, prefix); err != nil {
		return err
	}
	for _, s := range stubs {
		toID, err := m.c.resolveDocRef(m.tx, s.ProjectID, ParseReference(s.ToRaw))
		if err != nil {
			return err
		}
		id, ok := toID.(string)
		if !ok {
			continue
		}
		if _, err := m.tx.Exec(
			`UPDATE link SET to_id = ?
			 WHERE from_type = ? AND from_id = ? AND to_type = 'doc' AND to_raw = ? AND to_id IS NULL`,
			id, s.FromType, s.FromID, s.ToRaw); err != nil {
			return err
		}
	}
	return nil
}
