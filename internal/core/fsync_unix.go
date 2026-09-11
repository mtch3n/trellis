//go:build !windows

package core

import "os"

// syncDirectory flushes a directory entry so a rename or link survives a crash.
// Writing the file is not enough: the entry naming it lives in the directory,
// and that has to reach the disk too.
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
