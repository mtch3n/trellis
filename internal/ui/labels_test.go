package ui

import (
	"bytes"
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

func TestCreateAndDeleteLabel(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	_, err = c.CreateProject(context.Background(), "TEST", false)
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

	// Test creating a label
	createResp := request(http.MethodPost, "/api/p/TEST/labels", `{"name":"bug","description":"A bug label"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create label status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var createdLabel core.Label
	if err := json.Unmarshal(createResp.Body.Bytes(), &createdLabel); err != nil {
		t.Fatal(err)
	}

	if createdLabel.Name != "bug" {
		t.Fatalf("expected label name 'bug', got %q", createdLabel.Name)
	}
	if createdLabel.Description != "A bug label" {
		t.Fatalf("expected description 'A bug label', got %q", createdLabel.Description)
	}

	// Verify label exists by listing labels
	listResp := request(http.MethodGet, "/api/p/TEST/labels", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("list labels status = %d", listResp.Code)
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte("bug")) {
		t.Fatalf("expected 'bug' in labels list, body = %s", listResp.Body)
	}

	// Test deleting a label
	deleteResp := request(http.MethodDelete, "/api/p/TEST/labels/bug", "")
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete label status = %d, expected 204, body = %s", deleteResp.Code, deleteResp.Body)
	}

	// Verify label is deleted
	listAfterResp := request(http.MethodGet, "/api/p/TEST/labels", "")
	if listAfterResp.Code != http.StatusOK {
		t.Fatalf("list labels after delete status = %d", listAfterResp.Code)
	}
	if bytes.Contains(listAfterResp.Body.Bytes(), []byte("bug")) {
		t.Fatalf("expected 'bug' to be deleted, body = %s", listAfterResp.Body)
	}

	// Test deleting non-existent label returns 404
	deleteNotFoundResp := request(http.MethodDelete, "/api/p/TEST/labels/nonexistent", "")
	if deleteNotFoundResp.Code != http.StatusNotFound {
		t.Fatalf("delete nonexistent label status = %d, expected 404", deleteNotFoundResp.Code)
	}
}
