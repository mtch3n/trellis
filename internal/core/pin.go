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
)

// MaxInjectedPins is how many recaps the brief carries in full; the rest are
// listed as titles only. Pinned recaps compete with board state for the
// injection budget, and an injection nobody reads is no injection at all (§13.1).
const MaxInjectedPins = 5

// Pin is a knowledge entry whose recap is injected at session start (§10.5).
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

// PinKnowledge pins an entry, with the recap the agent wrote. Summarizing is
// what a model is good at and a CLI is not, so trellis never generates one: with
// no recap supplied it falls back to the frontmatter summary, then to the first
// paragraph.
//
// A private entry has no recap. Its pin injects a pointer, ref and title, so
// any recap supplied for it is discarded rather than stored: a stored one would
// never be shown, and would sit in knowledge.recap and the event log until the
// entry was un-marked and it was injected after all.
func (c *Core) PinKnowledge(ctx context.Context, projectID, slug, recap, board string) (Pin, error) {
	var pin Pin
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var doc Knowledge
		if err := c.loadDoc(tx, projectID, slug, &doc); err != nil {
			return err
		}
		var text string
		if !doc.Private {
			text = cmp.Or(strings.TrimSpace(recap), doc.Summary, FirstParagraph(doc.BodyMD))
			if text == "" {
				return ErrUsage("no_recap", "this entry has no summary to fall back on",
					`trellis knowledge pin `+doc.Slug+` --recap "one line an agent can act on"`)
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
		if !doc.Private {
			stored, hash = &text, &doc.ContentHash
		}
		if _, err := tx.Exec(
			`UPDATE knowledge SET recap = ?, recap_hash = ? WHERE id = ?`,
			stored, hash, doc.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO pin (id, knowledge_id, board_id, created_at) VALUES (?, ?, ?, ?)
			 ON CONFLICT (knowledge_id, board_id) DO UPDATE SET created_at = excluded.created_at`,
			NewCardID(), doc.ID, boardID, now); err != nil {
			return err
		}
		pin = Pin{Slug: doc.Slug, Title: doc.Title, Recap: text, BoardName: boardName, CreatedAt: now}
		return c.recordEvent(tx, "knowledge", doc.ID, "pinned", "", "", text)
	})
	return pin, err
}

// UnpinKnowledge removes a pin. The recap survives on the row: unpinning is a
// decision about injection, not about the summary being wrong.
func (c *Core) UnpinKnowledge(ctx context.Context, projectID, slug, board string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var doc Knowledge
		if err := c.loadDoc(tx, projectID, slug, &doc); err != nil {
			return err
		}
		var res sql.Result
		var err error
		if board == "" {
			res, err = tx.Exec(`DELETE FROM pin WHERE knowledge_id = ? AND board_id IS NULL`, doc.ID)
		} else {
			b, berr := c.boardByName(tx, projectID, board)
			if berr != nil {
				return berr
			}
			res, err = tx.Exec(`DELETE FROM pin WHERE knowledge_id = ? AND board_id = ?`, doc.ID, b.ID)
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound("not_pinned", doc.Slug+" is not pinned there",
				"trellis knowledge pins")
		}
		return c.recordEvent(tx, "knowledge", doc.ID, "unpinned", "", "", "")
	})
}

// Pins lists what would be injected, most recently pinned first. Staleness is
// detected rather than guessed: recap_hash is the content hash at the moment the
// recap was written, so a mismatch means the entry moved on and the recap may
// now be confidently wrong.
func (c *Core) Pins(ctx context.Context, projectID, boardID string) ([]Pin, error) {
	pins := []Pin{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		q := `SELECT k.id, k.slug, k.title, COALESCE(k.recap, '') AS recap,
		             b.name AS board_name,
		             (k.recap_hash IS NOT k.content_hash) AS stale, p.created_at
		      FROM pin p JOIN knowledge k ON k.id = p.knowledge_id
		      LEFT JOIN board b ON b.id = p.board_id
		      WHERE k.project_id = ?`
		args := []any{projectID}
		if boardID != "" {
			q += " AND (p.board_id IS NULL OR p.board_id = ?)"
			args = append(args, boardID)
		}
		q += " ORDER BY p.created_at DESC"
		if err := tx.Select(&pins, q, args...); err != nil {
			return err
		}
		ids := make([]string, 0, len(pins))
		for _, pin := range pins {
			ids = append(ids, pin.ID)
		}
		// The brief is injected without anyone asking for it, so the flag is
		// read from the files rather than from the mirror, which is one read
		// stale after a hand edit. Pins are curated, so this is a handful of
		// stats and reads.
		private, err := c.privateAfterRefresh(tx, ids)
		if err != nil {
			return err
		}
		for i := range pins {
			if private[pins[i].ID] {
				pins[i].Recap = ""
			}
			// Decided after the refresh, not in the SELECT. A purged row has
			// a NULL recap_hash, which the SELECT reads as stale, but the
			// SELECT sees each row as it was before this refresh, so the
			// read that runs the purge would say the opposite.
			if pins[i].Recap == "" {
				pins[i].Stale = false
			}
		}
		return nil
	})
	return pins, err
}

// Nomination is an agent's argument that an entry is useful beyond its project.
type Nomination struct {
	Slug      string `db:"slug" json:"slug"`
	Title     string `db:"title" json:"title"`
	Actor     string `db:"actor" json:"actor"`
	Reason    string `db:"reason" json:"reason"`
	Cited     int    `db:"cited" json:"cited"`
	Pinned    int    `db:"pinned" json:"pinned"`
	Reads     int    `db:"reads" json:"reads_30d"`
	Actors    int    `db:"actors" json:"actors"`
	Noms      int    `db:"noms" json:"noms"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
}

// NominateKnowledge records a candidate for escalation. No threshold blocks
// one: a genuinely new insight can deserve global status immediately, and a gate
// that argues with the nominator is a gate nobody uses (§10.7).
func (c *Core) NominateKnowledge(ctx context.Context, projectID, slug, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrUsage("missing_reason", "a nomination carries evidence, not just an opinion",
			`trellis knowledge nominate `+slug+` --reason "every repo re-derives this"`)
	}
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var doc Knowledge
		if err := c.loadDoc(tx, projectID, slug, &doc); err != nil {
			return err
		}
		if doc.Global {
			return ErrUsage("already_global", doc.Slug+" is already global", "trellis knowledge show "+doc.Slug)
		}
		if _, err := tx.Exec(
			`INSERT INTO nomination (id, knowledge_id, actor, reason, created_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT (knowledge_id, actor) DO UPDATE SET reason = excluded.reason`,
			NewCardID(), doc.ID, c.actor, reason, c.clock.NowMS()); err != nil {
			return err
		}
		return c.recordEvent(tx, "knowledge", doc.ID, "nominated", "", "", reason)
	})
}

// Nominations is the queue, ordered by evidence trellis already keeps: what was
// actually read and cited, not what merely exists (§10.7).
func (c *Core) Nominations(ctx context.Context, projectID string) ([]Nomination, error) {
	out := []Nomination{}
	since := c.clock.NowMS() - readWindowMS()
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&out,
			`SELECT k.slug, k.title, n.actor, n.reason, n.created_at,
			        (SELECT COUNT(*) FROM link l WHERE l.to_type = 'doc' AND l.to_id = k.id) AS cited,
			        (SELECT COUNT(*) FROM pin p WHERE p.knowledge_id = k.id) AS pinned,
			        (SELECT COUNT(*) FROM event e WHERE e.entity_type = 'knowledge'
			           AND e.entity_id = k.id AND e.action = 'read' AND e.ts > ?) AS reads,
			        (SELECT COUNT(DISTINCT e.actor) FROM event e WHERE e.entity_type = 'knowledge'
			           AND e.entity_id = k.id AND e.action = 'read' AND e.ts > ?) AS actors,
			        (SELECT COUNT(*) FROM nomination n2 WHERE n2.knowledge_id = k.id) AS noms
			 FROM nomination n JOIN knowledge k ON k.id = n.knowledge_id
			 WHERE k.project_id = ? AND k.global = 0
			 ORDER BY noms DESC, reads DESC, cited DESC, n.created_at`,
			since, since, projectID)
	})
	return out, err
}

// GlobalReviewDays is how long a global entry goes before it is called
// unreviewed. Shorter than anything local: reach amplifies staleness (§10.11).
const GlobalReviewDays = 180

// EscalateKnowledge moves an entry to the global vault. It MOVES rather than
// copies: two copies diverge, and a stale global copy is worse than none. Every
// existing reference keeps resolving because link.to_id stores identity.
//
// The human gate is enforced by the caller (§10.8): core has no opinion about
// who is typing, and an --i-am-human flag is exactly what an agent would reach
// for, so none exists anywhere.
func (c *Core) EscalateKnowledge(ctx context.Context, projectID, slug, reason string) (Knowledge, error) {
	var doc Knowledge
	var src, dest string
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		// A failure after the move undoes it before this closure returns,
		// while Core.Tx still holds SQLite's write lock.
		defer func() {
			if err != nil && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				dest = ""
			}
		}()

		if err := c.loadDoc(tx, projectID, slug, &doc); err != nil {
			return err
		}
		if doc.Global {
			return ErrUsage("already_global", doc.Slug+" is already global", "")
		}
		var taken int
		if err := tx.Get(&taken, `SELECT COUNT(*) FROM knowledge WHERE global = 1 AND slug = ?`, doc.Slug); err != nil {
			return err
		}
		if taken > 0 {
			return ErrConflict("global_slug_taken",
				"the global vault already has an entry named "+doc.Slug,
				"trellis knowledge show GLOBAL/"+doc.Slug)
		}
		dir, err := c.kbDir(GlobalKey, true)
		if err != nil {
			return err
		}
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
		now := c.clock.NowMS()
		reviewBy := now + int64(GlobalReviewDays)*24*60*60*1000
		if _, err := tx.Exec(
			`UPDATE knowledge SET global = 1, path = ?, board_id = NULL, review_by = ?,
			                      reviewed_at = ?, updated_at = ? WHERE id = ?`,
			dest, reviewBy, now, now, doc.ID); err != nil {
			return err
		}
		doc.Global, doc.Path, doc.ReviewBy, doc.ReviewedAt = true, dest, &reviewBy, &now
		if err := c.recordEvent(tx, "knowledge", doc.ID, "escalated", "", "", reason); err != nil {
			return err
		}
		return c.docView(tx, &doc)
	})
	if err != nil && dest != "" {
		// dest != "" only survives to here when the closure itself succeeded
		// and tx.Commit failed: durable state, not a guess, decides which
		// side of the move the file belongs on.
		var landed string
		if qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID); qerr != nil || landed != dest {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
		}
	}
	return doc, err
}

// DemoteKnowledge returns a global entry to its origin project. An escalation
// mistake must not be permanent.
func (c *Core) DemoteKnowledge(ctx context.Context, slug, reason string) (Knowledge, error) {
	var doc Knowledge
	var src, dest string
	err := c.Tx(ctx, func(tx *sqlx.Tx) (err error) {
		defer func() {
			if err != nil && dest != "" {
				if merr := moveBack(dest, src); merr != nil {
					err = errors.Join(err, merr)
				}
				dest = ""
			}
		}()

		gerr := tx.Get(&doc, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, Slugify(slug))
		if errors.Is(gerr, sql.ErrNoRows) {
			return ErrNotFound("not_global", "no global entry "+slug, "trellis knowledge ls --global")
		}
		if gerr != nil {
			return gerr
		}
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, doc.ProjectID); err != nil {
			return err
		}
		dir, err := c.kbDir(key, false)
		if err != nil {
			return err
		}
		src = doc.Path
		moved, err := moveFile(doc.Path, dir)
		if err != nil {
			return err
		}
		dest = moved
		if _, err := tx.Exec(
			`UPDATE knowledge SET global = 0, path = ?, review_by = NULL, updated_at = ? WHERE id = ?`,
			dest, c.clock.NowMS(), doc.ID); err != nil {
			return err
		}
		doc.Global, doc.Path, doc.ReviewBy = false, dest, nil
		if err := c.recordEvent(tx, "knowledge", doc.ID, "demoted", "", "", reason); err != nil {
			return err
		}
		return c.docView(tx, &doc)
	})
	if err != nil && dest != "" {
		var landed string
		if qerr := c.db.Get(&landed, `SELECT path FROM knowledge WHERE id = ?`, doc.ID); qerr != nil || landed != dest {
			if merr := moveBack(dest, src); merr != nil {
				err = errors.Join(err, merr)
			}
		}
	}
	return doc, err
}

// VerifyKnowledge resets the review clock on a global entry.
func (c *Core) VerifyKnowledge(ctx context.Context, slug string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var doc Knowledge
		err := tx.Get(&doc, `SELECT * FROM knowledge WHERE slug = ? AND global = 1`, Slugify(slug))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound("not_global", "no global entry "+slug, "trellis knowledge ls --global")
		}
		if err != nil {
			return err
		}
		now := c.clock.NowMS()
		reviewBy := now + int64(GlobalReviewDays)*24*60*60*1000
		if _, err := tx.Exec(
			`UPDATE knowledge SET reviewed_at = ?, review_by = ? WHERE id = ?`, now, reviewBy, doc.ID); err != nil {
			return err
		}
		return c.recordEvent(tx, "knowledge", doc.ID, "verified", "", "", "")
	})
}

// Unreviewed reports whether a global entry is past its review date, which is
// said out loud at every point of use rather than filed in a report nobody reads.
func (d Knowledge) Unreviewed(nowMS int64) bool {
	return d.Global && d.ReviewBy != nil && nowMS > *d.ReviewBy
}

// moveFile moves src into destDir, refusing to replace anything already
// there. A hard link is nearly free and needs no fallback for the common case
// (same filesystem); os.Link's O_EXCL-like semantics are what make "refuses
// to replace" true without a TOCTOU gap. A link across filesystems, or on a
// filesystem without hard links, falls back to copyAtomic, which refuses the
// same way. Either way src is only removed once dest is safely in place.
func moveFile(src, destDir string) (string, error) {
	dest := filepath.Join(destDir, baseName(src))
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
	if err := os.Remove(src); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	if err := syncDirectory(destDir); err != nil {
		return "", err
	}
	if err := syncDirectory(filepath.Dir(src)); err != nil {
		return "", err
	}
	return dest, nil
}

// moveBack undoes a successful moveFile: dest moves back into the directory
// src used to live in. It is the only compensation escalate and demote need,
// so it stays a small helper rather than growing into a general framework.
func moveBack(dest, src string) error {
	_, err := moveFile(dest, filepath.Dir(src))
	return err
}

func baseName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}
