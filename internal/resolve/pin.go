package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mtch3n/trellis/internal/vpath"
)

// PinFile is the file that links a directory to a project.
const PinFile = ".trellis"

// ErrNotRegular marks a .trellis entry that is not a regular file, such as
// the storage root $HOME/.trellis. The walk skips it; init refuses it.
var ErrNotRegular = errors.New("not a regular file")

// Pin is a .trellis file and the virtual path it names.
type Pin struct {
	Path   string // absolute path of the file
	Target vpath.Path
}

// PinError is a .trellis entry that cannot serve as a pin: malformed content,
// or not a regular file.
type PinError struct {
	Path string
	Err  error
}

func (e *PinError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *PinError) Unwrap() error { return e.Err }

// ReadPin parses the pin at path. A missing file yields an error wrapping
// fs.ErrNotExist; a symlink whose target is missing does not, because that is
// a broken pin rather than an absent one.
func ReadPin(path string) (Pin, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if _, lerr := os.Lstat(path); lerr == nil {
			return Pin{}, fmt.Errorf("%s is a symlink to a missing file", path)
		}
	}
	if err != nil {
		return Pin{}, err
	}
	if !info.Mode().IsRegular() {
		return Pin{}, &PinError{Path: path, Err: ErrNotRegular}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Pin{}, err
	}
	target, err := vpath.ParsePin(string(b))
	if err != nil {
		return Pin{}, &PinError{Path: path, Err: err}
	}
	return Pin{Path: path, Target: target}, nil
}

// FindPin walks up from dir to the nearest pin. found is false when no pin
// applies to dir.
//
// The walk never inspects $HOME or the filesystem root: a pin there would
// capture every directory beneath it, and $HOME/.trellis is the default
// storage root. It stops after a directory that contains .git, so a pin above
// a repository never applies to it. Pins are committed, and one outside the
// repository would make the same repository resolve differently depending on
// where it was cloned. .git is only a stop sign: nothing is read from it, and
// git is never run.
//
// Any failure other than absence is an error. Treating an unreadable .trellis
// or .git as missing would let the walk continue to an ancestor's pin and
// send work to the wrong board.
func FindPin(dir string) (pin Pin, found bool, err error) {
	start, err := canonicalDir(dir)
	if err != nil {
		return Pin{}, false, err
	}
	home := homeDir()
	for d := start; ; {
		parent := filepath.Dir(d)
		if d == home || parent == d {
			return Pin{}, false, nil
		}
		p, err := ReadPin(filepath.Join(d, PinFile))
		switch {
		case err == nil:
			return p, true, nil
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrNotRegular):
		default:
			return Pin{}, false, err
		}
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return Pin{}, false, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Pin{}, false, err
		}
		d = parent
	}
}

// Unpinnable says why a pin written in dir would never be read, or returns
// "" when it would be.
func Unpinnable(dir string) (reason string, err error) {
	d, err := canonicalDir(dir)
	if err != nil {
		return "", err
	}
	switch {
	case filepath.Dir(d) == d:
		return "the filesystem root is never searched for a pin", nil
	case d == homeDir():
		return "the home directory is never searched for a pin", nil
	}
	return "", nil
}

// canonicalDir resolves dir to an absolute path with symlinks evaluated.
// Unlike normalizeDir it does not fall back to lexical cleaning: the walk
// starts from a directory that must exist.
func canonicalDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", dir, err)
	}
	return filepath.Clean(resolved), nil
}

// homeDir is the normalized home directory, or "" when there is none; then
// only the filesystem root bounds the walk.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	return normalizeDir(h)
}
