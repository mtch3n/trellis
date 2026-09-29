package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestTrash_TRASH_C2_ui_delete_routes_trash_and_restore_brings_back(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 3_000_000}, "ui-trash-test", dir)
	p, err := c.CreateProject(ctx, "TRASHUI", false)
	if err != nil {
		t.Fatal(err)
	}
	c.CreateBoard(ctx, p.ID, "default", true)
	c.CreateBoard(ctx, p.ID, "side", true)
	entry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{Title: "Notes", Body: "keep\n"})
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(src, []byte("a"), 0o600)
	art, err := c.CreateArtifact(ctx, p.ID, src)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := c.CreateProject(ctx, "GONEUI", false)
	c.CreateBoard(ctx, other.ID, "default", true)

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	del := func(path, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		if rec.Code >= 300 {
			t.Fatalf("DELETE %s = %d %s", path, rec.Code, rec.Body)
		}
	}
	del("/api/p/TRASHUI/b/default/vault/"+entry.Slug, "")
	del("/api/p/TRASHUI/artifacts/"+art.Name, "")
	del("/api/p/TRASHUI/b/side", "")
	del("/api/p/GONEUI", `{"confirm":"GONEUI"}`)

	if _, err := c.RestoreEntry(ctx, p.ID, entry.Slug); err != nil {
		t.Errorf("entry: %v", err)
	}
	if _, err := c.RestoreArtifact(ctx, p.ID, art.Name); err != nil {
		t.Errorf("artifact: %v", err)
	}
	if _, err := c.RestoreBoard(ctx, p.ID, "side"); err != nil {
		t.Errorf("board: %v", err)
	}
	if _, err := c.RestoreProject(ctx, "GONEUI"); err != nil {
		t.Errorf("project: %v", err)
	}
}
