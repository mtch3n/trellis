package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// An entry in a directory has a slug with a slash in it. The web client
// sends that slash as %2F so the slug stays one path segment, and every
// knowledge route has to accept it.
func TestSlugWithSlashEncoded(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	p, err := c.CreateProject(ctx, "SLUG", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "board1", true); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}
	// decode fails on the SPA's HTML, so a route that does not exist
	// cannot pass as a 200 from the frontend fallback.
	decode := func(name string, rec *httptest.ResponseRecorder, want int, v any) {
		t.Helper()
		if rec.Code != want {
			t.Fatalf("%s status = %d, want %d, body = %s", name, rec.Code, want, rec.Body)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatalf("%s did not return JSON: %v, body = %.200s", name, err, rec.Body)
		}
	}

	doc, err := s.write.CreateKnowledge(ctx, p.ID, core.NewKnowledge{
		Title: "Rollback Runbook",
		Body:  "How to roll back.",
		Board: "board1",
		Dir:   "ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Slug, "/") {
		t.Fatalf("slug %q has no slash; the test needs one", doc.Slug)
	}
	enc := url.PathEscape(doc.Slug)
	if !strings.Contains(enc, "%2F") {
		t.Fatalf("escaped slug %q keeps a raw slash", enc)
	}
	project := "/api/p/SLUG/knowledge/" + enc
	board := "/api/p/SLUG/b/board1/knowledge/" + enc

	var edited core.Knowledge
	decode("patch", request(http.MethodPatch, board, `{"body":"How to roll back safely.","version":1}`), http.StatusOK, &edited)
	if edited.Slug != doc.Slug || edited.Version != 2 {
		t.Fatalf("patched entry = %s v%d, want %s v2", edited.Slug, edited.Version, doc.Slug)
	}

	var got core.Knowledge
	decode("get", request(http.MethodGet, project, ""), http.StatusOK, &got)
	if got.Slug != doc.Slug {
		t.Fatalf("get returned %q, want %q", got.Slug, doc.Slug)
	}

	var revs []json.RawMessage
	decode("history", request(http.MethodGet, project+"/history", ""), http.StatusOK, &revs)
	if len(revs) == 0 {
		t.Fatal("history is empty after an edit")
	}

	var diff map[string]any
	decode("diff", request(http.MethodGet, project+"/diff", ""), http.StatusOK, &diff)

	if rec := request(http.MethodDelete, board, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodGet, project, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, body = %s", rec.Code, rec.Body)
	}
}
