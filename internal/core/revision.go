package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// revisionDir is the hidden directory beside an entry's file that holds its
// retained versions: "standup.md" -> ".standup.md". Slugify turns every
// character outside a-z0-9 into "-" and trims "-", so no entry or directory
// name this package creates can start with a dot; a revision directory can
// therefore never be mistaken for one (revision-history design, "Never
// mistaken for an entry").
func revisionDir(entryPath string) string {
	return filepath.Join(filepath.Dir(entryPath), "."+filepath.Base(entryPath))
}

// revisionFilePath is where one version of an entry is stored. A revision
// keeps the entry's own extension, so version 7 of "standup.md" is
// ".standup.md/7.md".
func revisionFilePath(entryPath string, version int64) string {
	return filepath.Join(revisionDir(entryPath), strconv.FormatInt(version, 10)+filepath.Ext(entryPath))
}

// parseRevisionVersion extracts a revision file's version number from its
// name, ignoring anything that is not "<positive integer><anything>".
func parseRevisionVersion(name string) (int64, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	v, err := strconv.ParseInt(base, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// sortedRevisionVersions returns a revision directory's retained version
// numbers, ascending. A missing directory is not an error: nothing has ever
// been captured there.
func sortedRevisionVersions(dir string) ([]int64, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var versions []int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if v, ok := parseRevisionVersion(e.Name()); ok {
			versions = append(versions, v)
		}
	}
	slices.Sort(versions)
	return versions, nil
}

// trimRevisions removes an entry's oldest revisions until at most keep
// remain, ordered by version. It returns the number removed.
func trimRevisions(entryPath string, keep int) (int, error) {
	versions, err := sortedRevisionVersions(revisionDir(entryPath))
	if err != nil {
		return 0, err
	}
	removed := 0
	for len(versions) > keep {
		if err := os.Remove(revisionFilePath(entryPath, versions[0])); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		versions = versions[1:]
		removed++
	}
	if removed > 0 {
		if err := syncDirectory(revisionDir(entryPath)); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// removeRevisionDirIfEmpty removes an entry's revision directory when it
// holds no files, so a create that failed after writing version 1 leaves
// nothing behind for orphan detection to later report.
func removeRevisionDirIfEmpty(entryPath string) error {
	dir := revisionDir(entryPath)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(dir); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(dir))
}

// captureKnowledgeRevision retains raw as version's copy of entryPath, unless
// capture is disabled (historyKeep == 0), that version is already retained,
// or raw is byte-identical to the newest retained revision — the two rules
// that keep history free of repeats (revision-history design). It then trims
// down to historyKeep.
func (c *Core) captureKnowledgeRevision(entryPath string, version int64, raw []byte) error {
	if c.historyKeep == 0 {
		return nil
	}
	dest := revisionFilePath(entryPath, version)
	if _, err := os.Stat(dest); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := revisionDir(entryPath)
	versions, err := sortedRevisionVersions(dir)
	if err != nil {
		return err
	}
	if len(versions) > 0 {
		prior, err := os.ReadFile(revisionFilePath(entryPath, versions[len(versions)-1]))
		if err != nil {
			return err
		}
		if string(prior) == string(raw) {
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeAtomic(dest, raw, false); err != nil {
		return err
	}
	_, err = trimRevisions(entryPath, c.historyKeep)
	return err
}

// moveDir moves the directory at src into destDir, refusing to replace
// anything already there. Directories cannot be hard-linked the way moveFile
// links a file to get that refusal for free, so the destination is checked
// first — os.Rename silently replaces an empty directory it is given no
// chance to refuse. Rename is atomic and cheap on the common case (same
// filesystem); when it fails for any other reason, this falls back to a
// recursive copy, removing the source only once the copy is synced. src not
// existing is not an error: an entry created before this feature, or with
// capture disabled, may have no revision directory to move.
func moveDir(src, destDir string) (string, error) {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, baseName(src))
	if _, err := os.Stat(dest); err == nil {
		return "", ErrConflict("path_taken", dest+" already exists", "")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(src, dest); err != nil {
		if cerr := copyDirAtomic(dest, src); cerr != nil {
			return "", cerr
		}
		if err := os.RemoveAll(src); err != nil {
			return "", err
		}
	}
	if err := syncDirectory(destDir); err != nil {
		return "", err
	}
	return dest, syncDirectory(filepath.Dir(src))
}

// moveDirBack undoes a successful moveDir: dest moves back beside src. A
// moveDir that found nothing to move returns "", so dest may be empty here;
// that is a no-op, not an error.
func moveDirBack(dest, src string) error {
	if dest == "" {
		return nil
	}
	_, err := moveDir(dest, filepath.Dir(src))
	return err
}

// copyDirAtomic copies a flat directory of revision files. Revision
// directories never nest — each holds only "<version><ext>" files — so this
// need not recurse.
func copyDirAtomic(dest, src string) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyAtomic(filepath.Join(dest, e.Name()), filepath.Join(src, e.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(dest)
}
