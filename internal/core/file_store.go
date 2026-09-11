package core

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// writeAtomic replaces path only after the complete contents have been
// written and synced. When replace is false, the final link is created with
// O_EXCL-like semantics, which keeps a stale orphan from being overwritten.
func writeAtomic(path string, data []byte, replace bool) error {
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
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
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

// stageRemoval moves a file beside itself before a database transaction. The
// move is recoverable: restore brings it back if the transaction rolls back,
// while finalize removes the tombstone after commit.
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
	err := os.Remove(s.trash)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err == nil {
		err = syncDirectory(filepath.Dir(s.path))
	}
	return err
}
