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

func TestCreateComment(t *testing.T) {
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

	// Test creating a comment
	commentResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/comments", `{"body":"This is a test comment"}`)
	if commentResp.Code != http.StatusCreated {
		t.Fatalf("create comment status = %d, body = %s", commentResp.Code, commentResp.Body)
	}

	var comment core.Comment
	if err := json.Unmarshal(commentResp.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}

	if comment.BodyMD != "This is a test comment" {
		t.Fatalf("expected comment body 'This is a test comment', got %q", comment.BodyMD)
	}

	// A card holds any number of comments; the detail lists them oldest first.
	if rec := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/comments", `{"body":"A second comment"}`); rec.Code != http.StatusCreated {
		t.Fatalf("second comment status = %d, body = %s", rec.Code, rec.Body)
	}
	detailResp := request(http.MethodGet, "/api/p/TEST/b/board1/cards/"+cardRef, "")
	var detail struct {
		Comments []core.Comment `json:"comments"`
		Notes    any            `json:"notes"`
	}
	if err := json.Unmarshal(detailResp.Body.Bytes(), &detail); err != nil {
		t.Fatalf("card detail: %v, body = %.200s", err, detailResp.Body)
	}
	if len(detail.Comments) != 2 || detail.Comments[0].BodyMD != "This is a test comment" || detail.Comments[1].BodyMD != "A second comment" {
		t.Fatalf("detail comments = %+v", detail.Comments)
	}
	if detail.Notes != nil {
		t.Errorf("card detail still carries notes: %v", detail.Notes)
	}

	// Test creating a comment with empty body returns 400
	emptyResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/comments", `{"body":""}`)
	if emptyResp.Code != http.StatusBadRequest {
		t.Fatalf("empty comment status = %d, expected 400, body = %s", emptyResp.Code, emptyResp.Body)
	}

	// Test creating a comment for non-existent card returns 404
	notFoundResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/TEST-999/comments", `{"body":"test"}`)
	if notFoundResp.Code != http.StatusNotFound {
		t.Fatalf("comment for nonexistent card status = %d, expected 404", notFoundResp.Code)
	}
}
