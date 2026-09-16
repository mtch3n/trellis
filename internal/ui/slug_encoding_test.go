package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestSlugWithSlashEncoded(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	projKey := fmt.Sprintf("SLUG%d", rand.Intn(100000))
	p, err := c.CreateProject(context.Background(), projKey, false)
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

	// Create knowledge entries
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge", `{"title":"Rollback Runbook","body":"How to rollback"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create knowledge status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var doc core.Knowledge
	if err := json.Unmarshal(createResp.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	doc2, err := s.write.CreateKnowledge(ctx, p.ID, core.NewKnowledge{
		Title:    "Deployment/Steps",
		Body:     "How to deploy",
		Summary:  "",
		Template: "",
		Board:    "board1",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	// Test both documents, especially doc2 which has a slash in the title
	// that will be converted to a slash in the slug
	slug := doc.Slug
	encodedSlug := url.PathEscape(slug)

	// Test GET history with encoded slug
	historyResp := request(http.MethodGet, "/api/p/"+projKey+"/b/board1/knowledge/"+encodedSlug+"/history", "")
	if historyResp.Code != http.StatusOK {
		t.Fatalf("get history status = %d, expected 200, body = %s", historyResp.Code, historyResp.Body)
	}

	// Test GET diff with encoded slug
	diffResp := request(http.MethodGet, "/api/p/"+projKey+"/b/board1/knowledge/"+encodedSlug+"/diff", "")
	if diffResp.Code != http.StatusOK {
		t.Fatalf("get diff status = %d, expected 200, body = %s", diffResp.Code, diffResp.Body)
	}

	// Test PATCH edit with encoded slug
	patchResp := request(http.MethodPatch, "/api/p/"+projKey+"/b/board1/knowledge/"+encodedSlug, `{"title":"Updated Title","version":1}`)
	if patchResp.Code != http.StatusOK {
		t.Fatalf("patch knowledge status = %d, expected 200, body = %s", patchResp.Code, patchResp.Body)
	}

	var updatedDoc core.Knowledge
	if err := json.Unmarshal(patchResp.Body.Bytes(), &updatedDoc); err != nil {
		t.Fatal(err)
	}
	if updatedDoc.Title != "Updated Title" {
		t.Fatalf("expected updated title 'Updated Title', got %q", updatedDoc.Title)
	}

	// Test DELETE with encoded slug
	deleteResp := request(http.MethodDelete, "/api/p/"+projKey+"/b/board1/knowledge/"+encodedSlug, "")
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete knowledge status = %d, expected 204, body = %s", deleteResp.Code, deleteResp.Body)
	}

	// Now test with doc2's slug to see if encoding works with more complex slugs
	// First update it
	slug2 := doc2.Slug
	encodedSlug2 := url.PathEscape(slug2)

	patchResp2 := request(http.MethodPatch, "/api/p/"+projKey+"/b/board1/knowledge/"+encodedSlug2, `{"body":"Updated body","version":1}`)
	if patchResp2.Code != http.StatusOK {
		t.Fatalf("patch knowledge 2 status = %d, expected 200, body = %s", patchResp2.Code, patchResp2.Body)
	}

	// Test GET history for the second document
	historyResp2 := request(http.MethodGet, "/api/p/"+projKey+"/b/board1/knowledge/"+encodedSlug2+"/history", "")
	if historyResp2.Code != http.StatusOK {
		t.Fatalf("get history 2 status = %d, expected 200, body = %s", historyResp2.Code, historyResp2.Body)
	}

	// Test GET diff for the second document
	diffResp2 := request(http.MethodGet, "/api/p/"+projKey+"/b/board1/knowledge/"+encodedSlug2+"/diff", "")
	if diffResp2.Code != http.StatusOK {
		t.Fatalf("get diff 2 status = %d, expected 200, body = %s", diffResp2.Code, diffResp2.Body)
	}
}
