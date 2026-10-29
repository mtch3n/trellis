package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func clientInitDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("TRELLIS_PROJECT", "")
	return dir
}

func clientMarker(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".trellis"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitWritesTheMarkerFromTheDirectoryName(t *testing.T) {
	dir := clientInitDir(t, "my app")
	runCmd(t, "init", "--json")
	if got := clientMarker(t, dir); got != "/MY-APP\n" {
		t.Fatalf("marker = %q", got)
	}
}

func TestInitWritesTheMarkerForANamedKeyAndKeepsTheSameOne(t *testing.T) {
	dir := clientInitDir(t, "checkout")
	runCmd(t, "init", "--key", "alpha")
	runCmd(t, "init", "--key", "ALPHA")
	if got := clientMarker(t, dir); got != "/ALPHA\n" {
		t.Fatalf("marker = %q", got)
	}
}

func TestInitRefusesAMarkerThatNamesAnotherProject(t *testing.T) {
	dir := clientInitDir(t, "checkout")
	runCmd(t, "init", "--key", "ALPHA")
	_, err := runCmdErr(t, "init", "--key", "BETA")
	if cliErrCode(err) != "marker_exists" {
		t.Fatalf("err = %v, want marker_exists", err)
	}
	if got := clientMarker(t, dir); got != "/ALPHA\n" {
		t.Fatalf("marker = %q", got)
	}
}
