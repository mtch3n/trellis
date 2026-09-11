package resolve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Identity is how a working directory maps to a project.
type Identity struct {
	Kind         string // pin | env | remote | path
	Value        string
	RootPath     string
	SuggestedKey string
}

// Identify applies the resolution order: an explicit TRELLIS_PROJECT
// environment override, a .trellis pin walked up from dir, the repository's
// normalized remote, then the repository's top-level path. There is
// deliberately no cwd fallback: without that rule every session started in
// ~ or /tmp would silently create a project.
//
// The returned error is a plain error, not a *core.Error: package core
// already imports resolve for Identity and EnsureProject's signature, so
// resolve importing core back for its Error type would be an import cycle.
// The caller (the CLI layer that also knows the process's exit-code
// convention) is responsible for translating a non-nil error here into
// exit code 2.
func Identify(dir string) (Identity, error) {
	if v := os.Getenv("TRELLIS_PROJECT"); v != "" {
		return Identity{Kind: "env", Value: v, SuggestedKey: sanitizeKey(v)}, nil
	}

	// Repo is called before the pin lookup so the pin walk has a boundary:
	// the repository root when dir is inside one, $HOME otherwise. See
	// pinBoundary and findPin.
	info, inRepo := Repo(dir)
	boundary := pinBoundary(dir, info, inRepo)

	// Both sides are normalized before the walk compares them. git reports the
	// repository root through its resolved path -- /private/var on macOS for a
	// /var argument, the long form of an 8.3 name on Windows -- so comparing
	// the spellings as given lets the walk step straight past its boundary.
	key, root, err := findPin(normalizeDir(dir), normalizeDir(boundary))
	if err != nil {
		return Identity{}, err
	}
	if root != "" {
		return Identity{Kind: "pin", Value: key, RootPath: root,
			SuggestedKey: sanitizeKey(key)}, nil
	}

	if !inRepo {
		return Identity{}, errors.New("not inside a git repository and no .trellis pin found")
	}

	repoKey := sanitizeKey(filepath.Base(info.TopLevel))
	if id, ok := NormalizeRemote(info.Origin); ok {
		return Identity{Kind: "remote", Value: id, RootPath: info.TopLevel,
			SuggestedKey: repoKey}, nil
	}
	return Identity{Kind: "path", Value: info.TopLevel, RootPath: info.TopLevel,
		SuggestedKey: repoKey}, nil
}

// sanitizeKey upper-cases a candidate key and, when the result would start
// with a digit, prefixes it with P: a project key must start with a letter.
func sanitizeKey(s string) string {
	s = strings.ToUpper(s)
	if s != "" && (s[0] < 'A' || s[0] > 'Z') {
		s = "P" + s
	}
	return s
}

// findPin walks up from dir looking for a .trellis file holding a project key,
// stopping at stopAt (inclusive).
//
// The boundary is load-bearing. Without it a stray .trellis in a directory that
// happens to contain several repositories -- ~/Work, say -- silently merges
// every repository beneath it onto one board, with no error and no hint. The
// walk therefore stops at the repository root when inside one (a pin above the
// repository is not meant for that repository) and at $HOME otherwise.
//
// An existing but empty or whitespace-only .trellis is a HARD ERROR, never a
// silent skip.
func findPin(dir, stopAt string) (key, root string, err error) {
	for d := dir; ; {
		b, readErr := os.ReadFile(filepath.Join(d, ".trellis"))
		if readErr == nil {
			k := strings.TrimSpace(string(b))
			if k == "" {
				return "", "", fmt.Errorf("%s is empty; it must contain a project key", filepath.Join(d, ".trellis"))
			}
			return k, d, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return "", "", fmt.Errorf("reading %s: %w", filepath.Join(d, ".trellis"), readErr)
		}
		if d == stopAt {
			return "", "", nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", nil
		}
		d = parent
	}
}

// pinBoundary returns the highest directory findPin may inspect.
func pinBoundary(dir string, info RepoInfo, inRepo bool) string {
	if inRepo {
		return info.TopLevel
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return dir
}

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
