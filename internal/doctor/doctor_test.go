package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/mtch3n/trellis/internal/testhome"
)

// TestMain keeps these hermetic: every check reads TRELLIS_HOME, and one that
// fell through to the real one would describe the machine's own installation.
func TestMain(m *testing.M) { testhome.Main(m) }

// write is the fixture helper the CLI's own doctor tests use: a file with
// exactly these bytes, parents included.
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckStorageRoot(t *testing.T) {
	root := t.TempDir()
	if got := StorageRoot(root); got.Status != StatusOK {
		t.Errorf("a fresh temp dir should pass, got %+v", got)
	}
	if got := StorageRoot(filepath.Join(root, "absent")); got.Status != StatusFail {
		t.Errorf("a missing root should fail, got %+v", got)
	}

	file := filepath.Join(root, "afile")
	write(t, file, "")
	got := StorageRoot(file)
	if got.Status != StatusFail || !strings.Contains(got.Detail, "not a directory") {
		t.Errorf("a file where the root should be must fail, got %+v", got)
	}
}

func TestCheckDatabaseWarnsBeforeInit(t *testing.T) {
	t.Setenv("TRELLIS_HOME", t.TempDir())
	got := Database()
	if got.Status != StatusWarn || got.Fix != "trellis init" {
		t.Errorf("an uninitialized root should warn and point at init, got %+v", got)
	}
}

func TestCheckVectorSearch(t *testing.T) {
	cfg := config.Defaults()
	if got := VectorSearch(cfg); got.Status != StatusOK {
		t.Errorf("vector search off by default should pass, got %+v", got)
	}

	cfg.Search.Method = "hybrid"
	if got := VectorSearch(cfg); got.Status != StatusWarn {
		t.Errorf("hybrid search with vectors disabled should warn, got %+v", got)
	}

	cfg.Search.Vector.Enabled = true
	cfg.Search.Vector.Provider = "command"
	if got := VectorSearch(cfg); got.Status != StatusFail {
		t.Errorf("an empty embed command should fail, got %+v", got)
	}
	cfg.Search.Vector.EmbedCommand = filepath.Join(t.TempDir(), "no-such-embedder")
	if got := VectorSearch(cfg); got.Status != StatusFail {
		t.Errorf("an unresolvable embed command should fail, got %+v", got)
	}

	cfg.Search.Vector.Provider = "http"
	if got := VectorSearch(cfg); got.Status != StatusFail {
		t.Errorf("an http provider with no endpoint should fail, got %+v", got)
	}
	cfg.Search.Vector.Endpoint = "http://localhost:1234/embed"
	if got := VectorSearch(cfg); got.Status != StatusOK {
		t.Errorf("a configured http provider should pass, got %+v", got)
	}
}

func TestCheckProjectKeysFlagsKeysAMarkerCannotName(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	db, err := store.Open(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	// A key from before the grammar: reachable with --project, but no marker
	// can name it. One that follows the grammar must not be reported.
	_, err = db.Exec(`INSERT INTO project (id, key, name, created_at) VALUES
		('good', 'GOOD', 'GOOD', 1), ('x', 'MY_APP', 'MY_APP', 1)`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	got := ProjectKeys()
	if got.Status != StatusWarn || !strings.Contains(got.Detail, "MY_APP") || strings.Contains(got.Detail, "GOOD") {
		t.Errorf("check = %+v", got)
	}
}

func TestCheckProjectKeysWithoutADatabase(t *testing.T) {
	t.Setenv("TRELLIS_HOME", t.TempDir())
	if got := ProjectKeys(); got.Status != StatusOK {
		t.Errorf("check = %+v", got)
	}
}
