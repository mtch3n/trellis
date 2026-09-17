package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestKnowledgeLinksEndpoint(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	p, err := c.CreateProject(context.Background(), "UITEST", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBoard(context.Background(), p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "", filepath.Join(dir, "trellis.db"))
	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	// 1. empty array test
	resp, err := http.Get(ts.URL + "/api/p/UITEST/links/knowledge")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty array got status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
	var empty []core.KnowledgeLink
	if err := json.UnmarshalRead(resp.Body, &empty); err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected empty array `[]`, got %v", empty)
	}

	// 2. 404 for unknown project
	resp404, err := http.Get(ts.URL + "/api/p/UNKNOWN/links/knowledge")
	if err != nil {
		t.Fatal(err)
	}
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown project, got %d", resp404.StatusCode)
	}
	if ct := resp404.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected 404 to be JSON, got %q", ct)
	}

	// 3. JSON shape test
	// Create a document with a link
	_, err = c.CreateKnowledge(context.Background(), p.ID, core.NewKnowledge{
		Title: "Source",
		Body:  "[[target]]\n",
		Board: b.Name,
	})
	if err != nil {
		t.Fatal(err)
	}

	respJSON, err := http.Get(ts.URL + "/api/p/UITEST/links/knowledge")
	if err != nil {
		t.Fatal(err)
	}
	defer respJSON.Body.Close()
	if respJSON.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", respJSON.StatusCode)
	}
	var links []core.KnowledgeLink
	if err := json.UnmarshalRead(respJSON.Body, &links); err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Raw != "target" {
		t.Errorf("expected raw 'target', got %q", links[0].Raw)
	}
	if links[0].From != "/UITEST/knowledge/source" {
		t.Errorf("expected from '/UITEST/knowledge/source', got %q", links[0].From)
	}
	if links[0].To != nil {
		t.Errorf("expected to be null (stub), got %q", *links[0].To)
	}
}
