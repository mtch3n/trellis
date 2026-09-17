package testhome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

func TestGoLocationsStayOutsideTheTemporaryHome(t *testing.T) {
	home := os.Getenv("HOME")
	for _, name := range goLocations {
		if v := os.Getenv(name); v != "" && strings.HasPrefix(v, home) {
			t.Errorf("%s = %s is inside the temporary home %s", name, v, home)
		}
	}
	if os.Getenv("GOMODCACHE") == "" {
		t.Error("GOMODCACHE was not pinned")
	}
}

func TestAReexecutedBinarySharesTheHome(t *testing.T) {
	home := os.Getenv("HOME")
	cleanup := Setup()
	cleanup()
	if got := os.Getenv("HOME"); got != home {
		t.Errorf("a nested Setup moved HOME from %s to %s", home, got)
	}
	if _, err := os.Stat(home); err != nil {
		t.Errorf("a nested cleanup removed the shared home: %v", err)
	}
}

func TestRemoveAllRemovesReadOnlyTrees(t *testing.T) {
	dir, err := os.MkdirTemp("", "testhome-readonly-")
	if err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dir, "pkg", "mod")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "go.mod"), []byte("module x\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{inner, filepath.Dir(inner)} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	removeAll(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%s survived: %v", dir, err)
	}
}
