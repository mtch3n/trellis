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

func TestCreateNote(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	p, err := c.CreateProject(context.Background(), "TEST", false)
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

	// Create a card
	createResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards", `{"title":"Test Card"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create card status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var card core.Card
	if err := json.Unmarshal(createResp.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}

	cardRef := card.Ref

	// Test creating a note
	noteResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/notes", `{"body":"This is a test note"}`)
	if noteResp.Code != http.StatusCreated {
		t.Fatalf("create note status = %d, body = %s", noteResp.Code, noteResp.Body)
	}

	var note core.Note
	if err := json.Unmarshal(noteResp.Body.Bytes(), &note); err != nil {
		t.Fatal(err)
	}

	if note.BodyMD != "This is a test note" {
		t.Fatalf("expected note body 'This is a test note', got %q", note.BodyMD)
	}

	// Test creating a note with empty body returns 400
	emptyResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/notes", `{"body":""}`)
	if emptyResp.Code != http.StatusBadRequest {
		t.Fatalf("empty note status = %d, expected 400, body = %s", emptyResp.Code, emptyResp.Body)
	}

	// Test creating a note for non-existent card returns 404
	notFoundResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/TEST-999/notes", `{"body":"test"}`)
	if notFoundResp.Code != http.StatusNotFound {
		t.Fatalf("note for nonexistent card status = %d, expected 404", notFoundResp.Code)
	}
}
