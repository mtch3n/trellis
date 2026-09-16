package core

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// writeTemp creates a temp file beside path in the "."+name+".tmp-" pattern,
// writes data, and syncs and closes it. The caller links or renames it into
// place and removes it on any later failure. writeAtomic and
// replaceIfUnchanged both build on this so the temp-file dance exists once.
func writeTemp(dir, name string, data []byte) (string, error) {
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

// writeAtomic replaces path only after the complete contents have been
// written and synced. When replace is false, the final link is created with
// O_EXCL-like semantics, which keeps a stale orphan from being overwritten.
func writeAtomic(path string, data []byte, replace bool) error {
	dir := filepath.Dir(path)
	tmpName, err := writeTemp(dir, filepath.Base(path), data)
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
	return syncDirectory(dir)
}

// errFileChanged reports that a file no longer holds the bytes a write was
// based on.
var errFileChanged = errors.New("file changed on disk")

// replaceIfUnchanged replaces path with data only while path still holds the
// bytes whose ContentHash is base. The new bytes are written and synced to a
// temporary file first, so the check runs immediately before the rename.
//
// Trellis writers never race here: every caller holds SQLite's write lock. The
// check is for programs outside Trellis, such as an editor. It narrows their
// window to the moment between the check and the rename; no portable lock
// closes it, because editors do not take locks.
func replaceIfUnchanged(path string, data []byte, base string) error {
	dir := filepath.Dir(path)
	tmpName, err := writeTemp(dir, filepath.Base(path), data)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	cur, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if ContentHash(string(cur)) != base {
		return errFileChanged
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	keep = true
	return syncDirectory(dir)
}

// undoWrite puts back the bytes a failed write replaced, unless someone has
// written the file since: their bytes win and are picked up by the next read.
func undoWrite(path string, old []byte, written string) error {
	if err := replaceIfUnchanged(path, old, written); err != nil && !errors.Is(err, errFileChanged) {
		return err
	}
	return nil
}

func copyAtomic(path, source string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(tmp, in)
	closeErr := in.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(tmpName, path); err != nil {
		return err
	}
	if err := os.Remove(tmpName); err != nil {
		return err
	}
	keep = true
	return syncDirectory(dir)
}

type stagedRemoval struct {
	path  string
	trash string
}

// stageRemoval moves a file or a directory beside itself before a database
// transaction. The move is recoverable: restore brings it back if the
// transaction rolls back, while finalize removes the tombstone after commit.
func stageRemoval(path string) (*stagedRemoval, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return &stagedRemoval{path: path}, nil
	} else if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".delete-")
	if err != nil {
		return nil, err
	}
	trash := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(trash)
		return nil, err
	}
	if err := os.Remove(trash); err != nil {
		return nil, err
	}
	if err := os.Rename(path, trash); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		_ = os.Rename(trash, path)
		return nil, err
	}
	return &stagedRemoval{path: path, trash: trash}, nil
}

func (s *stagedRemoval) restore() error {
	if s == nil || s.trash == "" {
		return nil
	}
	if _, err := os.Stat(s.path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(s.trash, s.path); err != nil {
		return err
	}
	s.trash = ""
	return syncDirectory(filepath.Dir(s.path))
}

func (s *stagedRemoval) finalize() error {
	if s == nil || s.trash == "" {
		return nil
	}
	// RemoveAll, not Remove: a staged path may be a project directory or a
	// revision directory holding several files, and Remove refuses a
	// non-empty one. RemoveAll also treats an already-gone trash as success,
	// matching the ErrNotExist tolerance this had before.
	if err := os.RemoveAll(s.trash); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(s.path))
}
