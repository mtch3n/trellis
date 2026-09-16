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

func TestDeleteKnowledge(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	p, err := c.CreateProject(context.Background(), "KDEL", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(context.Background(), p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0")
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create a knowledge entry
	createResp := request(http.MethodPost, "/api/p/KDEL/b/board1/knowledge", `{"title":"Test Doc","body":"content"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create knowledge status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var doc core.Knowledge
	if err := json.Unmarshal(createResp.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}

	// Test deleting the knowledge entry
	deleteResp := request(http.MethodDelete, "/api/p/KDEL/b/board1/knowledge/"+doc.Slug, "")
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete knowledge status = %d, expected 204, body = %s", deleteResp.Code, deleteResp.Body)
	}

	// Verify it's deleted by trying to get it
	getResp := request(http.MethodGet, "/api/p/KDEL/b/board1/knowledge", "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("list knowledge status = %d", getResp.Code)
	}

	var docs []core.Knowledge
	if err := json.Unmarshal(getResp.Body.Bytes(), &docs); err != nil {
		t.Fatal(err)
	}

	if len(docs) != 0 {
		t.Fatalf("expected 0 knowledge entries after deletion, got %d", len(docs))
	}

	// Test deleting non-existent knowledge returns 404
	deleteNotFoundResp := request(http.MethodDelete, "/api/p/KDEL/b/board1/knowledge/nonexistent", "")
	if deleteNotFoundResp.Code != http.StatusNotFound {
		t.Fatalf("delete nonexistent knowledge status = %d, expected 404", deleteNotFoundResp.Code)
	}
}
