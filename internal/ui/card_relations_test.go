package ui

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func testServerWithCards(t *testing.T) *Server {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "test", dir)
	return NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
}

func TestHandleCardDetailIncludesRelations(t *testing.T) {
	s := testServerWithCards(t)

	// Create a project and board
	p, _ := s.core.CreateProject(context.Background(), "TEST", false)
	b, _ := s.core.CreateBoard(context.Background(), p.ID, "default", true)

	// Create two cards with a relation
	a, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "a"})
	d, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "d"})
	s.core.RelateCards(context.Background(), p.ID, core.CardRef{Seq: a.Seq}, "resolved_by", core.CardRef{Seq: d.Seq})

	// GET the card detail
	req := httptest.NewRequest("GET", "/api/p/"+p.Key+"/b/"+b.Slug+"/cards/"+a.Ref, nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp cardDetail
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Card.Ref != a.Ref {
		t.Errorf("card ref = %q, want %q", resp.Card.Ref, a.Ref)
	}
	if len(resp.Relations) != 1 {
		t.Errorf("relations count = %d, want 1", len(resp.Relations))
	}
	if resp.Relations[0].Rel != "resolved_by" {
		t.Errorf("rel = %q, want resolved_by", resp.Relations[0].Rel)
	}
	if resp.Relations[0].Ref != d.Ref {
		t.Errorf("relation ref = %q, want %q", resp.Relations[0].Ref, d.Ref)
	}
}

func TestHandleCardDetailProjectScopedIncludesRelations(t *testing.T) {
	s := testServerWithCards(t)

	// Create a project and board
	p, _ := s.core.CreateProject(context.Background(), "TEST", false)
	b, _ := s.core.CreateBoard(context.Background(), p.ID, "default", true)

	// Create two cards with a relation
	a, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "a"})
	d, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "d"})
	s.core.RelateCards(context.Background(), p.ID, core.CardRef{Seq: a.Seq}, "resolved_by", core.CardRef{Seq: d.Seq})

	// GET the card detail (project-scoped)
	req := httptest.NewRequest("GET", "/api/p/"+p.Key+"/cards/"+a.Ref, nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp cardDetail
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Relations) != 1 {
		t.Errorf("relations count = %d, want 1", len(resp.Relations))
	}
}

func TestHandleCardDetailNoRelations(t *testing.T) {
	s := testServerWithCards(t)

	p, _ := s.core.CreateProject(context.Background(), "TEST", false)
	b, _ := s.core.CreateBoard(context.Background(), p.ID, "default", true)
	a, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "a"})

	// GET the card detail
	req := httptest.NewRequest("GET", "/api/p/"+p.Key+"/b/"+b.Slug+"/cards/"+a.Ref, nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp cardDetail
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Relations field should be empty array or omitted
	if len(resp.Relations) != 0 {
		t.Errorf("expected empty relations, got %v", resp.Relations)
	}
}

func TestCreateCardRelationPOST(t *testing.T) {
	s := testServerWithCards(t)

	p, _ := s.core.CreateProject(context.Background(), "TEST", false)
	b, _ := s.core.CreateBoard(context.Background(), p.ID, "default", true)
	a, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "a"})
	d, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "d"})

	// POST a relation
	body := map[string]string{"rel": "resolved_by", "ref": d.Ref}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/p/"+p.Key+"/b/"+b.Slug+"/cards/"+a.Ref+"/relations",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp []core.CardRelation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp) != 1 {
		t.Errorf("relations count = %d, want 1", len(resp))
	}
	if resp[0].Rel != "resolved_by" {
		t.Errorf("rel = %q, want resolved_by", resp[0].Rel)
	}
}

func TestCreateCardRelationWithDashes(t *testing.T) {
	s := testServerWithCards(t)

	p, _ := s.core.CreateProject(context.Background(), "TEST", false)
	b, _ := s.core.CreateBoard(context.Background(), p.ID, "default", true)
	a, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "a"})
	d, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "d"})

	// POST a relation with dashes (should be converted to underscores)
	body := map[string]string{"rel": "resolved-by", "ref": d.Ref}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/p/"+p.Key+"/b/"+b.Slug+"/cards/"+a.Ref+"/relations",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp []core.CardRelation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp) != 1 || resp[0].Rel != "resolved_by" {
		t.Errorf("expected resolved_by relation")
	}
}

func TestDeleteCardRelation(t *testing.T) {
	s := testServerWithCards(t)

	p, _ := s.core.CreateProject(context.Background(), "TEST", false)
	b, _ := s.core.CreateBoard(context.Background(), p.ID, "default", true)
	a, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "a"})
	d, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "d"})
	s.core.RelateCards(context.Background(), p.ID, core.CardRef{Seq: a.Seq}, "resolved_by", core.CardRef{Seq: d.Seq})

	// DELETE the relation
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/p/%s/b/%s/cards/%s/relations/resolved_by/%s",
		p.Key, b.Slug, a.Ref, d.Ref), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []core.CardRelation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp) != 0 {
		t.Errorf("expected 0 relations after delete, got %d", len(resp))
	}
}

func TestDeleteCardRelationWithDashes(t *testing.T) {
	s := testServerWithCards(t)

	p, _ := s.core.CreateProject(context.Background(), "TEST", false)
	b, _ := s.core.CreateBoard(context.Background(), p.ID, "default", true)
	a, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "a"})
	d, _ := s.core.CreateCard(context.Background(), p.ID, b.ID, core.NewCard{Title: "d"})
	s.core.RelateCards(context.Background(), p.ID, core.CardRef{Seq: a.Seq}, "resolved_by", core.CardRef{Seq: d.Seq})

	// DELETE the relation with dashes
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/p/%s/b/%s/cards/%s/relations/resolved-by/%s",
		p.Key, b.Slug, a.Ref, d.Ref), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
