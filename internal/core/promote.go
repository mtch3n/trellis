package core

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// GlobalVerifyDays is how long a global entry goes before it is called
// unverified. Shorter than anything local: reach amplifies staleness (§10.11).
const GlobalVerifyDays = 180

// PromoteEntry moves an entry to the global vault. It MOVES rather than
// copies: two copies diverge, and a stale global copy is worse than none. Every
// existing reference keeps resolving because link.to_id stores identity.
//
// The human gate is enforced by the caller (§10.8): core has no opinion about
// who is typing, and an --i-am-human flag is exactly what an agent would reach
// for, so none exists anywhere.
func (c *Core) PromoteEntry(ctx context.Context, projectID, slug, reason string) (Entry, error) {
	var entry Entry
	var src, dest string
	var revMoved bool
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this
		// closure returns, while Core.Tx still holds SQLite's write lock.
		// done, not err, is what the undo is keyed on: a panic unwinds
		// through this defer without ever reaching the closure's own return
		// statement, so a named result would still read nil and the undo
		// would be skipped.
		defer func() {
			if !done {
				if dest != "" {
					if merr := moveBack(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
					if revMoved {
						if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
							err = errors.Join(err, merr)
						}
					}
					dest = ""
				}
			}
		}()

		if err := c.loadEntry(tx, projectID, slug, &entry); err != nil {
			return err
		}
		if entry.Global {
			return ErrUsage("already_global", entry.Slug+" is already global", "")
		}
		var taken int
		if err := tx.Get(&taken, `SELECT COUNT(*) FROM entry WHERE global = 1 AND slug = ?`, entry.Slug); err != nil {
			return err
		}
		if taken > 0 {
			return ErrConflict("global_slug_taken",
				"the global vault already has an entry named "+entry.Slug,
				"trellis knowledge show GLOBAL/"+entry.Slug)
		}
		src = entry.Path
		dest, err = moveFileTo(entry.Path, c.entryPath(GlobalKey, true, entry.Slug))
		if err != nil {
			return err
		}
		// The revision directory follows the entry to its new subpath, not
		// to the vault root: revisionDir(dest) may sit several segments
		// below dir when the slug carries one, exactly the way a plain
		// `knowledge mv` already moves it.
		revMoved, err = moveRevisionDirIfExists(src, dest)
		if err != nil {
			return err
		}
		now := c.clock.NowMS()
		verifyBy := now + int64(GlobalVerifyDays)*24*60*60*1000
		if _, err := tx.Exec(
			`UPDATE entry SET global = 1, board_id = NULL, verify_by = ?,
			                      verified_at = ?, updated_at = ? WHERE id = ?`,
			verifyBy, now, now, entry.ID); err != nil {
			return err
		}
		entry.Global, entry.Path, entry.VerifyBy, entry.VerifiedAt = true, dest, &verifyBy, &now
		// Links written to the vault address before the entry got there are
		// stubs; promoting is what makes them resolvable.
		if err := c.resolveEntryStubs(tx, &entry); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "entry", entry.ID, "promoted", "", "", reason); err != nil {
			return err
		}
		if err := c.entryView(tx, &entry); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		// done means the closure completed and it was tx.Commit that
		// failed: durable state, not a guess, decides which side of the
		// move the file belongs on, and when it landed the promote is a
		// success no matter what Commit reported.
		var landed bool
		qerr := c.db.Get(&landed, `SELECT global FROM entry WHERE id = ?`, entry.ID)
		if writeLanded(landed, qerr) {
			err = nil
		} else {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if revMoved {
				if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
			}
		}
	}
	return entry, err
}

// DemoteEntry returns a global entry to its origin project. An promotion
// mistake must not be permanent.
func (c *Core) DemoteEntry(ctx context.Context, slug, reason string) (Entry, error) {
	var entry Entry
	var src, dest string
	var revMoved bool
	var done bool
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure, or a panic, after the move undoes it before this
		// closure returns, while Core.Tx still holds SQLite's write lock.
		// done, not err, is what the undo is keyed on: a panic unwinds
		// through this defer without ever reaching the closure's own return
		// statement, so a named result would still read nil and the undo
		// would be skipped.
		defer func() {
			if !done {
				if dest != "" {
					if merr := moveBack(dest, src); merr != nil {
						err = errors.Join(err, merr)
					}
					if revMoved {
						if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
							err = errors.Join(err, merr)
						}
					}
					dest = ""
				}
			}
		}()

		parsedSlug, err := vaultSlug(slug)
		if err != nil {
			return err
		}
		exactSlug, rerr := c.resolveSlug(tx, "", parsedSlug, true)
		if rerr != nil {
			return rerr
		}
		gerr := tx.Get(&entry, `SELECT * FROM entry WHERE slug = ? AND global = 1`, exactSlug)
		if errors.Is(gerr, sql.ErrNoRows) {
			return ErrNotFound("not_global", "no global entry "+slug, "trellis knowledge ls --global")
		}
		if gerr != nil {
			return gerr
		}
		entry.Path = c.entryPath(GlobalKey, true, entry.Slug)
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, entry.ProjectID); err != nil {
			return err
		}
		src = entry.Path
		dest, err = moveFileTo(entry.Path, c.entryPath(key, false, entry.Slug))
		if err != nil {
			return err
		}
		// The revision directory follows the entry to its new subpath, not
		// to the vault root: revisionDir(dest) may sit several segments
		// below dir when the slug carries one, exactly the way a plain
		// `knowledge mv` already moves it.
		revMoved, err = moveRevisionDirIfExists(src, dest)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`UPDATE entry SET global = 0, verify_by = NULL, updated_at = ? WHERE id = ?`,
			c.clock.NowMS(), entry.ID); err != nil {
			return err
		}
		entry.Global, entry.Path, entry.VerifyBy = false, dest, nil
		// Links to the project address resolve once the entry is back.
		if err := c.resolveEntryStubs(tx, &entry); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "entry", entry.ID, "demoted", "", "", reason); err != nil {
			return err
		}
		if err := c.entryView(tx, &entry); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil && done {
		// done means the closure completed and it was tx.Commit that
		// failed: durable state, not a guess, decides which side of the
		// move the file belongs on, and when it landed the demote is a
		// success no matter what Commit reported.
		var landed bool
		qerr := c.db.Get(&landed, `SELECT global FROM entry WHERE id = ?`, entry.ID)
		if writeLanded(!landed, qerr) {
			err = nil
		} else {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
			if revMoved {
				if _, merr := moveRevisionDirIfExists(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
			}
		}
	}
	return entry, err
}

// VerifyEntry resets the verify date on a global entry.
func (c *Core) VerifyEntry(ctx context.Context, slug string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var entry Entry
		parsedSlug, err := vaultSlug(slug)
		if err != nil {
			return err
		}
		exactSlug, rerr := c.resolveSlug(tx, "", parsedSlug, true)
		if rerr != nil {
			return rerr
		}
		err = tx.Get(&entry, `SELECT * FROM entry WHERE slug = ? AND global = 1`, exactSlug)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound("not_global", "no global entry "+slug, "trellis knowledge ls --global")
		}
		if err != nil {
			return err
		}
		now := c.clock.NowMS()
		verifyBy := now + int64(GlobalVerifyDays)*24*60*60*1000
		if _, err := tx.Exec(
			`UPDATE entry SET verified_at = ?, verify_by = ? WHERE id = ?`, now, verifyBy, entry.ID); err != nil {
			return err
		}
		return c.recordEvent(tx, "entry", entry.ID, "verified", "", "", "")
	})
}

// Unverified reports whether a global entry is past its review date, which is
// said out loud at every point of use rather than filed in a report nobody reads.
func (e Entry) Unverified(nowMS int64) bool {
	return e.Global && e.VerifyBy != nil && nowMS > *e.VerifyBy
}

// moveFile moves src into destDir, refusing to replace anything already
// there. It is moveFileTo with the destination computed as "same basename,
// new directory" -- promote and demote never rename the leaf, only relocate
// it between the project vault and the global one.
func moveFile(src, destDir string) (string, error) {
	return moveFileTo(src, filepath.Join(destDir, baseName(src)))
}

func baseName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}
