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
