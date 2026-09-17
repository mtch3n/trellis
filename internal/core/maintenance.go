package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/atomicfile"
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

// entriesWithPaths returns entry rows with Path derived and filled in,
// scoped to projectID (empty means every project). It exists for maintenance
// and health code that walks a file directly rather than through
// loadEntry/refreshFromFile, which set Path as a side effect of reading one
// entry's own file.
func (c *Core) entriesWithPaths(tx *sqlx.Tx, projectID string) ([]Entry, error) {
	q := `SELECT k.*, p.key AS pkey FROM entry k JOIN project p ON p.id = k.project_id`
	var args []any
	if projectID != "" {
		q += ` WHERE k.project_id = ?`
		args = append(args, projectID)
	}
	var rows []struct {
		Entry
		Key string `db:"pkey"`
	}
	if err := tx.Select(&rows, q, args...); err != nil {
		return nil, err
	}
	entries := make([]Entry, len(rows))
	for i, r := range rows {
		entries[i] = r.Entry
		entries[i].Path = c.entryPath(r.Key, entries[i].Global, entries[i].Slug)
	}
	return entries, nil
}

// PruneRevisions trims every entry's and every card's revisions
// down to history.keep. Capture already enforces the limit going forward;
// this is for after lowering it, when the excess would otherwise wait for
// the next write. It returns the number of revisions removed.
func (c *Core) PruneRevisions(ctx context.Context) (int64, error) {
	var entries []Entry
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		entries, err = c.entriesWithPaths(tx, "")
		return err
	}); err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		n, err := trimRevisions(e.Path, c.historyKeep)
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

// ParseRetention parses a maintenance retention age: "90d", "12w", or any Go
// duration string time.ParseDuration accepts. internal/cli's `maintenance
// prune --before` and the settings API's prune route share this, so "90d"
// means the same age from either caller.
func ParseRetention(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return 0, fmt.Errorf("retention is required")
	}
	if strings.HasSuffix(raw, "d") || strings.HasSuffix(raw, "w") {
		unit := time.Hour * 24
		if strings.HasSuffix(raw, "w") {
			unit *= 7
		}
		n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(raw, "d"), "w"), 64)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid retention %q", raw)
		}
		return time.Duration(n * float64(unit)), nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid retention %q", raw)
	}
	return d, nil
}

// OrphanHistoryCount reports how many revision directories no entry accounts
// for, across every project and the global vault -- what the settings page's
// maintenance stats show before a prune.
func (c *Core) OrphanHistoryCount(ctx context.Context) (int, error) {
	orphans, err := c.orphanRevisionDirs(ctx, "")
	if err != nil {
		return 0, err
	}
	return len(orphans), nil
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
		if err := atomicfile.SyncDir(filepath.Dir(dir)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// orphanRevisionDirs lists every ".<name>/" directory in the vaults that no
// entry row accounts for, sorted. An empty projectID covers every project
// and the global vault; otherwise it covers that project's vault and the
// directories its own entries live in. Liveness is always checked against
// every project's rows, never just the ones a project filter selects: the
// global vault is walked unconditionally, and another project's entry
// promoted into it must not be reported as this project's orphan.
func (c *Core) orphanRevisionDirs(ctx context.Context, projectID string) ([]string, error) {
	var allEntries []Entry
	var keys []string
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		if allEntries, err = c.entriesWithPaths(tx, ""); err != nil {
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

	// The vaults to walk: every project's own directory, plus the global one.
	vaults := map[string]bool{c.vaultDir(GlobalKey, true): true}
	for _, key := range keys {
		vaults[c.vaultDir(key, false)] = true
	}
	live := make(map[string]bool, len(allEntries))
	for _, e := range allEntries {
		live[e.Path] = true
		if projectID == "" || e.ProjectID == projectID {
			vaults[filepath.Dir(e.Path)] = true
		}
	}
	// A vault nested under another one being walked is walked twice, once by
	// the ancestor's recursion and once on its own: drop it here so an orphan
	// inside it is not reported (and removed) twice.
	vaults = topLevelDirs(vaults)

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
			if looksLikeRevisionDir(path, e.Name()) {
				if !live[filepath.Join(filepath.Dir(path), strings.TrimPrefix(e.Name(), "."))] {
					orphans = append(orphans, path)
				}
			}
			// Revision directories never nest, and neither does anything else
			// worth walking into here (.git, .obsidian, ...): stop either way.
			return fs.SkipDir
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(orphans)
	return orphans, nil
}

// looksLikeRevisionDir reports whether the directory at path, whose name is
// name, could be an entry's revision directory: its name must be a
// dot followed by something ending in ".md" (the only extension an
// entry file has), and every entry inside it must be a regular file whose
// name parseRevisionVersion accepts. Anything else — ".git", ".obsidian", a
// user's own dot-directory — is left alone, never walked into and never
// treated as an orphan.
func looksLikeRevisionDir(path, name string) bool {
	if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			return false
		}
		if _, ok := parseRevisionVersion(e.Name()); !ok {
			return false
		}
	}
	return true
}

// topLevelDirs returns the entries of dirs that are not nested under another
// entry of dirs, so a caller that recurses into each one never visits the
// same subtree twice.
func topLevelDirs(dirs map[string]bool) map[string]bool {
	list := make([]string, 0, len(dirs))
	for d := range dirs {
		list = append(list, d)
	}
	slices.Sort(list)
	top := make(map[string]bool, len(list))
	var kept []string
	for _, d := range list {
		nested := false
		for _, p := range kept {
			if d == p || strings.HasPrefix(d, p+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			kept = append(kept, d)
			top[d] = true
		}
	}
	return top
}
