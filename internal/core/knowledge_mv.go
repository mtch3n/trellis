package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// revisionDirFor is where the revision-history design keeps an entry's old
// versions: a hidden directory beside the file, named after the file
// including its extension ("standup.md" -> ".standup.md/"). It has no
// database row of its own, so it is never mistaken for an entry -- Slugify
// never produces a leading dot, and projectDirectories is derived from the
// slug column rather than the filesystem, so it never appears there either.
func revisionDirFor(docPath string) string {
	return filepath.Join(filepath.Dir(docPath), "."+filepath.Base(docPath))
}

// moveRevisionDirIfExists moves an entry's revision directory alongside it.
// It is a no-op, not an error, when the directory does not exist: revision
// history may not have shipped yet, or the entry may be new. A same-content
// copy is the fallback when os.Rename cannot move a directory in one step
// (crossing a filesystem boundary), since a revision directory can hold many
// files.
func moveRevisionDirIfExists(oldDocPath, newDocPath string) (moved bool, err error) {
	oldDir, newDir := revisionDirFor(oldDocPath), revisionDirFor(newDocPath)
	if _, err := os.Stat(oldDir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := os.Stat(newDir); err == nil {
		return false, ErrConflict("path_taken", newDir+" already exists", "")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o700); err != nil {
		return false, err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		if cerr := copyDirFlat(oldDir, newDir); cerr != nil {
			return false, errors.Join(err, cerr)
		}
		if rerr := os.RemoveAll(oldDir); rerr != nil {
			return false, rerr
		}
	}
	if err := syncDirectory(filepath.Dir(newDir)); err != nil {
		return false, err
	}
	if err := syncDirectory(filepath.Dir(oldDir)); err != nil {
		return false, err
	}
	return true, nil
}

// copyDirFlat copies every regular file directly inside src into dst, which
// is all a revision directory ever holds: one file per retained version, no
// subdirectories.
func copyDirFlat(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyAtomic(filepath.Join(dst, e.Name()), filepath.Join(src, e.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(dst)
}

// MoveKnowledge renames or relocates an entry within its own project's
// vault, never replacing anything at the destination. It refuses a global
// entry (demote first) and a destination directory that resembles an
// existing one (see refuseResemblingDir), and it moves the entry's revision
// directory if one exists.
//
// It does not yet rewrite inbound wikilinks that name the old path -- that
// needs the directory-aware wikilink syntax Task 9 of this plan adds, which
// itself needs the virtual-paths layer. Until Task 9 lands, an existing
// inbound link keeps resolving correctly (link rows point at the entry's id,
// not its slug), but the referring document's stored text still names the
// old path; the next Trellis edit that re-syncs that document's wikilinks
// will find the old path gone and turn the link into a stub, which
// `knowledge lint` already reports -- the same way a deleted entry's inbound
// links do today.
func (c *Core) MoveKnowledge(ctx context.Context, projectID, ref, newPath string, newDir bool) (Knowledge, error) {
	newSlug, err := SlugifyPath(newPath)
	if err != nil {
		return Knowledge{}, err
	}
	if newSlug == "" {
		return Knowledge{}, ErrUsage("missing_path", "knowledge mv needs a destination path",
			"trellis knowledge mv "+ref+" new/path")
	}
	var doc Knowledge
	var src, dest string
	var revMoved bool
	var done bool
	err = c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this closure
		// returns, while Core.Tx still holds SQLite's write lock. done, not
		// err, is what the undo is keyed on: a panic unwinds through this
		// defer without ever reaching the closure's own return statement, so
		// a named result would still read nil and the undo would be skipped.
		defer func() {
			if !done && dest != "" {
				if _, merr := moveFileTo(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				if revMoved {
					if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
				}
				dest = ""
			}
		}()

		resolved, rerr := c.resolveSlug(tx, projectID, ref, false)
		if rerr != nil {
			return rerr
		}
		if err := tx.Get(&doc, `SELECT * FROM knowledge WHERE project_id = ? AND slug = ?`, projectID, resolved); err != nil {
			return err
		}
		if doc.Global {
			return ErrUsage("global_entry", doc.Slug+" is in the global vault; demote it first",
				"trellis knowledge demote "+doc.Slug)
		}
		if newSlug == doc.Slug {
			return ErrUsage("same_path", doc.Slug+" is already there", "")
		}
		var taken int
		if err := tx.Get(&taken, `SELECT COUNT(*) FROM knowledge WHERE project_id = ? AND slug = ?`,
			projectID, newSlug); err != nil {
			return err
		}
		if taken > 0 {
			return ErrConflict("slug_taken", newSlug+" already exists in this project",
				"trellis knowledge show "+newSlug)
		}
		destDir := ""
		if i := strings.LastIndex(newSlug, "/"); i >= 0 {
			destDir = newSlug[:i]
		}
		if err := c.refuseResemblingDir(tx, projectID, destDir, newDir); err != nil {
			return err
		}
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		vault, err := c.kbDir(key, false)
		if err != nil {
			return err
		}
		src = doc.Path
		dest, err = moveFileTo(doc.Path, filepath.Join(vault, filepath.FromSlash(newSlug)+".md"))
		if err != nil {
			return err
		}
		revMoved, err = moveRevisionDirIfExists(src, dest)
		if err != nil {
			return err
		}
		now := c.clock.NowMS()
		oldSlug := doc.Slug
		if _, err := tx.Exec(`UPDATE knowledge SET slug = ?, path = ?, updated_at = ? WHERE id = ?`,
			newSlug, dest, now, doc.ID); err != nil {
			return err
		}
		doc.Slug, doc.Path, doc.UpdatedAt = newSlug, dest, now
		if err := c.recordEvent(tx, "knowledge", doc.ID, "moved", "", oldSlug, newSlug); err != nil {
			return err
		}
		if err := c.docView(tx, &doc); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		// done means the closure completed and it was tx.Commit that failed:
		// durable state, not a guess, decides which side of the move the
		// file belongs on, exactly as EscalateKnowledge already resolves this.
		var landed string
		qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID)
		if writeLanded(landed == dest, qerr) {
			err = nil
		} else {
			if _, merr := moveFileTo(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if revMoved {
				if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
			}
		}
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
	}
	return doc, err
}
