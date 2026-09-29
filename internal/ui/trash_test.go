package ui

import (
	"context"
	"encoding/json/v2"
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

func TestTrash_TRASH_C33_the_web_api_lists_the_trash_and_restores_from_it(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 3_000_000}, "ui-trash-test", dir)
	p, _ := c.CreateProject(ctx, "ROUTES", false)
	b, _ := c.CreateBoard(ctx, p.ID, "default", true)
	card, _ := c.CreateCard(ctx, p.ID, b.ID, core.NewCard{Title: "gone"})
	if err := c.DeleteCard(ctx, p.ID, core.CardRef{UUID: card.ID}); err != nil {
		t.Fatal(err)
	}
	other, _ := c.CreateProject(ctx, "ELSEWHERE", false)
	c.CreateBoard(ctx, other.ID, "default", true)
	if err := c.DeleteProject(ctx, "ELSEWHERE"); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	call := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	var items []trashItem
	if err := json.Unmarshal(call(http.MethodGet, "/api/p/ROUTES/trash").Body.Bytes(), &items); err != nil || len(items) != 1 {
		t.Fatalf("trash = %v, %v", items, err)
	}
	if items[0].Kind != "card" || items[0].PurgeAt != items[0].TrashedAt+30*24*60*60*1000 {
		t.Errorf("item = %+v", items[0])
	}
	if rec := call(http.MethodPost, "/api/p/ROUTES/trash/"+items[0].ID+"/restore"); rec.Code != http.StatusNoContent {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodGet, "/api/p/ROUTES/trash"); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("trash after restore = %s", rec.Body)
	}

	if rec := call(http.MethodGet, "/api/trash/projects"); !strings.Contains(rec.Body.String(), "ELSEWHERE") {
		t.Errorf("trashed projects = %s", rec.Body)
	}
	if rec := call(http.MethodPost, "/api/trash/projects/ELSEWHERE/restore"); rec.Code != http.StatusNoContent {
		t.Errorf("project restore = %d %s", rec.Code, rec.Body)
	}
}
