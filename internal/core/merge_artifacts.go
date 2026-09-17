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
	// Path is derived, not scanned: see planArtifacts, which fills it in for
	// every row it selects.
	Path string `db:"-"`
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
		`SELECT id, name FROM artifact WHERE project_id = ? ORDER BY name`, m.src.ID); err != nil {
		return err
	}
	if err := m.tx.Select(&dst, `SELECT id, name FROM artifact WHERE project_id = ?`, m.dst.ID); err != nil {
		return err
	}
	for i := range src {
		src[i].Path = m.c.artifactPath(m.src.Key, src[i].Name)
	}
	for i := range dst {
		dst[i].Path = m.c.artifactPath(m.dst.Key, dst[i].Name)
	}
	byName, taken := map[string]artifactRow{}, map[string]bool{}
	for _, d := range dst {
		byName[d.Name], taken[d.Name] = d, true
	}
	for _, s := range src {
		taken[s.Name] = true
	}
	out := &m.plan.Artifacts
	movable := func(s artifactRow, name string) bool {
		if _, err := os.Lstat(s.Path); err != nil {
			out.Conflicts = append(out.Conflicts, MergeConflict{Name: s.Name, Reason: "its file is missing: " + s.Path})
			return false
		}
		dest := m.c.artifactPath(m.dst.Key, name)
		if _, err := os.Lstat(dest); err == nil {
			out.Conflicts = append(out.Conflicts,
				MergeConflict{Name: s.Name, Reason: "a file with no artifact is already at " + dest})
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
				m.artRenamed[s.Name] = name
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
// move to DST's copy; deleting its row fires artifact_links_ad, a safety net
// that finds nothing left, since every link naming it was already re-pointed.
// Its file stays in SRC's directory, which is kept with the backup.
func (m *merger) moveArtifacts() error {
	for _, mv := range m.artMoves {
		a := mv.row
		if mv.into != "" {
			// link's UNIQUE constraint includes anchor, which a card->artifact
			// link never sets, so SQLite never treats two such rows as equal
			// (the same gap 0019 closed for card->card links). A card already
			// linked straight to DST's copy would end up with two identical
			// rows once SRC's copy is re-pointed at the same id; drop SRC's
			// side of that pair first.
			if _, err := m.tx.Exec(
				`DELETE FROM link WHERE from_type = 'card' AND to_type = 'artifact' AND to_id = ? AND rel = 'artifact'
				 AND from_id IN (
				     SELECT from_id FROM link
				     WHERE from_type = 'card' AND to_type = 'artifact' AND to_id = ? AND rel = 'artifact')`,
				a.ID, mv.into); err != nil {
				return err
			}
			// A card link's to_raw is the artifact id and must follow it to
			// the new one. A doc link's to_raw is the artifact's name,
			// resolved within the document's own project by
			// resolveArtifactName, and renaming the id underneath it must not
			// change what the document typed.
			if _, err := m.tx.Exec(
				`UPDATE OR IGNORE link SET to_id = ?,
				        to_raw = CASE WHEN from_type = 'card' THEN ? ELSE to_raw END
				 WHERE to_type = 'artifact' AND to_id = ?`,
				mv.into, mv.into, a.ID); err != nil {
				return err
			}
			if _, err := m.tx.Exec(`DELETE FROM artifact WHERE id = ?`, a.ID); err != nil {
				return err
			}
			continue
		}
		path := m.c.artifactPath(m.dst.Key, mv.name)
		if _, err := m.tx.Exec(`UPDATE artifact SET project_id = ?, name = ? WHERE id = ?`,
			m.dst.ID, mv.name, a.ID); err != nil {
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

// rewriteArtifactNames replaces a renamed artifact's old name with its new
// one wherever text's `artifacts:` frontmatter list names it. planArtifacts
// renames only the artifact's file and row, never the SRC documents that name
// it; left alone, such a name would resolve after the merge to whatever DST
// already has under it (doc_relations.go's resolveArtifactName is scoped to
// the document's own project, which is DST's by the time this runs).
func (m *merger) rewriteArtifactNames(path, text string) (string, error) {
	fm, body, err := splitDocFile(path, []byte(text))
	if err != nil {
		return "", err
	}
	changed := false
	for i, name := range fm.Artifacts {
		if to, ok := m.artRenamed[name]; ok {
			fm.Artifacts[i] = to
			changed = true
		}
	}
	if !changed {
		return text, nil
	}
	return RenderDoc(fm, body), nil
}
