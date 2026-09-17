package resolve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/address"
)

// isolateHome points $HOME (and Windows' USERPROFILE) at a fresh directory so
// the developer's real home never takes part in a walk.
func isolateHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	return h
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func markerAt(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustFind(t *testing.T, dir string) Marker {
	t.Helper()
	marker, found, err := FindMarker(dir)
	if err != nil {
		t.Fatalf("FindMarker(%s): %v", dir, err)
	}
	if !found {
		t.Fatalf("FindMarker(%s) found nothing", dir)
	}
	return marker
}

func mustNotFind(t *testing.T, dir string) {
	t.Helper()
	marker, found, err := FindMarker(dir)
	if err != nil {
		t.Fatalf("FindMarker(%s): %v", dir, err)
	}
	if found {
		t.Fatalf("FindMarker(%s) = %+v, want nothing", dir, marker)
	}
}

func TestFindMarkerNearestWins(t *testing.T) {
	isolateHome(t)
	repo := mkdir(t, t.TempDir(), "mono")
	mkdir(t, repo, ".git")
	markerAt(t, repo, "/MONO\n")
	api := mkdir(t, repo, "api")
	markerAt(t, api, "/API/boards/api\n")
	src := mkdir(t, api, "src", "deep")
	web := mkdir(t, repo, "web")

	if got := mustFind(t, src); got.Target != address.Board("API", "api") {
		t.Errorf("from api/src/deep: %+v, want /API/boards/api", got.Target)
	}
	got := mustFind(t, web)
	if got.Target.Project != "MONO" {
		t.Errorf("from web: %+v, want /MONO", got.Target)
	}
	if want := filepath.Join(normalizeDir(repo), MarkerFile); got.Path != want {
		t.Errorf("marker path = %s, want %s", got.Path, want)
	}
}

func TestFindMarkerStopsAtGitDirectory(t *testing.T) {
	isolateHome(t)
	parent := t.TempDir()
	markerAt(t, parent, "/STRAY\n")
	repo := mkdir(t, parent, "repo")
	mkdir(t, repo, ".git")
	mustNotFind(t, mkdir(t, repo, "sub"))
}

// A worktree's .git is a file, and must stop the walk just like a directory.
func TestFindMarkerStopsAtGitFile(t *testing.T) {
	isolateHome(t)
	parent := t.TempDir()
	markerAt(t, parent, "/STRAY\n")
	tree := mkdir(t, parent, "worktree")
	if err := os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustNotFind(t, tree)
}

// A marker at a repository root is found: the marker check comes before the
// stop.
func TestFindMarkerReadsTheRepositoryRoot(t *testing.T) {
	isolateHome(t)
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	markerAt(t, repo, "/ROOT\n")
	if got := mustFind(t, repo); got.Target.Project != "ROOT" {
		t.Errorf("got %+v", got.Target)
	}
}

// The home directory is never inspected. Using a TempDir as $HOME also
// exercises normalization on macOS, where it is spelled /var/... and resolves
// to /private/var/....
func TestFindMarkerNeverReadsHome(t *testing.T) {
	h := isolateHome(t)
	markerAt(t, h, "/HOMEMARKER\n")
	mustNotFind(t, mkdir(t, h, "project"))
}

// The regression test for the storage root: $HOME/.trellis is a directory.
// Any .trellis that is not a regular file is skipped, not read and not fatal.
func TestFindMarkerSkipsATrellisDirectory(t *testing.T) {
	isolateHome(t)
	top := t.TempDir()
	mkdir(t, top, ".git")
	markerAt(t, top, "/TOP\n")
	mid := mkdir(t, top, "mid")
	mkdir(t, mid, MarkerFile)
	if got := mustFind(t, mid); got.Target.Project != "TOP" {
		t.Errorf("got %+v, want the marker above the .trellis directory", got.Target)
	}
}

func TestFindMarkerFollowsASymlinkedMarker(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "shared-marker")
	if err := os.WriteFile(target, []byte("/LINKED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, MarkerFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mkdir(t, dir, ".git")
	if got := mustFind(t, dir); got.Target.Project != "LINKED" {
		t.Errorf("got %+v", got.Target)
	}
}

func TestFindMarkerDanglingSymlinkIsAnError(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, MarkerFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, found, err := FindMarker(dir)
	if err == nil || found {
		t.Fatalf("FindMarker = found %v, err %v; want an error", found, err)
	}
}

func TestFindMarkerMalformedIsAMarkerError(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	markerAt(t, dir, "TRELLIS\n")
	_, _, err := FindMarker(dir)
	me, ok := errors.AsType[*MarkerError](err)
	if !ok {
		t.Fatalf("error = %v, want *MarkerError", err)
	}
	if !strings.Contains(me.Error(), "old bare-key format") || !strings.HasSuffix(me.Path, MarkerFile) {
		t.Errorf("MarkerError = %q (path %s)", me.Error(), me.Path)
	}
}

// An unreadable marker must never let the walk continue to an ancestor's
// marker.
func TestFindMarkerUnreadableMarkerIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny reads here")
	}
	isolateHome(t)
	parent := t.TempDir()
	markerAt(t, parent, "/PARENT\n")
	child := mkdir(t, parent, "child")
	markerAt(t, child, "/CHILD\n")
	if err := os.Chmod(filepath.Join(child, MarkerFile), 0o000); err != nil {
		t.Fatal(err)
	}
	_, found, err := FindMarker(child)
	if err == nil || found || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("FindMarker = found %v, err %v; want a permission error", found, err)
	}
}

func TestFindMarkerUnsearchableDirectoryIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny access here")
	}
	isolateHome(t)
	parent := t.TempDir()
	markerAt(t, parent, "/PARENT\n")
	locked := mkdir(t, parent, "locked")
	inner := mkdir(t, locked, "inner")
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if _, found, err := FindMarker(inner); err == nil || found {
		t.Fatalf("FindMarker = found %v, err %v; want an error", found, err)
	}
}

func TestFindMarkerWithoutAHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	dir := t.TempDir()
	markerAt(t, dir, "/NOHOME\n")
	if got := mustFind(t, dir); got.Target.Project != "NOHOME" {
		t.Errorf("got %+v", got.Target)
	}
}

func TestUnmarkable(t *testing.T) {
	h := isolateHome(t)
	if reason, err := Unmarkable(h); err != nil || reason == "" {
		t.Errorf("Unmarkable(home) = %q, %v; want a reason", reason, err)
	}
	root := filepath.VolumeName(h) + string(filepath.Separator)
	if reason, err := Unmarkable(root); err != nil || reason == "" {
		t.Errorf("Unmarkable(%s) = %q, %v; want a reason", root, reason, err)
	}
	if reason, err := Unmarkable(mkdir(t, h, "project")); err != nil || reason != "" {
		t.Errorf("Unmarkable(project) = %q, %v; want none", reason, err)
	}
}

func TestScanRootIsTheEnclosingRepository(t *testing.T) {
	isolateHome(t)
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	if got, err := ScanRoot(mkdir(t, repo, "a", "b")); err != nil || got != normalizeDir(repo) {
		t.Errorf("inside a repository: %s, %v", got, err)
	}
	loose := t.TempDir()
	if got, err := ScanRoot(loose); err != nil || got != normalizeDir(loose) {
		t.Errorf("outside any repository: %s, %v", got, err)
	}
}

func TestMarkersUnderSkipsGitAndNestedRepositories(t *testing.T) {
	isolateHome(t)
	repo := normalizeDir(t.TempDir())
	mkdir(t, repo, ".git")
	markerAt(t, repo, "/MONO\n")
	markerAt(t, mkdir(t, repo, "api"), "/API\n")
	markerAt(t, mkdir(t, repo, ".git", "hooks"), "/HIDDEN\n")
	nested := mkdir(t, repo, "vendor", "lib")
	mkdir(t, nested, ".git")
	markerAt(t, nested, "/LIB\n")
	markerAt(t, mkdir(t, repo, "old"), "OLD\n")

	markers, skipped, err := MarkersUnder(repo)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range markers {
		keys = append(keys, p.Target.Project)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"API", "MONO"}) {
		t.Errorf("markers = %v", keys)
	}
	if !slices.Equal(skipped, []string{filepath.Join(repo, "old", MarkerFile)}) {
		t.Errorf("skipped = %v", skipped)
	}
}
