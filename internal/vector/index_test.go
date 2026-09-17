package vector

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/mtch3n/trellis/internal/testhome"
	_ "modernc.org/sqlite"
)

// fakeEmbed is the compiled stand-in embedding provider, built once for the
// whole package because every test that indexes anything needs it.
var fakeEmbed string

// TestMain folds this package's own setup -- compiling fakeembed once -- into
// testhome's hermetic-home setup: see internal/testhome.
func TestMain(m *testing.M) {
	cleanupHome := testhome.Setup()
	dir, err := os.MkdirTemp("", "fakeembed")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fakeEmbed = filepath.Join(dir, "fakeembed")
	if runtime.GOOS == "windows" {
		fakeEmbed += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", fakeEmbed, "./testdata/fakeembed").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build fakeembed: %v\n%s", err, out)
		os.RemoveAll(dir)
		cleanupHome()
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	cleanupHome()
	os.Exit(code)
}

func TestIndexUpsertSearchAndPrune(t *testing.T) {
	dir := t.TempDir()
	cmd := fakeEmbed
	dbPath := filepath.Join(dir, "trellis.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	idx, err := New(db, dbPath, config.VectorSearchConfig{Enabled: true, EmbedCommand: cmd, Dimension: 2, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	if n, err := idx.Upsert(ctx, []Entry{{ID: "a", Title: "Concurrency", Content: "locks"}, {ID: "b", Title: "Other", Content: "unrelated"}}, "p"); err != nil || n != 2 {
		t.Fatalf("upsert n=%d err=%v", n, err)
	}
	hits, err := idx.Search(ctx, "p", "locking", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "a" {
		t.Fatalf("unexpected hits: %#v", hits)
	}
	if n, err := idx.Prune(ctx, []string{"a"}, "p"); err != nil || n != 1 {
		t.Fatalf("prune n=%d err=%v", n, err)
	}
	count, err := idx.Count(ctx, "p")
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if result, err := idx.Reindex(ctx, "p"); err != nil || result == "" {
		t.Fatalf("reindex result=%q err=%v", result, err)
	}
}

func TestNewWithEagerStoreConnection(t *testing.T) {
	dir := t.TempDir()
	cmd := fakeEmbed
	dbPath := filepath.Join(dir, "trellis.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx, err := New(db.DB, filepath.Join(dir, "vectors.db"), config.VectorSearchConfig{Enabled: true, EmbedCommand: cmd, Dimension: 2, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if _, err := idx.Upsert(t.Context(), []Entry{{ID: "eager", Content: "works"}}, "p"); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertSkipsUnchangedAndPreservesOnEmbeddingFailure(t *testing.T) {
	dir := t.TempDir()
	cmd := fakeEmbed
	count := filepath.Join(dir, "count")
	failed := filepath.Join(dir, "failed")
	t.Setenv("FAKEEMBED_COUNT_FILE", count)
	t.Setenv("FAKEEMBED_FAIL_IF_EXISTS", failed)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	idx, err := New(db, filepath.Join(dir, "vectors.db"), config.VectorSearchConfig{Enabled: true, EmbedCommand: cmd, Dimension: 2, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	entry := Entry{ID: "stable", Title: "Stable", Content: "same"}
	if n, err := idx.Upsert(t.Context(), []Entry{entry}, "p"); err != nil || n != 1 {
		t.Fatalf("first upsert n=%d err=%v", n, err)
	}
	before, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := idx.Upsert(t.Context(), []Entry{entry}, "p"); err != nil || n != 0 {
		t.Fatalf("unchanged upsert n=%d err=%v", n, err)
	}
	after, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("unchanged entry was embedded again: before %q after %q", before, after)
	}
	if err := os.WriteFile(failed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Upsert(t.Context(), []Entry{{ID: entry.ID, Title: entry.Title, Content: "changed"}}, "p"); err == nil {
		t.Fatal("expected embedding failure")
	}
	if got, err := idx.Count(t.Context(), "p"); err != nil || got != 1 {
		t.Fatalf("failed replacement removed old index: count=%d err=%v", got, err)
	}
}

func TestIndexesUseSeparateVirtualTablesAndDatabases(t *testing.T) {
	dir := t.TempDir()
	cmd := fakeEmbed
	primary, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "primary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	primary.SetMaxOpenConns(1)
	cfg := config.VectorSearchConfig{Enabled: true, EmbedCommand: cmd, Dimension: 2, Limit: 5}
	pathA := filepath.Join(dir, "a", "vectors.db")
	pathB := filepath.Join(dir, "b", "vectors.db")
	if err := os.MkdirAll(filepath.Dir(pathA), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pathB), 0o700); err != nil {
		t.Fatal(err)
	}
	a, err := New(primary, pathA, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := New(primary, pathB, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.virtualTable == b.virtualTable || a.shadowTable == b.shadowTable {
		t.Fatalf("project vector tables collided: %q and %q", a.virtualTable, b.virtualTable)
	}
	if _, err := a.Upsert(t.Context(), []Entry{{ID: "a-entry", Content: "alpha"}}, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Upsert(t.Context(), []Entry{{ID: "b-entry", Content: "bravo"}}, "b"); err != nil {
		t.Fatal(err)
	}
	if got, err := a.Count(t.Context(), "a"); err != nil || got != 1 {
		t.Fatalf("a count = %d, err = %v", got, err)
	}
	if got, err := b.Count(t.Context(), "b"); err != nil || got != 1 {
		t.Fatalf("b count = %d, err = %v", got, err)
	}
}

// A deleted project's virtual tables are dropped from the shared database, and
// a later New recreates them.
func TestDropTablesRemovesAProjectsVirtualTables(t *testing.T) {
	dir := t.TempDir()
	primary, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "primary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	primary.SetMaxOpenConns(1)
	cfg := config.VectorSearchConfig{Enabled: true, EmbedCommand: fakeEmbed, Dimension: 2, Limit: 5}
	path := filepath.Join(dir, "p", "vectors.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	idx, err := New(primary, path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	virtual, shadow := idx.virtualTable, idx.shadowTable
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	tableCount := func() int {
		t.Helper()
		var n int
		if err := primary.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN (?, ?)`, virtual, shadow).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if tableCount() == 0 {
		t.Fatalf("New created neither %s nor %s", virtual, shadow)
	}
	if err := DropTables(t.Context(), primary, path); err != nil {
		t.Fatal(err)
	}
	if n := tableCount(); n != 0 {
		t.Fatalf("%d of the project's tables remain after DropTables", n)
	}
	// Dropping twice is harmless, and New brings the tables back.
	if err := DropTables(t.Context(), primary, path); err != nil {
		t.Fatal(err)
	}
	again, err := New(primary, path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if tableCount() == 0 {
		t.Fatal("New did not recreate the tables")
	}
}
