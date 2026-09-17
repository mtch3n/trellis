package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type artifactRow struct {
	ID   string `db:"id"`
	Name string `db:"name"`
	Path string `db:"path"`
}

// artifactMove is what happens to one SRC artifact: it moves under name, or,
// when into is set, collapses into DST's artifact with the same bytes.
type artifactMove struct {
	row  artifactRow
	name string
	into string
}

// planArtifacts applies the document rules to artifacts, by file name.
// There is no vault for artifacts, so every conflict may be renamed.
func (m *merger) planArtifacts() error {
	var src, dst []artifactRow
	if err := m.tx.Select(&src,
		`SELECT id, name, path FROM artifact WHERE project_id = ? ORDER BY name`, m.src.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&dst, `SELECT id, name, path FROM artifact WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	byName, taken := map[string]artifactRow{}, map[string]bool{}
	for _, d := range dst {
		byName[d.Name], taken[d.Name] = d, true
	}
	for _, s := range src {
		taken[s.Name] = true
	}
	out := &m.plan.Artifacts
	dir := filepath.Join(m.root, "projects", m.dst.Key, "artifacts")
	movable := func(s artifactRow, name string) bool {
		if _, err := os.Lstat(s.Path); err != nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Name, Reason: "its file is missing: " + s.Path})
			return false
		}
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			out.Conflicts = append(out.Conflicts,
				MergeConflict{Name: s.Name, Reason: "a file with no artifact is already at " + filepath.Join(dir, name)})
			return false
		}
		return true
	}
	for _, s := range src {
		d, clash := byName[s.Name]
		if !clash {
			if movable(s, s.Name) {
				m.artMoves = append(m.artMoves, artifactMove{row: s, name: s.Name})
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
		case srcHash == dstHash:
			m.artMoves = append(m.artMoves, artifactMove{row: s, name: s.Name, into: d.ID})
			out.Collapsed = append(out.Collapsed, s.Name)
		case !m.opts.RenameConflicts:
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Name, SrcHash: srcHash, DstHash: dstHash})
		default:
			name := freeArtifactName(s.Name, Slugify(m.src.Key), taken)
			taken[name] = true
			if movable(s, name) {
				m.artMoves = append(m.artMoves, artifactMove{row: s, name: name})
				out.Renamed = append(out.Renamed, Rename{From: s.Name, To: name})
			}
		}
	}
	return nil
}

// freeArtifactName suffixes the stem and keeps the extension last:
// shot-api.png, then shot-api-2.png.
func freeArtifactName(name, suffix string, taken map[string]bool) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext) + "-" + suffix
	candidate := stem + ext
	for n := 2; taken[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d%s", stem, n, ext)
	}
	return candidate
}

// moveArtifacts carries out planArtifacts. A collapsed artifact's card links
// move to DST's copy; deleting its row fires artifact_links_ad, which removes
// any link that already pointed at both. Its file stays in SRC's directory,
// which is kept with the backup.
func (m *merger) moveArtifacts() error {
	dir := filepath.Join(m.root, "projects", m.dst.Key, "artifacts")
	for _, mv := range m.artMoves {
		a := mv.row
		if mv.into != "" {
			if _, err := m.tx.Exec(
				`UPDATE OR IGNORE link SET to_id = ?, to_raw = ? WHERE to_type = 'artifact' AND to_id = ?`,
				mv.into, mv.into, a.ID); err != nil {
				return err
			}
			if _, err := m.tx.Exec(`DELETE FROM artifact WHERE id = ?`, a.ID); err != nil {
				return err
			}
			continue
		}
		path := filepath.Join(dir, mv.name)
		if _, err := m.tx.Exec(`UPDATE artifact SET project_id = ?, name = ?, path = ? WHERE id = ?`,
			m.dst.ID, mv.name, path, a.ID); err != nil {
			return err
		}
		if err := m.touch(a.Path); err != nil {
			return err
		}
		if m.apply {
			if err := m.stage.move(a.Path, path); err != nil {
				return err
			}
		}
		m.plan.Artifacts.Moved++
	}
	return nil
}
