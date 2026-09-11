package resolve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestRepoFindsTopLevelFromSubdirectory(t *testing.T) {
	root := gitInit(t)
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	info, ok := Repo(sub)
	if !ok {
		t.Fatal("Repo() returned ok=false inside a repository")
	}
	// macOS resolves /var to /private/var, so compare resolved paths.
	want, _ := filepath.EvalSymlinks(root)
	got, _ := filepath.EvalSymlinks(info.TopLevel)
	if got != want {
		t.Errorf("TopLevel = %q, want %q", got, want)
	}
}

// The reason --path-format=absolute is mandatory: without it --git-common-dir
// returns a relative ".git" from inside the main repository.
func TestRepoCommonDirIsAbsolute(t *testing.T) {
	root := gitInit(t)
	info, ok := Repo(root)
	if !ok {
		t.Fatal("Repo() returned ok=false")
	}
	if !filepath.IsAbs(info.CommonDir) {
		t.Errorf("CommonDir = %q, want an absolute path", info.CommonDir)
	}
}

// A worktree must report the MAIN repository's common dir, so both resolve to
// the same project.
func TestWorktreeSharesCommonDir(t *testing.T) {
	root := gitInit(t)
	wt := filepath.Join(t.TempDir(), "wt")
	cmd := exec.Command("git", "worktree", "add", "-q", "--detach", wt)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}

	main, _ := Repo(root)
	side, ok := Repo(wt)
	if !ok {
		t.Fatal("Repo() returned ok=false in a worktree")
	}
	a, _ := filepath.EvalSymlinks(main.CommonDir)
	b, _ := filepath.EvalSymlinks(side.CommonDir)
	if a != b {
		t.Errorf("worktree CommonDir = %q, main = %q; they must match", b, a)
	}
}

func TestRepoOutsideRepository(t *testing.T) {
	if _, ok := Repo(t.TempDir()); ok {
		t.Error("Repo() returned ok=true outside a repository")
	}
}

// GIT_DIR and friends make git answer about a different repository while
// exiting 0. Without scrubbing them, a non-repository directory reports that
// other repository's paths and Repo() returns a confident wrong answer -- the
// silent misclassification this function exists to prevent.
func TestRepoIgnoresInheritedGitEnv(t *testing.T) {
	elsewhere := gitInit(t)
	notARepo := t.TempDir()

	t.Setenv("GIT_DIR", filepath.Join(elsewhere, ".git"))
	t.Setenv("GIT_WORK_TREE", elsewhere)

	if info, ok := Repo(notARepo); ok {
		t.Errorf("Repo(%q) = %+v, ok=true; inherited GIT_DIR leaked another repository",
			notARepo, info)
	}
}
