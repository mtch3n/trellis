package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/atomicfile"
)

// moveRevisionDirIfExists moves an entry's revision directory alongside it.
// It is a no-op, not an error, when the directory does not exist: revision
// history may not have shipped yet, or the entry may be new. A same-content
// copy is the fallback when os.Rename cannot move a directory in one step
// (crossing a filesystem boundary), since a revision directory can hold many
// files.
func moveRevisionDirIfExists(oldEntryPath, newEntryPath string) (moved bool, err error) {
	oldDir, newDir := revisionDir(oldEntryPath), revisionDir(newEntryPath)
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
		if cerr := copyDirAtomic(newDir, oldDir); cerr != nil {
			return false, errors.Join(err, cerr)
		}
		if rerr := os.RemoveAll(oldDir); rerr != nil {
			return false, rerr
		}
	}
	if err := atomicfile.SyncDir(filepath.Dir(newDir)); err != nil {
		return false, err
	}
	if err := atomicfile.SyncDir(filepath.Dir(oldDir)); err != nil {
		return false, err
	}
	return true, nil
}

// MoveEntry renames or relocates an entry within its own project's
// vault, never replacing anything at the destination. It refuses a global
// entry (demote first) and a destination directory that resembles an
// existing one (see refuseResemblingDir), and it moves the entry's revision
// directory if one exists.
//
// Wikilinks that resolve to the entry, in any entry of the project including
// itself, are rewritten to name the new path in the same transaction (see
// rewriteInboundWikilinks), and undone with the move.
func (c *Core) MoveEntry(ctx context.Context, projectID, ref, newPath string, newDir bool) (Entry, error) {
	newSlug, err := SlugifyPath(newPath)
	if err != nil {
		return Entry{}, err
	}
	if newSlug == "" {
		return Entry{}, ErrUsage("missing_path", "knowledge mv needs a destination path",
			"trellis knowledge mv "+ref+" new/path")
	}
	var entry Entry
	var src, dest string
	var revMoved bool
	var rewrites []fileWrite
	var done bool
	err = c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this closure
		// returns, while Core.Tx still holds SQLite's write lock. done, not
		// err, is what the undo is keyed on: a panic unwinds through this
		// defer without ever reaching the closure's own return statement, so
		// a named result would still read nil and the undo would be skipped.
		defer func() {
			if done {
				return
			}
			// Newest first: a rewrite of the entry itself sits at dest and is
			// restored there before the entry moves back.
			for i := len(rewrites) - 1; i >= 0; i-- {
				w := rewrites[i]
				if uerr := undoWrite(w.path, w.old, w.written); uerr != nil {
					err = errors.Join(err, uerr)
				}
				if derr := discardCapturedRevision(w.revisionDest); derr != nil {
					err = errors.Join(err, derr)
				}
			}
			rewrites = nil
			if dest != "" {
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

		key, err := projectKeyOf(tx, projectID)
		if err != nil {
			return err
		}
		// ref may be the canonical address show/search/recall print
		// (/KEY/vault/x), not just a bare slug: parse it the way loadEntry
		// does, so a /GLOBAL address is refused up front and one naming
		// another project reports wrong_project instead of not-found.
		d, derr := readEntryArg(ref, key)
		if derr != nil {
			return derr
		}
		if d.scope == entryVault {
			return ErrUsage("global_entry", ref+" is in the global vault; demote it first",
				"trellis knowledge demote "+ref)
		}
		resolved, rerr := c.resolveSlug(tx, projectID, d.slug, false)
		if rerr != nil {
			return rerr
		}
		if err := tx.Get(&entry, `SELECT * FROM entry WHERE project_id = ? AND slug = ?`, projectID, resolved); err != nil {
			return err
		}
		entry.Path = c.entryPath(key, entry.Global, entry.Slug)
		if entry.Global {
			return ErrUsage("global_entry", entry.Slug+" is in the global vault; demote it first",
				"trellis knowledge demote "+entry.Slug)
		}
		if newSlug == entry.Slug {
			return ErrUsage("same_path", entry.Slug+" is already there", "")
		}
		var taken int
		if err := tx.Get(&taken, `SELECT COUNT(*) FROM entry WHERE project_id = ? AND slug = ?`,
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
		src = entry.Path
		dest, err = moveFileTo(entry.Path, c.entryPath(key, false, newSlug))
		if err != nil {
			return err
		}
		revMoved, err = moveRevisionDirIfExists(src, dest)
		if err != nil {
			return err
		}
		now := c.clock.NowMS()
		oldSlug := entry.Slug
		if _, err := tx.Exec(`UPDATE entry SET slug = ?, updated_at = ? WHERE id = ?`,
			newSlug, now, entry.ID); err != nil {
			return err
		}
		entry.Slug, entry.Path, entry.UpdatedAt = newSlug, dest, now
		if err := c.recordEvent(tx, "entry", entry.ID, "moved", "", oldSlug, newSlug); err != nil {
			return err
		}
		// A move changes the entry's address the same way escalate and
		// demote do; a wikilink written to the new path before this move,
		// still a stub, becomes resolvable now.
		if err := c.resolveEntryStubs(tx, &entry); err != nil {
			return err
		}
		if err := c.rewriteInboundWikilinks(tx, &entry, key, oldSlug, &rewrites); err != nil {
			return err
		}
		if err := c.entryView(tx, &entry); err != nil {
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
		qerr := c.db.Get(&landed, `SELECT slug FROM entry WHERE id = ?`, entry.ID)
		if writeLanded(landed == newSlug, qerr) {
			err = nil
		} else {
			// Newest first, mirroring the deferred undo above: a rewrite of
			// the entry itself sits at dest and is restored there before the
			// entry moves back.
			for i := len(rewrites) - 1; i >= 0; i-- {
				w := rewrites[i]
				if uerr := undoWrite(w.path, w.old, w.written); uerr != nil {
					err = errors.Join(err, uerr)
				}
				if derr := discardCapturedRevision(w.revisionDest); derr != nil {
					err = errors.Join(err, derr)
				}
			}
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
		c.notifyEntryChanged(ctx, projectID)
	}
	return entry, err
}

// fileWrite is a file a transaction replaced, with what it held before, so
// the transaction can put it back if it does not complete.
type fileWrite struct {
	path         string
	old          []byte
	written      string // content hash of what was written
	revisionDest string // the new version captureEntryRevision wrote, if any
}

// rewriteInboundWikilinks rewrites every wikilink that resolves to entry so it
// names entry's new slug. Which links those are comes from the link rows, by
// their stored text, so a link that merely looks alike -- or a bare leaf that
// resolves to another entry -- is left alone. An address stays an address.
//
// Each rewrite is a Trellis write of the referring entry: its prior content
// is kept as a revision, its row follows the file, and the event log records
// the edit. Every file is appended to undo as soon as it is written, so the
// caller can restore it if anything later fails. A referring entry whose file
// is gone is skipped; lint reports its links.
func (c *Core) rewriteInboundWikilinks(tx *sqlx.Tx, entry *Entry, projectKey, oldSlug string, undo *[]fileWrite) error {
	var rows []struct {
		FromID string `db:"from_id"`
		Raw    string `db:"to_raw"`
	}
	if err := tx.Select(&rows, `SELECT from_id, to_raw FROM link
		WHERE from_type = 'entry' AND to_type = 'entry' AND rel = 'wikilink' AND to_id = ?
		ORDER BY from_id`, entry.ID); err != nil {
		return err
	}
	raws := map[string]map[string]bool{}
	var order []string
	for _, r := range rows {
		if raws[r.FromID] == nil {
			raws[r.FromID] = map[string]bool{}
			order = append(order, r.FromID)
		}
		raws[r.FromID][r.Raw] = true
	}
	newTarget := func(old string) string {
		if strings.HasPrefix(old, "/") {
			return EntryAddress(projectKey, false, entry.Slug)
		}
		return entry.Slug
	}
	for _, fromID := range order {
		var from Entry
		if err := tx.Get(&from, `SELECT * FROM entry WHERE id = ?`, fromID); err != nil {
			return err
		}
		if err := c.refreshFromFile(tx, &from); err != nil {
			if e, ok := errors.AsType[*Error](err); ok && e.Code == "file_missing" {
				continue
			}
			return err
		}
		raw, err := os.ReadFile(from.Path)
		if err != nil {
			return err
		}
		if ContentHash(string(raw)) != from.ContentHash {
			return changedOnDisk(from.Slug)
		}
		fm, body, err := splitEntryFile(from.Path, raw)
		if err != nil {
			return err
		}
		newBody := RewriteWikilinks(body, func(ref Reference) (string, bool) {
			if !raws[fromID][ref.Raw] {
				return "", false
			}
			target, anchor, hasAnchor := strings.Cut(ref.Raw, "#")
			if hasAnchor {
				return newTarget(target) + "#" + anchor, true
			}
			return newTarget(target), true
		})
		if newBody == body {
			continue
		}
		if _, err := c.captureEntryRevision(from.Path, from.Version, raw); err != nil {
			return err
		}
		now := c.clock.NowMS()
		fm.Updated = msToRFC3339(now)
		out := RenderEntry(fm, newBody)
		if err := replaceIfUnchanged(from.Path, []byte(out), from.ContentHash); err != nil {
			if errors.Is(err, errFileChanged) {
				return changedOnDisk(from.Slug)
			}
			return err
		}
		*undo = append(*undo, fileWrite{path: from.Path, old: raw, written: ContentHash(out)})
		st, err := os.Stat(from.Path)
		if err != nil {
			return err
		}
		from.BodyMD, from.ContentHash = newBody, ContentHash(out)
		from.MTime, from.Size = st.ModTime().UnixMilli(), st.Size()
		from.Version++
		from.UpdatedAt = now
		// The spec's copy table asks for the new file too, captured after any
		// Trellis write; the pre-write capture above only ever retained the
		// version this rewrite replaced.
		revisionDest, err := c.captureEntryRevision(from.Path, from.Version, []byte(out))
		if err != nil {
			return err
		}
		(*undo)[len(*undo)-1].revisionDest = revisionDest
		if _, err := tx.Exec(`UPDATE entry SET content_hash = ?, mtime = ?, size = ?, version = ?, updated_at = ?
			WHERE id = ?`, from.ContentHash, from.MTime, from.Size, from.Version, from.UpdatedAt, from.ID); err != nil {
			return err
		}
		if err := c.syncEntryRelations(tx, &from, fm, newBody); err != nil {
			return err
		}
		value := newBody
		if from.Private {
			value = ""
		}
		if err := c.recordEvent(tx, "entry", from.ID, "edited", "body", "", value); err != nil {
			return err
		}
		if from.ID == entry.ID {
			entry.BodyMD, entry.ContentHash, entry.MTime, entry.Size = from.BodyMD, from.ContentHash, from.MTime, from.Size
			entry.Version, entry.UpdatedAt = from.Version, from.UpdatedAt
		}
	}
	if len(*undo) > 0 {
		return c.rebuildEntryFTS(tx)
	}
	return nil
}
