package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mtch3n/trellis/internal/address"
)

type entryRow struct {
	ID     string `db:"id"`
	Slug   string `db:"slug"`
	Global bool   `db:"global"`
	// Path is derived, not scanned: see planEntries, which fills it in for every
	// row it selects.
	Path string `db:"-"`
}

// entryMove is what happens to one SRC entry: it moves under slug, or, when
// into is set, it collapses into DST's identical entry.
type entryMove struct {
	entry      entryRow
	slug       string
	into       string
	intoGlobal bool
}

// planEntries decides every SRC entry's fate before anything changes.
// Identical bytes under one slug collapse; different content is a conflict,
// renamed only on request and never for a vault entry.
func (m *merger) planEntries() error {
	var src, dst []entryRow
	if err := m.tx.Select(&src,
		`SELECT id, slug, global FROM entry WHERE project_id = ? ORDER BY slug`, m.src.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&dst,
		`SELECT id, slug, global FROM entry WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	for i := range src {
		src[i].Path = m.c.entryPath(m.src.Key, src[i].Global, src[i].Slug)
	}
	for i := range dst {
		dst[i].Path = m.c.entryPath(m.dst.Key, dst[i].Global, dst[i].Slug)
	}
	bySlug, taken := map[string]entryRow{}, map[string]bool{}
	for _, e := range dst {
		bySlug[e.Slug], taken[e.Slug] = e, true
	}
	for _, s := range src {
		taken[s.Slug] = true
	}
	out := &m.plan.Entries
	// movable reports whether s can move under slug, recording a conflict
	// when its file is gone or an untracked file already holds the
	// destination. The plan must see what the apply would trip over.
	movable := func(s entryRow, slug string) bool {
		if _, err := os.Lstat(s.Path); err != nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Slug, Reason: "its file is missing: " + s.Path})
			return false
		}
		if s.Global {
			return true
		}
		dest := m.c.entryPath(m.dst.Key, false, slug)
		if _, err := os.Lstat(dest); err == nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Slug, Reason: "a file with no entry is already at " + dest})
			return false
		}
		return true
	}
	for _, s := range src {
		m.entryPath[s.ID], m.origPath[s.ID], m.fromSrc[s.ID] = s.Path, s.Path, true
		e, clash := bySlug[s.Slug]
		if !clash {
			if movable(s, s.Slug) {
				m.entryMoves = append(m.entryMoves, entryMove{entry: s, slug: s.Slug})
			}
			continue
		}
		srcHash, err := fileHash(s.Path)
		if err != nil {
			return err
		}
		dstHash, err := fileHash(e.Path)
		if err != nil {
			return err
		}
		switch {
		case srcHash == dstHash && !s.Global:
			m.entryMoves = append(m.entryMoves, entryMove{entry: s, slug: s.Slug, into: e.ID, intoGlobal: e.Global})
			out.Collapsed = append(out.Collapsed, s.Slug)
		case s.Global || !m.opts.RenameConflicts:
			out.Conflicts = append(out.Conflicts,
				MergeConflict{Name: s.Slug, SrcHash: srcHash, DstHash: dstHash, Vault: s.Global})
		default:
			slug := freeName(s.Slug+"-"+Slugify(m.src.Key), taken)
			taken[slug] = true
			if movable(s, slug) {
				m.entryMoves = append(m.entryMoves, entryMove{entry: s, slug: slug})
				out.Renamed = append(out.Renamed, Rename{From: s.Slug, To: slug})
			}
		}
	}
	return nil
}

// moveEntries carries out planEntries. A project entry's file moves into DST's
// vault directory; a vault entry stays where it is and only changes origin.
func (m *merger) moveEntries() error {
	for _, mv := range m.entryMoves {
		e := mv.entry
		old := EntryAddress(m.src.Key, e.Global, e.Slug)
		if mv.into != "" {
			if err := m.collapseEntry(e, mv.into); err != nil {
				return err
			}
			m.addr[old] = EntryAddress(m.dst.Key, mv.intoGlobal, mv.slug)
			continue
		}
		path := e.Path
		if !e.Global {
			path = m.c.entryPath(m.dst.Key, false, mv.slug)
		}
		if _, err := m.tx.Exec(`UPDATE entry SET project_id = ?, slug = ? WHERE id = ?`,
			m.dst.ID, mv.slug, e.ID); err != nil {
			return err
		}
		if path != e.Path {
			// The entry's revisions move with it, one file at a time, so the
			// backup holds each and a failure puts each back.
			revs, err := revisionFiles(e.Path)
			if err != nil {
				return err
			}
			for _, f := range append([]string{e.Path}, revs...) {
				if err := m.touch(f); err != nil {
					return err
				}
			}
			if m.apply {
				if err := m.stage.move(e.Path, path); err != nil {
					return err
				}
				for _, f := range revs {
					if err := m.stage.move(f, filepath.Join(revisionDir(path), filepath.Base(f))); err != nil {
						return err
					}
				}
				m.entryPath[e.ID] = path
			}
		}
		if !e.Global {
			m.addr[old] = EntryAddress(m.dst.Key, false, mv.slug)
		}
		if mv.slug != e.Slug {
			m.renamed[e.Slug] = mv.slug
		}
		m.plan.Entries.Moved++
	}
	return nil
}

// collapseEntry folds a SRC entry into DST's identical one: whatever pointed at
// SRC's row now points at DST's. SRC's file stays in SRC's directory, which
// is kept with the backup after the commit.
func (m *merger) collapseEntry(e entryRow, into string) error {
	// One actor's two nominations cannot both survive the unique key: keep
	// DST's row and append SRC's reason to it, so no evidence is lost.
	if _, err := m.tx.Exec(
		`UPDATE nomination AS dn
		 SET reason = dn.reason || char(10) || sn.reason, created_at = min(dn.created_at, sn.created_at)
		 FROM nomination AS sn
		 WHERE sn.entry_id = ? AND dn.entry_id = ? AND dn.actor = sn.actor AND dn.reason <> sn.reason`,
		e.ID, into); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE link SET to_id = ? WHERE to_type = 'entry' AND to_id = ?`,
		`UPDATE OR IGNORE pin SET entry_id = ? WHERE entry_id = ?`,
		`UPDATE OR IGNORE nomination SET entry_id = ? WHERE entry_id = ?`,
	} {
		if _, err := m.tx.Exec(q, into, e.ID); err != nil {
			return err
		}
	}
	// recordEvent looks up its project_id from the entry row itself, so it
	// must run before that row is gone -- otherwise the event lands with a
	// NULL project_id, which retire()'s later re-homing (WHERE project_id =
	// src) does not match either, and it never reaches SRC's or DST's events.
	if err := m.c.recordEvent(m.tx, "entry", e.ID, "collapsed", "into", "", into); err != nil {
		return err
	}
	if _, err := m.tx.Exec(`DELETE FROM link WHERE from_type = 'entry' AND from_id = ?`, e.ID); err != nil {
		return err
	}
	if _, err := m.tx.Exec(`DELETE FROM entry WHERE id = ?`, e.ID); err != nil {
		return err
	}
	delete(m.fromSrc, e.ID)
	return nil
}

// references rewrites every link that named a SRC entry by address, in
// any project, and every relative link from a SRC entry to one that was
// renamed. Then it resolves the stubs the merge satisfied.
func (m *merger) references() error {
	// A SRC with no entries still has artifacts and cards that sources:
	// items cite, so the scan runs whatever SRC holds.
	ids, err := m.entriesCiting(m.src.Key)
	if err != nil {
		return err
	}
	if len(m.renamed) > 0 || len(m.artRenamed) > 0 {
		for id := range m.fromSrc {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if err := m.rewriteEntry(id); err != nil {
			return err
		}
	}
	if err := m.rewriteCardTargets("/" + m.src.Key + "/"); err != nil {
		return err
	}
	return m.resolveStubs()
}

// entriesCiting lists every entry whose file, as it is on disk now, holds a
// wikilink into project key, or a sources: address under it. The files are
// the source of truth; link rows lag behind an edit made outside Trellis
// until that entry is next read, and sources: addresses have no row at
// all.
func (m *merger) entriesCiting(key string) ([]string, error) {
	var entries []struct {
		ID     string `db:"id"`
		Slug   string `db:"slug"`
		Global bool   `db:"global"`
		Key    string `db:"pkey"`
	}
	if err := m.tx.Select(&entries,
		`SELECT k.id, k.slug, k.global, p.key AS pkey FROM entry k
		 JOIN project p ON p.id = k.project_id ORDER BY k.id`); err != nil {
		return nil, err
	}
	var ids []string
	for _, d := range entries {
		path := m.c.entryPath(d.Key, d.Global, d.Slug)
		if p, ok := m.entryPath[d.ID]; ok {
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
		fm, body, _ := SplitFrontmatter(string(raw))
		cites := slices.ContainsFunc(ParseWikilinks(body), func(r Reference) bool { return r.ProjectKey == key })
		if !cites {
			cites = slices.ContainsFunc(fm.Sources, func(s string) bool {
				_, ok := m.rewriteSourceAddress(s)
				return ok
			})
		}
		if cites {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// rewriteSourceAddress rewrites one sources: item that names something under
// SRC by absolute address -- the same objects a wikilink, a card's
// `cites` link, or an artifacts: entry already follow through the merge:
// an entry (moved, renamed, or collapsed into DST's identical
// entry), an artifact (moved, or renamed on conflict), or a card, whose
// stored ref never changes. Anything else -- a URL, prose, a path:lines
// pointer, a wikilink, or an address elsewhere -- is left alone.
func (m *merger) rewriteSourceAddress(raw string) (string, bool) {
	target, anchor := address.SplitAnchor(strings.TrimSpace(raw))
	if anchor != "" {
		anchor = "#" + anchor
	}
	p, err := address.Parse(target)
	if err != nil || p.Project != m.src.Key {
		return "", false
	}
	switch p.Collection {
	case address.CollectionVault:
		to, ok := m.addr[EntryAddress(m.src.Key, false, p.Name)]
		if !ok {
			return "", false
		}
		return to + anchor, true
	case address.CollectionArtifacts:
		name := p.Name
		if to, ok := m.artRenamed[name]; ok {
			name = to
		}
		return ArtifactAddress(m.dst.Key, name) + anchor, true
	case address.CollectionCards:
		return address.Card(m.dst.Key, p.Name).String() + anchor, true
	default:
		return "", false
	}
}

// rewriteSources rewrites an entry's sources: frontmatter through
// rewriteSourceAddress. It runs on text, not raw, so it composes with
// whatever the wikilink and artifact-name rewrites already applied to this
// pass.
func (m *merger) rewriteSources(path, text string) (string, error) {
	fm, body, err := splitEntryFile(path, []byte(text))
	if err != nil {
		return "", err
	}
	changed := false
	for i, s := range fm.Sources {
		if to, ok := m.rewriteSourceAddress(s); ok {
			fm.Sources[i] = to
			changed = true
		}
	}
	if !changed {
		return text, nil
	}
	return RenderEntry(fm, body), nil
}

// rewriteEntry rewrites one entry's links through RewriteWikilinks, then
// reloads its row so the link table follows the new text.
func (m *merger) rewriteEntry(id string) error {
	var d struct {
		Key    string `db:"key"`
		Slug   string `db:"slug"`
		Global bool   `db:"global"`
	}
	if err := m.tx.Get(&d,
		`SELECT p.key, k.slug, k.global FROM entry k JOIN project p ON p.id = k.project_id
		 WHERE k.id = ?`, id); err != nil {
		return err
	}
	entryPath := m.c.entryPath(d.Key, d.Global, d.Slug)
	current, original := entryPath, entryPath
	if p, ok := m.entryPath[id]; ok {
		current, original = p, m.origPath[id]
	}
	raw, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	fromSrc := m.fromSrc[id]
	text := RewriteWikilinks(string(raw), func(ref Reference) (string, bool) {
		_, anchor := address.SplitAnchor(ref.Raw)
		if anchor != "" {
			anchor = "#" + anchor
		}
		switch {
		case ref.ProjectKey == m.src.Key:
			to, ok := m.addr[EntryAddress(m.src.Key, false, ref.Slug)]
			return to + anchor, ok
		case ref.ProjectKey == "" && fromSrc:
			to, ok := m.renamed[ref.Slug]
			return to + anchor, ok
		}
		return "", false
	})
	if fromSrc && len(m.artRenamed) > 0 {
		next, err := m.rewriteArtifactNames(current, text)
		if err != nil {
			return err
		}
		text = next
	}
	text, err = m.rewriteSources(current, text)
	if err != nil {
		return err
	}
	if text == string(raw) {
		return nil
	}
	m.plan.EntriesRewritten = append(m.plan.EntriesRewritten, EntryAddress(d.Key, d.Global, d.Slug))
	if err := m.touch(original); err != nil {
		return err
	}
	if !m.apply {
		return nil
	}
	// A rewrite is a Trellis write: the text it replaces is kept as a
	// revision, like any edit's.
	var entry Entry
	if err := m.tx.Get(&entry, `SELECT * FROM entry WHERE id = ?`, id); err != nil {
		return err
	}
	if err := m.c.refreshFromFile(m.tx, &entry); err != nil {
		return err
	}
	rev, keep, err := m.c.revisionToKeep(current, entry.Version, raw)
	if err != nil {
		return err
	}
	if keep {
		if err := m.stage.create(rev, raw); err != nil {
			return err
		}
	}
	// The refreshFromFile below treats the write as an external edit and
	// would, on its own, record the version it assigns by writing straight to
	// disk -- a write this merge's stage never sees and so cannot undo.
	// Staging that file here first, under the version refreshFromFile is
	// about to assign, makes its own capture a no-op (revisionToKeep skips a
	// destination that already exists) and keeps the write inside the undo.
	nextRev, nextKeep, err := m.c.revisionToKeep(current, entry.Version+1, []byte(text))
	if err != nil {
		return err
	}
	if nextKeep {
		if err := m.stage.create(nextRev, []byte(text)); err != nil {
			return err
		}
	}
	if err := m.stage.rewrite(current, []byte(text)); err != nil {
		return err
	}
	return m.c.refreshFromFile(m.tx, &entry)
}

// rewriteCardTargets updates `trellis link` targets that named a SRC entry
// by address. Their text is all the link keeps of what was typed.
func (m *merger) rewriteCardTargets(prefix string) error {
	var links []struct {
		FromID string `db:"from_id"`
		ToRaw  string `db:"to_raw"`
	}
	if err := m.tx.Select(&links,
		`SELECT from_id, to_raw FROM link
		 WHERE from_type = 'card' AND rel = 'cites' AND upper(substr(to_raw, 1, ?)) = ?`,
		utf8.RuneCountInString(prefix), prefix); err != nil {
		return err
	}
	for _, l := range links {
		ref := ParseReference(l.ToRaw)
		to, ok := m.addr[EntryAddress(m.src.Key, false, ref.Slug)]
		if ref.ProjectKey != m.src.Key || !ok {
			continue
		}
		if _, anchor := address.SplitAnchor(l.ToRaw); anchor != "" {
			to += "#" + anchor
		}
		if _, err := m.tx.Exec(
			`UPDATE OR IGNORE link SET to_raw = ?
			 WHERE from_type = 'card' AND from_id = ? AND rel = 'cites' AND to_raw = ?`,
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
		 FROM link l JOIN entry k ON k.id = l.from_id
		 WHERE l.from_type = 'entry' AND l.to_type = 'entry' AND l.to_id IS NULL
		   AND (k.project_id = ? OR upper(substr(l.to_raw, 1, ?)) = ?)
		 UNION ALL
		 SELECT l.from_type, l.from_id, l.to_raw, cd.project_id
		 FROM link l JOIN card cd ON cd.id = l.from_id
		 WHERE l.from_type = 'card' AND l.to_type = 'entry' AND l.to_id IS NULL
		   AND (cd.project_id = ? OR upper(substr(l.to_raw, 1, ?)) = ?)`,
		m.dst.ID, n, prefix, m.dst.ID, n, prefix); err != nil {
		return err
	}
	for _, s := range stubs {
		toID, err := m.c.resolveEntryRef(m.tx, s.ProjectID, ParseReference(s.ToRaw))
		if err != nil {
			return err
		}
		id, ok := toID.(string)
		if !ok {
			continue
		}
		if _, err := m.tx.Exec(
			`UPDATE link SET to_id = ?
			 WHERE from_type = ? AND from_id = ? AND to_type = 'entry' AND to_raw = ? AND to_id IS NULL`,
			id, s.FromType, s.FromID, s.ToRaw); err != nil {
			return err
		}
	}
	return nil
}
