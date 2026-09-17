package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// A web save sends title, summary and body together. Each one present must be
// written, and each one absent must keep its value.
func TestKnowledgeEditWritesTitleAndSummary(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-edit-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "EDIT", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	doc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{Title: "Before", Summary: "old summary", Body: "old body\n"})
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0")
	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/p/EDIT/b/default/knowledge/"+doc.Slug, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	rec := patch(`{"title":"After","summary":"new summary","body":"new body\n","version":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got, err := c.LoadKnowledge(ctx, p.ID, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "After" || got.Summary != "new summary" || !strings.Contains(got.BodyMD, "new body") {
		t.Fatalf("after full save: title=%q summary=%q body=%q", got.Title, got.Summary, got.BodyMD)
	}

	// Only the body: title and summary stay.
	rec = patch(`{"body":"third body\n","version":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got, err = c.LoadKnowledge(ctx, p.ID, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "After" || got.Summary != "new summary" || !strings.Contains(got.BodyMD, "third body") {
		t.Fatalf("after body-only save: title=%q summary=%q body=%q", got.Title, got.Summary, got.BodyMD)
	}

	// A save without a version is refused and changes nothing.
	rec = patch(`{"title":"Unversioned"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unversioned status = %d, want 400, body = %s", rec.Code, rec.Body)
	}
	// A stale version is a conflict.
	rec = patch(`{"title":"Stale","version":1}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, want 409, body = %s", rec.Code, rec.Body)
	}
	got, err = c.LoadKnowledge(ctx, p.ID, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "After" || got.Version != 3 {
		t.Fatalf("refused saves changed the entry: title=%q version=%d", got.Title, got.Version)
	}
}

// EditKnowledgeMetadata tests changing type, private, tags, and labels via PATCH.
func TestKnowledgeEditMetadata(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-edit-metadata-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "META", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateLabel(ctx, p.ID, "reviewed", ""); err != nil {
		t.Fatal(err)
	}
	doc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{Title: "Metadata", Body: "original\n"})
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0")
	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/p/META/b/default/knowledge/"+doc.Slug, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Set template, private, tags, and labels
	rec := patch(`{"template":"research","private":true,"tags":["new"],"labels":["reviewed"],"version":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got, err := c.LoadKnowledge(ctx, p.ID, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Template != "research" || !got.Private || len(got.Tags) != 1 || got.Tags[0] != "new" || len(got.Labels) != 1 || got.Labels[0] != "reviewed" {
		t.Fatalf("after edit: template=%q private=%v tags=%v labels=%v", got.Template, got.Private, got.Tags, got.Labels)
	}

	// Clear tags and labels, set private to false
	rec = patch(`{"private":false,"tags":[],"labels":[],"version":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got, err = c.LoadKnowledge(ctx, p.ID, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Private || len(got.Tags) != 0 || len(got.Labels) != 0 || got.Template != "research" {
		t.Fatalf("after clear: template=%q private=%v tags=%v labels=%v", got.Template, got.Private, got.Tags, got.Labels)
	}
}

// A template that rejects an entry without sources must still be usable from
// the web, so the create request carries sources and template fields.
func TestKnowledgeCreateCarriesSources(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-create-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "CREATE", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	s := NewServer(c, db, "127.0.0.1:0")
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/p/CREATE/b/default/knowledge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	rec := post(`{"title":"Chose SQLite","template":"decision"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a decision without sources: status = %d, want 400, body = %s", rec.Code, rec.Body)
	}

	rec = post(`{"title":"Chose SQLite","template":"decision","sources":["https://sqlite.org/whentouse.html"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a decision with a source: status = %d, want 201, body = %s", rec.Code, rec.Body)
	}
	doc, err := c.LoadKnowledge(ctx, p.ID, "chose-sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Sources) != 1 || doc.Sources[0] != "https://sqlite.org/whentouse.html" {
		t.Fatalf("sources = %v", doc.Sources)
	}
}

// Without a private field on the create request, the only way to make an
// entry private from the web is to create it ordinary and PATCH it private
// afterward -- a window in which the body already reached an embedder under
// vector search. A private entry created over HTTP must be private from its
// first write: the file on disk must say so from the moment CreateKnowledge
// returns, not after a follow-up request.
func TestKnowledgeCreatePrivateOverHTTPIsPrivateFromFirstWrite(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-create-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "CREATE", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	s := NewServer(c, db, "127.0.0.1:0")
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/p/CREATE/b/default/knowledge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	rec := post(`{"title":"Staging credentials","private":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("private create status = %d, body = %s", rec.Code, rec.Body)
	}
	var created core.Knowledge
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.Private {
		t.Fatalf("created response Private = false, want true: %s", rec.Body)
	}

	raw, err := os.ReadFile(created.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "private: true") {
		t.Errorf("file written by the create request is missing private: true:\n%s", raw)
	}

	doc, err := c.LoadKnowledge(ctx, p.ID, created.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Private {
		t.Fatal("reloaded doc Private = false, want true")
	}
}
