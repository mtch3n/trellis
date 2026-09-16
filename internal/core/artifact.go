package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

type Artifact struct {
	ID          string `db:"id" json:"id"`
	ProjectID   string `db:"project_id" json:"-"`
	Name        string `db:"name" json:"name"`
	Path        string `db:"path" json:"path"`
	Kind        string `db:"kind" json:"kind"`
	MIME        string `db:"mime" json:"mime"`
	Size        int64  `db:"size" json:"size"`
	ContentHash string `db:"content_hash" json:"content_hash"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
	UpdatedAt   int64  `db:"updated_at" json:"updated_at"`
}

func (c *Core) artifactDir(projectKey string) (string, error) {
	root, err := c.root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "projects", projectKey, "artifacts")
	return dir, os.MkdirAll(dir, 0o700)
}

// CreateArtifact copies a permitted artifact into Trellis storage. The blob
// stays on disk; the database receives only metadata.
func (c *Core) CreateArtifact(ctx context.Context, projectID, source string) (Artifact, error) {
	stat, err := os.Stat(source)
	if err != nil {
		return Artifact{}, err
	}
	if !stat.Mode().IsRegular() {
		return Artifact{}, ErrUsage("artifact_not_regular", "artifact must be a regular file", "trellis artifact add <file>")
	}
	if stat.Size() == 0 {
		return Artifact{}, ErrUsage("artifact_empty", "artifact must not be empty", "trellis artifact add <file>")
	}

	in, err := os.Open(source)
	if err != nil {
		return Artifact{}, err
	}
	defer in.Close()
	header := make([]byte, 512)
	n, _ := io.ReadFull(io.LimitReader(in, 512), header)
	mimeType := http.DetectContentType(header[:n])
	if ext := mime.TypeByExtension(strings.ToLower(filepath.Ext(source))); mimeType == "application/octet-stream" && ext != "" {
		mimeType = ext
	}
	kind := artifactKind(mimeType)
	if kind == "" {
		return Artifact{}, ErrUsage("artifact_type_not_allowed", "only images, PDFs, text, audio, video, and archives are accepted", "trellis artifact add <file>")
	}
	in.Close()

	var out Artifact
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		dir, err := c.artifactDir(key)
		if err != nil {
			return err
		}
		name := filepath.Base(source)
		name = Slugify(strings.TrimSuffix(name, filepath.Ext(name))) + filepath.Ext(name)
		if name == "." || name == "" {
			name = "artifact"
		}
		path := filepath.Join(dir, name)
		for n := 2; ; n++ {
			taken, err := artifactNameTaken(tx, projectID, path)
			if err != nil {
				return err
			}
			if !taken {
				break
			}
			path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, filepath.Ext(name)), n, filepath.Ext(name)))
		}
		if err := copyFile(source, path); err != nil {
			return err
		}
		actual, err := os.Stat(path)
		if err != nil {
			return err
		}
		contentHash, err := fileHash(path)
		if err != nil {
			return err
		}
		now := c.clock.NowMS()
		out = Artifact{ID: NewCardID(), ProjectID: projectID, Name: filepath.Base(path), Path: path, Kind: kind, MIME: mimeType, Size: actual.Size(), ContentHash: contentHash, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.Exec(`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.ProjectID, out.Name, out.Path, out.Kind, out.MIME, out.Size, out.ContentHash, out.CreatedAt, out.UpdatedAt); err != nil {
			return err
		}
		// An entry may already name this artifact, written before it existed.
		// The name is new to the project (artifactNameTaken made sure), so no
		// stub it fills was ambiguous.
		_, err = tx.Exec(
			`UPDATE link SET to_id = ?
			 WHERE to_type = 'artifact' AND rel = 'artifact' AND to_id IS NULL AND to_raw = ?
			   AND from_type = 'doc'
			   AND from_id IN (SELECT id FROM knowledge WHERE project_id = ?)`,
			out.ID, out.Name, projectID)
		return err
	})
	return out, err
}

// artifactNameTaken reports whether a candidate path cannot be used: a file is
// already there, or the project already has an artifact with that name. The
// database check matters because names resolve entries' references, and a
// storage root that has moved leaves rows whose files are elsewhere. An error
// other than "no such file" stops the search rather than looping forever.
func artifactNameTaken(tx *sqlx.Tx, projectID, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	var n int
	if err := tx.Get(&n,
		`SELECT COUNT(*) FROM artifact WHERE project_id = ? AND name = ?`,
		projectID, filepath.Base(path)); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *Core) LinkArtifactToCard(ctx context.Context, projectID string, cardID, artifactID string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var exists int
		if err := tx.Get(&exists, `SELECT COUNT(*) FROM artifact WHERE id = ? AND project_id = ?`, artifactID, projectID); err != nil || exists == 0 {
			if err != nil {
				return err
			}
			return ErrNotFound("artifact_not_found", "artifact not found", "trellis artifact ls")
		}
		if err := tx.Get(&exists, `SELECT COUNT(*) FROM card WHERE id = ? AND project_id = ?`, cardID, projectID); err != nil || exists == 0 {
			if err != nil {
				return err
			}
			return ErrNotFound("card_not_found", "card not found", "trellis card ls")
		}
		_, err := tx.Exec(`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('card', ?, 'artifact', ?, ?, 'artifact')`, cardID, artifactID, artifactID)
		return err
	})
}

// ResolveArtifact finds an artifact by id or by name within a project. Ids are
// UUIDs and names are filenames, so the two cannot be confused. A name shared by
// several artifacts is an error that names them, never a guess.
func (c *Core) ResolveArtifact(ctx context.Context, projectID, ref string) (Artifact, error) {
	var out Artifact
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		err := tx.Get(&out, `SELECT * FROM artifact WHERE project_id = ? AND id = ?`, projectID, ref)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var matches []Artifact
		if err := tx.Select(&matches,
			`SELECT * FROM artifact WHERE project_id = ? AND name = ? ORDER BY created_at`,
			projectID, ref); err != nil {
			return err
		}
		switch len(matches) {
		case 0:
			return ErrNotFound("artifact_not_found", "no artifact "+ref, "trellis artifact ls")
		case 1:
			out = matches[0]
			return nil
		default:
			ids := make([]string, len(matches))
			for i, m := range matches {
				ids[i] = m.ID
			}
			return ErrUsage("artifact_ambiguous",
				"more than one artifact is named "+ref+": "+strings.Join(ids, ", "),
				"trellis artifact rm <id>   # remove the extra ones, then refer to it by name")
		}
	})
	return out, err
}

// LinkArtifactToDoc adds an artifact to an entry's `artifacts` list. The list
// lives in the entry's file, so this is an edit of that file, and the link row
// follows from it. Linking a name already listed changes nothing.
func (c *Core) LinkArtifactToDoc(ctx context.Context, projectID, slug, artifactRef string) (Knowledge, error) {
	a, err := c.ResolveArtifact(ctx, projectID, artifactRef)
	if err != nil {
		return Knowledge{}, err
	}
	return c.editDocArtifacts(ctx, projectID, slug, func(names []string) ([]string, bool) {
		if slices.Contains(names, a.Name) {
			return names, false
		}
		return append(names, a.Name), true
	})
}

// UnlinkArtifactFromDoc removes an artifact from an entry's list. The reference
// is resolved when it can be; when it cannot — the artifact is gone, or its name
// is shared — it is taken as written, so a stub can still be cleared. Removing a
// name that is not listed changes nothing.
func (c *Core) UnlinkArtifactFromDoc(ctx context.Context, projectID, slug, artifactRef string) (Knowledge, error) {
	name := artifactRef
	a, err := c.ResolveArtifact(ctx, projectID, artifactRef)
	switch e, ok := errors.AsType[*Error](err); {
	case err == nil:
		name = a.Name
	case ok && (e.Code == "artifact_not_found" || e.Code == "artifact_ambiguous"):
		// Keep the reference as written.
	default:
		return Knowledge{}, err
	}
	return c.editDocArtifacts(ctx, projectID, slug, func(names []string) ([]string, bool) {
		if !slices.Contains(names, name) {
			return names, false
		}
		return slices.DeleteFunc(names, func(n string) bool { return n == name }), true
	})
}

// editDocArtifacts reads an entry's artifact list from its file, lets change
// produce the next one, and writes it through EditKnowledgeFields. The list is
// read from the file rather than from derived link rows, which can lag the file
// after a database restore; rewriting the file from a lagging copy would drop
// names. IfVersion makes a concurrent edit between the read and the write a
// conflict instead of a lost update.
func (c *Core) editDocArtifacts(ctx context.Context, projectID, slug string,
	change func(names []string) ([]string, bool)) (Knowledge, error) {
	doc, err := c.LoadKnowledge(ctx, projectID, slug)
	if err != nil {
		return Knowledge{}, err
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		return Knowledge{}, err
	}
	fm, _, err := splitDocFile(doc.Path, raw)
	if err != nil {
		return Knowledge{}, err
	}
	next, changed := change(dedupeNames(fm.Artifacts))
	if !changed {
		return doc, nil
	}
	return c.EditKnowledgeFields(ctx, projectID, slug,
		KnowledgeEdit{Artifacts: &next, IfVersion: &doc.Version})
}

// UnlinkArtifactFromCard removes a card's link to an artifact. Removing a link
// that does not exist is not an error.
func (c *Core) UnlinkArtifactFromCard(ctx context.Context, projectID, cardID, artifactID string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(
			`DELETE FROM link
			 WHERE from_type = 'card' AND from_id = ? AND to_type = 'artifact'
			   AND to_id = ? AND rel = 'artifact'`, cardID, artifactID)
		return err
	})
}

// namesAdded returns the names in after that before lacks, in after's order.
func namesAdded(before, after []string) []string {
	var out []string
	for _, n := range after {
		if !slices.Contains(before, n) {
			out = append(out, n)
		}
	}
	return out
}

// ListArtifacts lists a project's artifacts, or only those linked to one card
// or one entry. Unresolved names are not artifacts and are not listed here; an
// entry's stubs appear in its computed Artifacts.
func (c *Core) ListArtifacts(ctx context.Context, projectID, cardID, docID string) ([]Artifact, error) {
	out := []Artifact{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		switch {
		case cardID != "":
			return tx.Select(&out,
				`SELECT a.* FROM artifact a JOIN link l ON l.to_type = 'artifact' AND l.to_id = a.id
				 WHERE a.project_id = ? AND l.from_type = 'card' AND l.from_id = ?
				 ORDER BY a.updated_at DESC`, projectID, cardID)
		case docID != "":
			return tx.Select(&out,
				`SELECT a.* FROM artifact a JOIN link l ON l.to_type = 'artifact' AND l.to_id = a.id
				 WHERE a.project_id = ? AND l.from_type = 'doc' AND l.from_id = ? AND l.rel = 'artifact'
				 ORDER BY l.rowid`, projectID, docID)
		default:
			return tx.Select(&out,
				`SELECT * FROM artifact WHERE project_id = ? ORDER BY updated_at DESC`, projectID)
		}
	})
	return out, err
}

// DeleteArtifact removes metadata, graph links, and the stored file.
func (c *Core) DeleteArtifact(ctx context.Context, projectID, artifactID string) error {
	var path string
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&path, `SELECT path FROM artifact WHERE id = ? AND project_id = ?`, artifactID, projectID); err != nil {
			return ErrNotFound("artifact_not_found", "artifact not found", "trellis artifact ls")
		}
		// An entry names its artifacts in its own file, so its link survives as
		// a stub, the same as a wikilink to a deleted entry. Clearing to_id
		// first keeps the DELETE below, and the artifact_links_ad trigger, from
		// matching it. A card's link lives only in the database and goes.
		if _, err := tx.Exec(
			`UPDATE link SET to_id = NULL
			 WHERE to_type = 'artifact' AND to_id = ? AND from_type = 'doc'`, artifactID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM link WHERE (to_type = 'artifact' AND to_id = ?) OR (from_type = 'artifact' AND from_id = ?)`, artifactID, artifactID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM artifact WHERE id = ? AND project_id = ?`, artifactID, projectID)
		return err
	})
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func artifactKind(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "text/"):
		return "text"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case mimeType == "application/pdf":
		return "document"
	case mimeType == "application/zip", mimeType == "application/gzip", mimeType == "application/x-tar":
		return "archive"
	default:
		return ""
	}
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
