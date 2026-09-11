package resolve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo makes dir a minimal git repository so Repo(dir) succeeds.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func TestIdentifyEnvBeatsPinAndGit(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".trellis"), []byte("PINNED"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRELLIS_PROJECT", "envwins")

	id, err := Identify(dir)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Kind != "env" || id.Value != "envwins" {
		t.Errorf("identity = %+v, want kind=env value=envwins", id)
	}
}

func TestIdentifyPinAtRepoRootBeatsRemote(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".trellis"), []byte("PINNED"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Give the repo a remote too, so a pass here proves the pin really beats
	// the remote rather than merely being the only identity available.
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin",
		"git@github.com:mtch3n/example.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	id, err := Identify(dir)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Kind != "pin" || id.Value != "PINNED" {
		t.Errorf("identity = %+v, want kind=pin value=PINNED", id)
	}
}

// TestIdentifyIgnoresPinAboveRepoRoot is Finding 2's regression test: a stray
// .trellis in a directory that happens to contain several repositories (like
// ~/Work) must not silently merge every repository beneath it onto one board.
func TestIdentifyIgnoresPinAboveRepoRoot(t *testing.T) {
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, ".trellis"), []byte("STRAY"), 0o644); err != nil {
		t.Fatal(err)
	}

	repoDir := filepath.Join(parent, "myrepo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repoDir)
	if out, err := exec.Command("git", "-C", repoDir, "remote", "add", "origin",
		"git@github.com:mtch3n/myrepo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	id, err := Identify(repoDir)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Kind == "pin" {
		t.Fatalf("identity = %+v, a pin ABOVE the repository root must not be found", id)
	}
	if id.Kind != "remote" || id.Value != "github.com/mtch3n/myrepo" {
		t.Errorf("identity = %+v, want kind=remote value=github.com/mtch3n/myrepo", id)
	}
}

func TestIdentifyEmptyPinIsHardError(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".trellis"), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Identify(dir)
	if err == nil {
		t.Fatal("expected an error for an empty .trellis, got nil")
	}
}

func TestIdentifyNoRepoNoPinErrors(t *testing.T) {
	dir := t.TempDir()

	_, err := Identify(dir)
	if err == nil {
		t.Fatal("expected an error outside a repository with no pin, got nil")
	}
}

func TestSanitizeKeyPrefixesLeadingDigit(t *testing.T) {
	got := sanitizeKey("2024-migrations")
	if got[0] < 'A' || got[0] > 'Z' {
		t.Errorf("sanitizeKey(%q) = %q, want a leading letter", "2024-migrations", got)
	}
}
