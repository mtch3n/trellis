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
// things must come back holding them.
func (c *Core) BackupsPrune(dir string, keep int) (int, error) {
	if keep < 1 {
		return 0, ErrUsage("invalid_keep", "keep at least one backup", "trellis backup prune <directory> --keep 5")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	type backup struct {
		path    string
		modTime time.Time
	}
	backups := make([]backup, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), backupPrefix) || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, err
		}
		backups = append(backups, backup{filepath.Join(dir, entry.Name()), info.ModTime()})
	}
	slices.SortFunc(backups, func(a, b backup) int { return b.modTime.Compare(a.modTime) })
	deleted := 0
	for _, item := range backups[min(keep, len(backups)):] {
		if err := os.Remove(item.path); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
