package home

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRootPrefersTrellisHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRELLIS_HOME", dir)

	got, err := Root()
	if err != nil {
		t.Fatalf("Root() error: %v", err)
	}
	if got != dir {
		t.Errorf("Root() = %q, want %q", got, dir)
	}
}

func TestRootDefaultsUnderHome(t *testing.T) {
	fake := t.TempDir()
	t.Setenv("TRELLIS_HOME", "")

	// Each platform has its own idea of where per-user state belongs: a dotted
	// directory under the profile on Unix, LOCALAPPDATA on Windows.
	want := filepath.Join(fake, ".trellis")
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", fake)
		want = filepath.Join(fake, "trellis")
	} else {
		t.Setenv("HOME", fake)
	}

	got, err := Root()
	if err != nil {
		t.Fatalf("Root() error: %v", err)
	}
	if got != want {
		t.Errorf("Root() = %q, want %q", got, want)
	}
}

func TestRootCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "trellis")
	t.Setenv("TRELLIS_HOME", dir)

	got, err := Root()
	if err != nil {
		t.Fatalf("Root() error: %v", err)
	}
	// Assert the directory really exists: without this the test passes even
	// if Root() never calls MkdirAll.
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Root() returned %q but it does not exist: %v", got, err)
	}
	if !info.IsDir() {
		t.Fatalf("Root() returned %q, which is not a directory", got)
	}

	dbPath, err := DBPath()
	if err != nil {
		t.Fatalf("DBPath() error: %v", err)
	}
	if filepath.Dir(dbPath) != got {
		t.Errorf("DBPath() = %q, want it inside %q", dbPath, got)
	}
}

// A test that does not set TRELLIS_HOME must not reach the user's real home.
func TestRootRefusesTheRealHomeUnderTest(t *testing.T) {
	t.Setenv("TRELLIS_HOME", "")
	outside := filepath.Join(string(filepath.Separator), "not-a-temp-dir-"+t.Name())
	t.Setenv("HOME", outside)
	t.Setenv("LOCALAPPDATA", outside)
	if got, err := Root(); err == nil {
		t.Fatalf("Root() = %q, want a refusal", got)
	}
}
