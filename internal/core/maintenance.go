package core

import (
	"context"
	"fmt"
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
