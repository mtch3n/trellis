package core

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/atomicfile"
)

// MaxInjectedPins is how many recaps the brief carries in full; the rest are
// listed as titles only. Pinned recaps compete with board state for the
// injection budget, and an injection nobody reads is no injection at all (§13.1).
const MaxInjectedPins = 5

// Pin is an entry whose recap is injected at session start (§10.5).
// An empty Recap makes the pin a pointer, ref and title, which is all a private
// entry ever injects. A pointer is never Stale: there is no recap to be wrong.
type Pin struct {
	// ID is not part of the payload — callers address a pin by slug — but the
	// disclosure refresh needs it. See privateAfterRefresh.
	ID        string  `db:"id" json:"-"`
	Slug      string  `db:"slug" json:"slug"`
	Title     string  `db:"title" json:"title"`
	Recap     string  `db:"recap" json:"recap"`
	BoardName *string `db:"board_name" json:"board,omitempty"`
	Stale     bool    `db:"stale" json:"stale"`
	CreatedAt int64   `db:"created_at" json:"created_at"`
}

// PinEntry pins an entry, with the recap the agent wrote. Summarizing is
// what a model is good at and a CLI is not, so trellis never generates one: with
// no recap supplied it falls back to the frontmatter summary, then to the first
// paragraph.
//
// A private entry has no recap. Its pin injects a pointer, ref and title, so
// any recap supplied for it is discarded rather than stored: a stored one would
// never be shown, and would sit in entry.recap and the event log until the
// entry was un-marked and it was injected after all.
func (c *Core) PinEntry(ctx context.Context, projectID, slug, recap, board string) (Pin, error) {
	var pin Pin
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var entry Entry
		if err := c.loadEntry(tx, projectID, slug, &entry); err != nil {
			return err
		}
		var text string
		if !entry.Private {
			text = cmp.Or(strings.TrimSpace(recap), entry.Summary, FirstParagraph(entry.BodyMD))
			if text == "" {
				return ErrUsage("no_recap", "this entry has no summary to fall back on",
					`trellis knowledge pin `+entry.Slug+` --recap "one line an agent can act on"`)
			}
		}

		var boardID *string
		var boardName *string
		if board != "" {
			b, err := c.boardByName(tx, projectID, board)
			if err != nil {
				return err
			}
			boardID, boardName = &b.ID, &b.Name
		}
		now := c.clock.NowMS()
		// NULL for a private entry, written explicitly rather than left alone
		// so the rule holds whatever the row carried before.
		var stored, hash *string
		if !entry.Private {
			stored, hash = &text, &entry.ContentHash
		}
		if _, err := tx.Exec(
			`UPDATE entry SET recap = ?, recap_hash = ? WHERE id = ?`,
			stored, hash, entry.ID); err != nil {
			return err
		}

		// The table's UNIQUE treats NULL boards as distinct, so a project-wide
		// pin has its own partial index to conflict on.
		conflict := `(entry_id, board_id)`
		if boardID == nil {
			conflict = `(entry_id) WHERE board_id IS NULL`
		}
		if _, err := tx.Exec(
			`INSERT INTO pin (id, entry_id, board_id, created_at) VALUES (?, ?, ?, ?)
			 ON CONFLICT `+conflict+` DO UPDATE SET created_at = excluded.created_at`,
			NewID(), entry.ID, boardID, now); err != nil {
			return err
		}
		pin = Pin{Slug: entry.Slug, Title: entry.Title, Recap: text, BoardName: boardName, CreatedAt: now}
		return c.recordEvent(tx, "entry", entry.ID, "pinned", "", "", text)
	})
	return pin, err
}

// UnpinEntry removes a pin. The recap survives on the row: unpinning is a
// decision about injection, not about the summary being wrong.
func (c *Core) UnpinEntry(ctx context.Context, projectID, slug, board string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var entry Entry
		if err := c.loadEntry(tx, projectID, slug, &entry); err != nil {
			return err
		}
		var res sql.Result
		var err error
		if board == "" {
			res, err = tx.Exec(`DELETE FROM pin WHERE entry_id = ? AND board_id IS NULL`, entry.ID)
		} else {
			b, berr := c.boardByName(tx, projectID, board)
			if berr != nil {
				return berr
			}
			res, err = tx.Exec(`DELETE FROM pin WHERE entry_id = ? AND board_id = ?`, entry.ID, b.ID)
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound("not_pinned", entry.Slug+" is not pinned there",
				"trellis knowledge pins")
		}
		return c.recordEvent(tx, "entry", entry.ID, "unpinned", "", "", "")
	})
}

// Pins lists what would be injected, most recently pinned first. Staleness is
// detected rather than guessed: recap_hash is the content hash at the moment the
// recap was written, so a mismatch means the entry moved on and the recap may
// now be confidently wrong. Limit controls how many pins are returned; 0 means
// no limit.
func (c *Core) Pins(ctx context.Context, projectID, boardID string, limit int) ([]Pin, error) {
	var pins []Pin
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		pins, err = c.pins(tx, projectID, boardID, limit)
		return err
	})
	return pins, err
}

func (c *Core) pins(tx *sqlx.Tx, projectID, boardID string, limit int) ([]Pin, error) {
	pins := []Pin{}
	q := `SELECT k.id, k.slug, k.title, COALESCE(k.recap, '') AS recap,
	             b.name AS board_name,
	             (k.recap_hash IS NOT k.content_hash) AS stale, p.created_at
	      FROM pin p JOIN entry k ON k.id = p.entry_id
	      LEFT JOIN board b ON b.id = p.board_id
	      WHERE k.project_id = ?`
	args := []any{projectID}
	if boardID != "" {
		q += " AND (p.board_id IS NULL OR p.board_id = ?)"
		args = append(args, boardID)
	}
	q += " ORDER BY p.created_at DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	if err := tx.Select(&pins, q, args...); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(pins))
	for _, pin := range pins {
		ids = append(ids, pin.ID)
	}
	// The brief is injected without anyone asking for it, so the flag is
	// read from the files rather than from the mirror, which is one read
	// stale after a hand edit. Pins are curated, so this is a handful of
	// stats and reads.
	private, _, err := c.privateAfterRefresh(tx, ids)
	if err != nil {
		return nil, err
	}
	for i := range pins {
		// Decided after the refresh, not in the SELECT, which sees each row
		// as it was before the refresh purged it. A private pin is a pointer
		// and never needs a recap. Any other pin without one does: it was
		// made while the entry was private, and nothing else prompts for it.
		if private[pins[i].ID] {
			pins[i].Recap = ""
			pins[i].Stale = false
		} else if pins[i].Recap == "" {
			pins[i].Stale = true
		}
	}
	return pins, nil
}

// moveFileTo moves src to the exact destination path dest, refusing to
// replace anything there. A hard link is nearly free and needs no fallback
// for the common case (same filesystem); os.Link's O_EXCL-like semantics are
// what make "refuses to replace" true without a TOCTOU gap. A link across
// filesystems, or on a filesystem without hard links, falls back to
// copyAtomic, which refuses the same way. Either way src is only removed once
// dest is safely in place. knowledge mv calls this directly because, unlike
// promote and demote, it can rename the leaf as well as relocate it.
func moveFileTo(src, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := os.Link(src, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ErrConflict("path_taken", dest+" already exists", "")
		}
		if err := copyAtomic(dest, src); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return "", ErrConflict("path_taken", dest+" already exists", "")
			}
			return "", err
		}
	}
	if removeErr := os.Remove(src); removeErr != nil {
		if cleanupErr := os.Remove(dest); cleanupErr != nil {
			return "", errors.Join(removeErr, cleanupErr)
		}
		return "", removeErr
	}
	destDir := filepath.Dir(dest)
	srcDir := filepath.Dir(src)
	if err := atomicfile.SyncDir(destDir); err != nil {
		return "", err
	}
	if destDir != srcDir {
		if err := atomicfile.SyncDir(srcDir); err != nil {
			return "", err
		}
	}
	return dest, nil
}

// moveBack undoes a successful moveFile: dest moves back into the directory
// src used to live in. It is the only compensation promote and demote need,
// so it stays a small helper rather than growing into a general framework.
func moveBack(dest, src string) error {
	_, err := moveFile(dest, filepath.Dir(src))
	return err
}
