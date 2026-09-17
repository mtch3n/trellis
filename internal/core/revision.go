package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/mtch3n/trellis/internal/atomicfile"
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
		if err := atomicfile.SyncDir(revisionDir(entryPath)); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// removeRevisionDirIfEmpty removes an entry's revision directory when it
// holds no files, so a create that failed after writing version 1 leaves
// nothing behind for leftover detection to later report.
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
	return atomicfile.SyncDir(filepath.Dir(dir))
}

// revisionToKeep says whether raw should be retained as version's copy of
// entryPath, and where. It should not when capture is disabled
// (historyKeep == 0), that version is already retained, or raw is
// byte-identical to the newest retained revision — the two rules that keep
// history free of repeats (revision-history design).
func (c *Core) revisionToKeep(entryPath string, version int64, raw []byte) (string, bool, error) {
	if c.historyKeep == 0 {
		return "", false, nil
	}
	dest := revisionFilePath(entryPath, version)
	if _, err := os.Stat(dest); err == nil {
		return "", false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	versions, err := sortedRevisionVersions(revisionDir(entryPath))
	if err != nil {
		return "", false, err
	}
	if len(versions) > 0 {
		prior, err := os.ReadFile(revisionFilePath(entryPath, versions[len(versions)-1]))
		if err != nil {
			return "", false, err
		}
		if string(prior) == string(raw) {
			return "", false, nil
		}
	}
	return dest, true, nil
}

// captureEntryRevision retains raw as version's copy of entryPath when
// revisionToKeep says to, then trims down to historyKeep. It returns the path
// written, or "" when revisionToKeep declined (capture disabled, that version
// already retained, or raw unchanged from the newest revision) -- so a caller
// that captures a version speculatively, before the write it belongs to is
// known to have landed, can remove exactly this file if it does not.
func (c *Core) captureEntryRevision(entryPath string, version int64, raw []byte) (string, error) {
	dest, keep, err := c.revisionToKeep(entryPath, version, raw)
	if err != nil || !keep {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := atomicfile.Write(dest, raw, false); err != nil {
		return "", err
	}
	if _, err := trimRevisions(entryPath, c.historyKeep); err != nil {
		return "", err
	}
	return dest, nil
}

// discardCapturedRevision removes a revision file captureEntryRevision
// wrote, when the write it was speculatively captured for turned out not to
// land. A no-op for "" (nothing was written) and for a file already gone.
func discardCapturedRevision(dest string) error {
	if dest == "" {
		return nil
	}
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(dest)
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
	return atomicfile.SyncDir(filepath.Dir(dir))
}

// revisionFiles lists the files in an entry's revision directory, none when
// it has none.
func revisionFiles(entryPath string) ([]string, error) {
	dir := revisionDir(entryPath)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
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
	return atomicfile.SyncDir(dest)
}

// RevisionInfo is one retained version of an entry, newest first.
type RevisionInfo struct {
	Version   int64 `json:"version"`
	Timestamp int64 `json:"timestamp"`
}

// RevisionDiff is a unified diff between two retained versions.
type RevisionDiff struct {
	From int64  `json:"from"`
	To   int64  `json:"to"`
	Diff string `json:"diff"`
}

// resolveDiffRange picks the two versions a diff compares, from a sorted
// (ascending) list of retained versions. With neither from nor to given, to
// is the latest retained version and from is the one immediately before it.
// Given only one, the other is resolved the same way relative to it: --to 8
// alone diffs the version before 8 against 8; --from 5 alone diffs 5 against
// the current latest.
func resolveDiffRange(versions []int64, from, to int64, historyCmd string) (int64, int64, error) {
	retained := func(v int64) bool {
		_, ok := slices.BinarySearch(versions, v)
		return ok
	}
	rangeMsg := func() string {
		if len(versions) == 0 {
			return "no revisions are retained"
		}
		return fmt.Sprintf("the retained range is %d-%d", versions[0], versions[len(versions)-1])
	}
	if to != 0 {
		if !retained(to) {
			return 0, 0, ErrUsage("revision_not_retained",
				fmt.Sprintf("version %d is not retained; %s", to, rangeMsg()), historyCmd)
		}
	} else {
		if len(versions) == 0 {
			return 0, 0, ErrUsage("no_revisions", "no revisions are retained", historyCmd)
		}
		to = versions[len(versions)-1]
	}
	if from != 0 {
		if !retained(from) {
			return 0, 0, ErrUsage("revision_not_retained",
				fmt.Sprintf("version %d is not retained; %s", from, rangeMsg()), historyCmd)
		}
	} else {
		idx, _ := slices.BinarySearch(versions, to)
		if idx == 0 {
			return 0, 0, ErrUsage("no_earlier_revision",
				fmt.Sprintf("version %d has no earlier retained revision; %s", to, rangeMsg()), historyCmd)
		}
		from = versions[idx-1]
	}
	return from, to, nil
}

// ListEntryRevisions lists an entry's retained versions, newest first.
// Entry revisions carry no actor: the entry file has no author field, and
// a direct edit has no Trellis actor at all.
func (c *Core) ListEntryRevisions(ctx context.Context, projectID, slug string) ([]RevisionInfo, error) {
	entry, err := c.LoadEntry(ctx, projectID, slug)
	if err != nil {
		return nil, err
	}
	versions, err := sortedRevisionVersions(revisionDir(entry.Path))
	if err != nil {
		return nil, err
	}
	out := make([]RevisionInfo, len(versions))
	for i, v := range versions {
		st, err := os.Stat(revisionFilePath(entry.Path, v))
		if err != nil {
			return nil, err
		}
		out[len(versions)-1-i] = RevisionInfo{Version: v, Timestamp: st.ModTime().UnixMilli()}
	}
	return out, nil
}

// DiffEntry returns a unified diff between two retained versions of an
// entry's whole file, frontmatter included.
func (c *Core) DiffEntry(ctx context.Context, projectID, slug string, from, to int64) (RevisionDiff, error) {
	entry, err := c.LoadEntry(ctx, projectID, slug)
	if err != nil {
		return RevisionDiff{}, err
	}
	versions, err := sortedRevisionVersions(revisionDir(entry.Path))
	if err != nil {
		return RevisionDiff{}, err
	}
	from, to, err = resolveDiffRange(versions, from, to, "trellis knowledge history "+entry.Slug)
	if err != nil {
		return RevisionDiff{}, err
	}
	fromRaw, err := os.ReadFile(revisionFilePath(entry.Path, from))
	if err != nil {
		return RevisionDiff{}, err
	}
	toRaw, err := os.ReadFile(revisionFilePath(entry.Path, to))
	if err != nil {
		return RevisionDiff{}, err
	}
	diff := udiff.Unified(
		fmt.Sprintf("%s@v%d", entry.Slug, from), fmt.Sprintf("%s@v%d", entry.Slug, to),
		string(fromRaw), string(toRaw))
	return RevisionDiff{From: from, To: to, Diff: diff}, nil
}
