package core

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jmoiron/sqlx"
)

// PruneHistory deletes only explicitly selected historical telemetry. Event
// deletion is caller-controlled because event.seq is also the sync cursor.
func (c *Core) PruneHistory(ctx context.Context, before int64, events, invocations bool) (int64, error) {
	if !events && !invocations {
		return 0, ErrUsage("nothing_to_prune", "select --events and/or --invocations", "trellis maintenance prune --before 90d --events")
	}
	if before <= 0 {
		return 0, ErrUsage("invalid_cutoff", "the cutoff must be in the past", "trellis maintenance prune --before 90d --events")
	}
	var total int64
	// Keep the implementation on the database connection rather than a
	// transaction: this allows VACUUM to follow immediately when requested.
	if events {
		res, err := c.db.ExecContext(ctx, `DELETE FROM event WHERE ts < ?`, before)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
	}
	if invocations {
		res, err := c.db.ExecContext(ctx, `DELETE FROM invocation WHERE ts < ?`, before)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// Compact checkpoints the WAL and rebuilds the main database so deleted
// telemetry pages are returned to the filesystem.
func (c *Core) Compact(ctx context.Context) error {
	if _, err := c.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("checkpoint WAL: %w", err)
	}
	if _, err := c.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum database: %w", err)
	}
	return nil
}

// PruneRevisions trims every knowledge entry's and every card's revisions
// down to history.keep. Capture already enforces the limit going forward;
// this is for after lowering it, when the excess would otherwise wait for
// the next write. It returns the number of revisions removed.
func (c *Core) PruneRevisions(ctx context.Context) (int64, error) {
	var docs []Knowledge
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&docs, `SELECT * FROM knowledge`)
	}); err != nil {
		return 0, err
	}
	var total int64
	for _, d := range docs {
		n, err := trimRevisions(d.Path, c.historyKeep)
		if err != nil {
			return total, err
		}
		total += int64(n)
	}

	var cardIDs []string
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&cardIDs, `SELECT DISTINCT card_id FROM card_revision`)
	}); err != nil {
		return total, err
	}
	for _, id := range cardIDs {
		if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
			var before, after int
			if err := tx.Get(&before, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, id); err != nil {
				return err
			}
			if err := trimCardRevisions(tx, id, c.historyKeep); err != nil {
				return err
			}
			if err := tx.Get(&after, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, id); err != nil {
				return err
			}
			total += int64(before - after)
			return nil
		}); err != nil {
			return total, err
		}
	}
	return total, nil
}

// PruneOrphanHistory removes revision directories whose entry file is gone --
// what deleting a file outside Trellis leaves behind, since nothing else
// notices. It returns the number of directories removed.
func (c *Core) PruneOrphanHistory(ctx context.Context) (int64, error) {
	var docs []Knowledge
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&docs, `SELECT * FROM knowledge`)
	}); err != nil {
		return 0, err
	}
	var removed int64
	for _, d := range docs {
		dir := revisionDir(d.Path)
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return removed, err
		}
		if _, err := os.Stat(d.Path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
