package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

// PruneOrphanHistory removes revision directories no entry accounts for --
// what deleting a file and its row outside Trellis leaves behind. It returns
// the number of directories removed.
//
// An entry whose file is missing is NOT one of these. The entry is still
// registered, `knowledge lint` reports the missing file, and its history is
// the only copy of that content left: deleting it here would finish the job
// the accidental `rm` started.
func (c *Core) PruneOrphanHistory(ctx context.Context) (int64, error) {
	orphans, err := c.orphanRevisionDirs(ctx, "")
	if err != nil {
		return 0, err
	}
	var removed int64
	for _, dir := range orphans {
		if err := os.RemoveAll(dir); err != nil {
			return removed, err
		}
		if err := syncDirectory(filepath.Dir(dir)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// orphanRevisionDirs lists every ".<name>/" directory in the vaults that no
// knowledge row accounts for, sorted. An empty projectID covers every project
// and the global vault; otherwise it covers that project's vault and the
// directories its own entries live in.
func (c *Core) orphanRevisionDirs(ctx context.Context, projectID string) ([]string, error) {
	var docs []Knowledge
	var keys []string
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		q := `SELECT * FROM knowledge`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id = ?`
			args = append(args, projectID)
		}
		if err := tx.Select(&docs, q, args...); err != nil {
			return err
		}
		kq := `SELECT key FROM project`
		kargs := []any{}
		if projectID != "" {
			kq += ` WHERE id = ?`
			kargs = append(kargs, projectID)
		}
		return tx.Select(&keys, kq, kargs...)
	}); err != nil {
		return nil, err
	}

	root, err := c.root()
	if err != nil {
		return nil, err
	}
	// The vaults to walk: every project's own directory, the global one, and
	// wherever the rows actually point, which covers a moved storage root.
	vaults := map[string]bool{filepath.Join(root, "global", "knowledge"): true}
	for _, key := range keys {
		vaults[filepath.Join(root, "projects", key, "knowledge")] = true
	}
	live := make(map[string]bool, len(docs))
	for _, d := range docs {
		live[d.Path] = true
		vaults[filepath.Dir(d.Path)] = true
	}

	var orphans []string
	for vault := range vaults {
		err := filepath.WalkDir(vault, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if path == vault || !e.IsDir() {
				return nil
			}
			if !strings.HasPrefix(e.Name(), ".") {
				return nil
			}
			// A revision directory is ".<entry file name>" beside its entry.
			if !live[filepath.Join(filepath.Dir(path), strings.TrimPrefix(e.Name(), "."))] {
				orphans = append(orphans, path)
			}
			return fs.SkipDir
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(orphans)
	return orphans, nil
}
