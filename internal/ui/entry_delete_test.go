package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestDeleteEntry(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	p, err := c.CreateProject(context.Background(), "KDEL", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(context.Background(), p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create an entry
	createResp := request(http.MethodPost, "/api/p/KDEL/b/board1/vault", `{"title":"Test Entry","body":"content"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create entry status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var entry core.Entry
	if err := json.Unmarshal(createResp.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	// Test deleting the entry
	deleteResp := request(http.MethodDelete, "/api/p/KDEL/b/board1/vault/"+entry.Slug, "")
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete entry status = %d, expected 204, body = %s", deleteResp.Code, deleteResp.Body)
	}

	// Verify it's deleted by trying to get it
	getResp := request(http.MethodGet, "/api/p/KDEL/b/board1/vault", "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("list entry status = %d", getResp.Code)
	}

	var entries []core.Entry
	if err := json.Unmarshal(getResp.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after deletion, got %d", len(entries))
	}

	// Test deleting non-existent entry returns 404
	deleteNotFoundResp := request(http.MethodDelete, "/api/p/KDEL/b/board1/vault/nonexistent", "")
	if deleteNotFoundResp.Code != http.StatusNotFound {
		t.Fatalf("delete nonexistent entry status = %d, expected 404", deleteNotFoundResp.Code)
	}
}
