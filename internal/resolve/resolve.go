// Package resolve maps a working directory to the project it is pinned to.
// A .trellis file is the only link; see FindPin.
package resolve

import "path/filepath"

// normalizeDir resolves symlinks and canonicalizes a directory so that two
// spellings of the same place compare equal. A path that cannot be resolved --
// one that does not exist yet -- is only cleaned, which keeps this usable as a
// plain comparison helper.
func normalizeDir(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}
