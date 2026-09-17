package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// markerEnv isolates a test from the developer's Trellis state: a private
// storage root and home, no project or board override, and a fresh working
// directory called name, which it returns.
func markerEnv(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("TRELLIS_HOME", t.TempDir())
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("TRELLIS_PROJECT", "")
	t.Setenv("TRELLIS_BOARD", "")
	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

// seedProject creates key, plus any extra boards, the way `project new` does.
// The handle is closed before any command opens the same file: an open handle
// would hold the database, and on Windows would block TempDir cleanup.
func seedProject(t *testing.T, key string, boards ...string) core.Project {
	t.Helper()
	root, err := home.Root()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.RealClock{}, "test", root)
	p, err := c.CreateProject(t.Context(), key, false)
	if err != nil {
		t.Fatalf("CreateProject(%s): %v", key, err)
	}
	for _, name := range boards {
		if _, err := c.CreateBoard(t.Context(), p.ID, name, true); err != nil {
			t.Fatalf("CreateBoard(%s): %v", name, err)
		}
	}
	return p
}

func writeMarker(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".trellis"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func coreErr(t *testing.T, err error) *core.Error {
	t.Helper()
	ce, ok := errors.AsType[*core.Error](err)
	if !ok {
		t.Fatalf("error = %v, want a *core.Error", err)
	}
	return ce
}
