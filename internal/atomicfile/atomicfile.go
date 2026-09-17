// Package atomicfile writes files so a crash leaves either the old bytes or
// the new ones, never a mix. Both core and the store's migrations write vault
// files, and the temp-file dance must exist once.
package atomicfile

import (
	"os"
	"path/filepath"
)

// WriteTemp creates a temp file beside path in the "."+name+".tmp-" pattern,
// writes data, and syncs and closes it. The caller links or renames it into
// place and removes it on any later failure. Write and replaceIfUnchanged
// both build on this so the temp-file dance exists once.
func WriteTemp(dir, name string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	ok = true
	return tmpName, nil
}

// Write replaces path only after the complete contents have been written and
// synced. When replace is false, the final link is created with
// O_EXCL-like semantics, which keeps a stale orphan from being overwritten.
func Write(path string, data []byte, replace bool) error {
	dir := filepath.Dir(path)
	tmpName, err := WriteTemp(dir, filepath.Base(path), data)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	if replace {
		err = os.Rename(tmpName, path)
	} else {
		err = os.Link(tmpName, path)
		if err == nil {
			err = os.Remove(tmpName)
		}
	}
	if err != nil {
		return err
	}
	keep = true
	return SyncDir(dir)
}
