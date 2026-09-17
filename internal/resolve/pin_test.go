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

func pinAt(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, PinFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustFind(t *testing.T, dir string) Pin {
	t.Helper()
	pin, found, err := FindPin(dir)
	if err != nil {
		t.Fatalf("FindPin(%s): %v", dir, err)
	}
	if !found {
		t.Fatalf("FindPin(%s) found nothing", dir)
	}
	return pin
}

func mustNotFind(t *testing.T, dir string) {
	t.Helper()
	pin, found, err := FindPin(dir)
	if err != nil {
		t.Fatalf("FindPin(%s): %v", dir, err)
	}
	if found {
		t.Fatalf("FindPin(%s) = %+v, want nothing", dir, pin)
	}
}

func TestFindPinNearestWins(t *testing.T) {
	isolateHome(t)
	repo := mkdir(t, t.TempDir(), "mono")
	mkdir(t, repo, ".git")
	pinAt(t, repo, "/MONO\n")
	api := mkdir(t, repo, "api")
	pinAt(t, api, "/API/boards/api\n")
	src := mkdir(t, api, "src", "deep")
	web := mkdir(t, repo, "web")

	if got := mustFind(t, src); got.Target != address.Board("API", "api") {
		t.Errorf("from api/src/deep: %+v, want /API/boards/api", got.Target)
	}
	got := mustFind(t, web)
	if got.Target.Project != "MONO" {
		t.Errorf("from web: %+v, want /MONO", got.Target)
	}
	if want := filepath.Join(normalizeDir(repo), PinFile); got.Path != want {
		t.Errorf("pin path = %s, want %s", got.Path, want)
	}
}

func TestFindPinStopsAtGitDirectory(t *testing.T) {
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/STRAY\n")
	repo := mkdir(t, parent, "repo")
	mkdir(t, repo, ".git")
	mustNotFind(t, mkdir(t, repo, "sub"))
}

// A worktree's .git is a file, and must stop the walk just like a directory.
func TestFindPinStopsAtGitFile(t *testing.T) {
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/STRAY\n")
	tree := mkdir(t, parent, "worktree")
	if err := os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustNotFind(t, tree)
}

// A pin at a repository root is found: the pin check comes before the stop.
func TestFindPinReadsTheRepositoryRoot(t *testing.T) {
	isolateHome(t)
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	pinAt(t, repo, "/ROOT\n")
	if got := mustFind(t, repo); got.Target.Project != "ROOT" {
		t.Errorf("got %+v", got.Target)
	}
}

// The home directory is never inspected. Using a TempDir as $HOME also
// exercises normalization on macOS, where it is spelled /var/... and resolves
// to /private/var/....
func TestFindPinNeverReadsHome(t *testing.T) {
	h := isolateHome(t)
	pinAt(t, h, "/HOMEPIN\n")
	mustNotFind(t, mkdir(t, h, "project"))
}

// The regression test for the storage root: $HOME/.trellis is a directory.
// Any .trellis that is not a regular file is skipped, not read and not fatal.
func TestFindPinSkipsATrellisDirectory(t *testing.T) {
	isolateHome(t)
	top := t.TempDir()
	mkdir(t, top, ".git")
	pinAt(t, top, "/TOP\n")
	mid := mkdir(t, top, "mid")
	mkdir(t, mid, PinFile)
	if got := mustFind(t, mid); got.Target.Project != "TOP" {
		t.Errorf("got %+v, want the pin above the .trellis directory", got.Target)
	}
}

func TestFindPinFollowsASymlinkedPin(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "shared-pin")
	if err := os.WriteFile(target, []byte("/LINKED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, PinFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mkdir(t, dir, ".git")
	if got := mustFind(t, dir); got.Target.Project != "LINKED" {
		t.Errorf("got %+v", got.Target)
	}
}

func TestFindPinDanglingSymlinkIsAnError(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, PinFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, found, err := FindPin(dir)
	if err == nil || found {
		t.Fatalf("FindPin = found %v, err %v; want an error", found, err)
	}
}

func TestFindPinMalformedIsAPinError(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	pinAt(t, dir, "TRELLIS\n")
	_, _, err := FindPin(dir)
	pe, ok := errors.AsType[*PinError](err)
	if !ok {
		t.Fatalf("error = %v, want *PinError", err)
	}
	if !strings.Contains(pe.Error(), "old bare-key format") || !strings.HasSuffix(pe.Path, PinFile) {
		t.Errorf("PinError = %q (path %s)", pe.Error(), pe.Path)
	}
}

// An unreadable pin must never let the walk continue to an ancestor's pin.
func TestFindPinUnreadablePinIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny reads here")
	}
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/PARENT\n")
	child := mkdir(t, parent, "child")
	pinAt(t, child, "/CHILD\n")
	if err := os.Chmod(filepath.Join(child, PinFile), 0o000); err != nil {
		t.Fatal(err)
	}
	_, found, err := FindPin(child)
	if err == nil || found || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("FindPin = found %v, err %v; want a permission error", found, err)
	}
}

func TestFindPinUnsearchableDirectoryIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny access here")
	}
	isolateHome(t)
	parent := t.TempDir()
	pinAt(t, parent, "/PARENT\n")
	locked := mkdir(t, parent, "locked")
	inner := mkdir(t, locked, "inner")
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if _, found, err := FindPin(inner); err == nil || found {
		t.Fatalf("FindPin = found %v, err %v; want an error", found, err)
	}
}

func TestFindPinWithoutAHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	dir := t.TempDir()
	pinAt(t, dir, "/NOHOME\n")
	if got := mustFind(t, dir); got.Target.Project != "NOHOME" {
		t.Errorf("got %+v", got.Target)
	}
}

func TestUnpinnable(t *testing.T) {
	h := isolateHome(t)
	if reason, err := Unpinnable(h); err != nil || reason == "" {
		t.Errorf("Unpinnable(home) = %q, %v; want a reason", reason, err)
	}
	root := filepath.VolumeName(h) + string(filepath.Separator)
	if reason, err := Unpinnable(root); err != nil || reason == "" {
		t.Errorf("Unpinnable(%s) = %q, %v; want a reason", root, reason, err)
	}
	if reason, err := Unpinnable(mkdir(t, h, "project")); err != nil || reason != "" {
		t.Errorf("Unpinnable(project) = %q, %v; want none", reason, err)
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

func TestPinsUnderSkipsGitAndNestedRepositories(t *testing.T) {
	isolateHome(t)
	repo := normalizeDir(t.TempDir())
	mkdir(t, repo, ".git")
	pinAt(t, repo, "/MONO\n")
	pinAt(t, mkdir(t, repo, "api"), "/API\n")
	pinAt(t, mkdir(t, repo, ".git", "hooks"), "/HIDDEN\n")
	nested := mkdir(t, repo, "vendor", "lib")
	mkdir(t, nested, ".git")
	pinAt(t, nested, "/LIB\n")
	pinAt(t, mkdir(t, repo, "old"), "OLD\n")

	pins, skipped, err := PinsUnder(repo)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range pins {
		keys = append(keys, p.Target.Project)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"API", "MONO"}) {
		t.Errorf("pins = %v", keys)
	}
	if !slices.Equal(skipped, []string{filepath.Join(repo, "old", PinFile)}) {
		t.Errorf("skipped = %v", skipped)
	}
}
