package ui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestActivity_PPL_C4_a_key_that_names_no_project_answers_404(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 3_000_000}, "human:ui-activity-test", dir)
	if _, err := c.CreateProject(t.Context(), "SEEN", false); err != nil {
		t.Fatal(err)
	}
	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/api/p/SEEN/activity"); rec.Code != http.StatusOK {
		t.Fatalf("GET a known project's activity = %d %s", rec.Code, rec.Body)
	}
	if rec := get("/api/p/NOSUCH/activity"); rec.Code != http.StatusNotFound {
		t.Errorf("GET an unknown project's activity = %d %s, want 404", rec.Code, rec.Body)
	}
}
