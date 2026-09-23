package core

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Backup writes a consistent copy of the database with VACUUM INTO (§5).
// Copying the live file instead can capture a torn WAL.
func (c *Core) Backup(ctx context.Context, dest string) error {
	_, err := c.db.ExecContext(ctx, `VACUUM INTO ?`, dest)
	return err
}

// backupPrefix names the backups Trellis writes, so pruning can tell them
// from whatever else a person keeps in the same directory.
const backupPrefix = "trellis-backup-"

// BackupInto writes a consistent copy into a directory, named for the moment
// it was taken. The CLI's `backup <path>` names the file itself; a caller with
// nowhere in mind — the web UI's Back up now — needs the name chosen for it,
// and the name is what BackupsPrune recognises later.
func (c *Core) BackupInto(ctx context.Context, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := time.UnixMilli(c.clock.NowMS()).UTC().Format("20060102-150405")
	path := filepath.Join(dir, backupPrefix+stamp+".db")
	if _, err := os.Stat(path); err == nil {
		return "", ErrConflict("backup_exists", "a backup of this second already exists: "+path,
			"wait a second and try again")
	}
	if err := c.Backup(ctx, path); err != nil {
		return "", err
	}
	return path, nil
}

// BackupsPrune keeps the newest named backups in a directory and deletes the
// rest. Only files Trellis named are considered: a directory holding other
// things must come back holding them. Newest is read from the name, which
// records when the backup was taken; a file's modification time changes when
// it is copied or restored, and would delete the wrong ones.
func (c *Core) BackupsPrune(dir string, keep int) (int, error) {
	if keep < 1 {
		return 0, ErrUsage("invalid_keep", "keep at least one backup", "trellis backup prune <directory> --keep 5")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), backupPrefix) || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		names = append(names, entry.Name())
	}
	// The stamp in the name sorts as the time it records.
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(b, a) })
	deleted := 0
	for _, name := range names[min(keep, len(names)):] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
