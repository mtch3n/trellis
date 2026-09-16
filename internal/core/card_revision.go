package core

import (
	"context"
	"fmt"

	"github.com/aymanbagabas/go-udiff"
	"github.com/jmoiron/sqlx"
)

// captureCardRevision retains a card's title and body at version, unless
// capture is disabled. The insert is idempotent (INSERT OR IGNORE) so
// callers may capture a version that might already have a row -- the one a
// card is about to leave, which an earlier edit's own "after" capture may
// already have written -- without checking first.
func (c *Core) captureCardRevision(tx *sqlx.Tx, cardID string, version int64, title, body string) error {
	if c.historyKeep == 0 {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO card_revision (card_id, version, title, body_md, actor, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		cardID, version, title, body, c.actor, c.clock.NowMS()); err != nil {
		return err
	}
	return trimCardRevisions(tx, cardID, c.historyKeep)
}

// cardHasRevision reports whether a card has any retained revision at all.
// EditCard's "before" capture is gated on this rather than on the card's
// current version, so a version bump with no content change (a move, a lease
// change) between two edits does not get invented into a revision of its own.
func (c *Core) cardHasRevision(tx *sqlx.Tx, cardID string) (bool, error) {
	var exists bool
	err := tx.Get(&exists, `SELECT EXISTS(SELECT 1 FROM card_revision WHERE card_id = ?)`, cardID)
	return exists, err
}

// trimCardRevisions removes a card's oldest revisions until at most keep
// remain, ordered by version.
func trimCardRevisions(tx *sqlx.Tx, cardID string, keep int) error {
	var versions []int64
	if err := tx.Select(&versions,
		`SELECT version FROM card_revision WHERE card_id = ? ORDER BY version DESC`, cardID); err != nil {
		return err
	}
	if len(versions) <= keep {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM card_revision WHERE card_id = ? AND version <= ?`, cardID, versions[keep])
	return err
}

// CardRevisionInfo is one retained version of a card, newest first.
type CardRevisionInfo struct {
	Version   int64  `db:"version" json:"version"`
	Timestamp int64  `db:"created_at" json:"timestamp"`
	Actor     string `db:"actor" json:"actor"`
}

// ListCardRevisions lists a card's retained versions, newest first.
func (c *Core) ListCardRevisions(ctx context.Context, projectID string, ref CardRef) ([]CardRevisionInfo, error) {
	var card Card
	out := []CardRevisionInfo{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		return tx.Select(&out,
			`SELECT version, created_at, actor FROM card_revision WHERE card_id = ? ORDER BY version DESC`, card.ID)
	})
	return out, err
}

// DiffCard returns a unified diff between two retained versions of a card,
// each rendered as "# <title>\n\n<body>".
func (c *Core) DiffCard(ctx context.Context, projectID string, ref CardRef, from, to int64) (RevisionDiff, error) {
	var card Card
	var rows []struct {
		Version int64  `db:"version"`
		Title   string `db:"title"`
		Body    string `db:"body_md"`
	}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		return tx.Select(&rows,
			`SELECT version, title, body_md FROM card_revision WHERE card_id = ? ORDER BY version`, card.ID)
	})
	if err != nil {
		return RevisionDiff{}, err
	}
	versions := make([]int64, len(rows))
	rendered := map[int64]string{}
	for i, r := range rows {
		versions[i] = r.Version
		rendered[r.Version] = "# " + r.Title + "\n\n" + r.Body
	}
	from, to, err = resolveDiffRange(versions, from, to, "trellis card history "+card.Ref)
	if err != nil {
		return RevisionDiff{}, err
	}
	diff := udiff.Unified(
		fmt.Sprintf("%s@v%d", card.Ref, from), fmt.Sprintf("%s@v%d", card.Ref, to),
		rendered[from], rendered[to])
	return RevisionDiff{From: from, To: to, Diff: diff}, nil
}
