package ui

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

func TestServerCardLifecycleAndEmbeddedSPA(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	p, err := c.EnsureProject(context.Background(), resolve.Identity{Kind: "test", Value: "ui", SuggestedKey: "UITEST"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(context.Background(), p.ID, "default", true); err != nil {
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

	created := request(http.MethodPost, "/api/p/UITEST/b/default/cards", `{"title":"API card","body":"context","priority":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body)
	}
	var card core.Card
	if err := json.Unmarshal(created.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if card.Ref != "UITEST-1" {
		t.Fatalf("created ref = %q, want UITEST-1", card.Ref)
	}
	detail := request(http.MethodGet, "/api/p/UITEST/b/default/cards/UITEST-1", "")
	if detail.Code != http.StatusOK || !bytes.Contains(detail.Body.Bytes(), []byte(`"activity"`)) {
		t.Fatalf("detail status = %d, body = %s", detail.Code, detail.Body)
	}
	projectDetail := request(http.MethodGet, "/api/p/UITEST/cards/UITEST-1", "")
	if projectDetail.Code != http.StatusOK || !bytes.Contains(projectDetail.Body.Bytes(), []byte(`"card"`)) {
		t.Fatalf("project detail status = %d, body = %s", projectDetail.Code, projectDetail.Body)
	}

	updated := request(http.MethodPatch, "/api/p/UITEST/b/default/cards/UITEST-1", `{"title":"Updated","body":"new context","if_version":1}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body)
	}

	moved := request(http.MethodPost, "/api/p/UITEST/b/default/cards/UITEST-1/move", `{"column":"done"}`)
	if moved.Code != http.StatusOK {
		t.Fatalf("move status = %d, body = %s", moved.Code, moved.Body)
	}

	stale := request(http.MethodPatch, "/api/p/UITEST/b/default/cards/UITEST-1", `{"title":"stale","if_version":1}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d, want %d, body = %s", stale.Code, http.StatusConflict, stale.Body)
	}

	page := request(http.MethodGet, "/", "")
	if page.Code != http.StatusOK || !bytes.Contains(page.Body.Bytes(), []byte("<div id=\"root\">")) {
		t.Fatalf("SPA response = %d, body = %s", page.Code, page.Body.String())
	}
}

func TestServerKnowledgeGraphLabelsAndStealRoutes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-p5-test").WithKBRoot(t.TempDir())
	p, err := c.EnsureProject(context.Background(), resolve.Identity{Kind: "test", Value: "p5", SuggestedKey: "P5TEST"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(context.Background(), p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateLabel(context.Background(), p.ID, "old", "legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateLabel(context.Background(), p.ID, "new", "current"); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	created := request(http.MethodPost, "/api/p/P5TEST/b/default/knowledge", `{"title":"Concurrency","summary":"leases","body":"first"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("knowledge create status = %d, body = %s", created.Code, created.Body)
	}
	var doc core.Knowledge
	if err := json.Unmarshal(created.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Slug == "" || doc.Version == 0 {
		t.Fatalf("created knowledge = %+v", doc)
	}

	listed := request(http.MethodGet, "/api/p/P5TEST/b/default/knowledge", "")
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"slug":"`+doc.Slug+`"`)) {
		t.Fatalf("knowledge list status = %d, body = %s", listed.Code, listed.Body)
	}
	projectKnowledge := request(http.MethodGet, "/api/p/P5TEST/knowledge", "")
	if projectKnowledge.Code != http.StatusOK || !bytes.Contains(projectKnowledge.Body.Bytes(), []byte(`"slug":"`+doc.Slug+`"`)) {
		t.Fatalf("project knowledge status = %d, body = %s", projectKnowledge.Code, projectKnowledge.Body)
	}
	globalKnowledge := request(http.MethodGet, "/api/global/knowledge", "")
	if globalKnowledge.Code != http.StatusOK {
		t.Fatalf("global knowledge status = %d, body = %s", globalKnowledge.Code, globalKnowledge.Body)
	}
	edited := request(http.MethodPatch, "/api/p/P5TEST/b/default/knowledge/"+doc.Slug, `{"body":"second","version":1}`)
	if edited.Code != http.StatusOK || !bytes.Contains(edited.Body.Bytes(), []byte("second")) {
		t.Fatalf("knowledge edit status = %d, body = %s", edited.Code, edited.Body)
	}
	graph := request(http.MethodGet, "/api/p/P5TEST/b/default/graph/"+doc.Slug, "")
	if graph.Code != http.StatusOK || !bytes.Contains(graph.Body.Bytes(), []byte(`"nodes"`)) {
		t.Fatalf("graph status = %d, body = %s", graph.Code, graph.Body)
	}
	search := request(http.MethodGet, "/api/search?q=Concurrency", "")
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte(`"kind":"knowledge"`)) {
		t.Fatalf("search status = %d, body = %s", search.Code, search.Body)
	}
	activity := request(http.MethodGet, "/api/activity?limit=10", "")
	if activity.Code != http.StatusOK || !bytes.Contains(activity.Body.Bytes(), []byte(`"entity_type":"knowledge"`)) {
		t.Fatalf("activity status = %d, body = %s", activity.Code, activity.Body)
	}

	labels := request(http.MethodGet, "/api/p/P5TEST/labels", "")
	if labels.Code != http.StatusOK || !bytes.Contains(labels.Body.Bytes(), []byte(`"name":"old"`)) {
		t.Fatalf("labels status = %d, body = %s", labels.Code, labels.Body)
	}
	merged := request(http.MethodPost, "/api/p/P5TEST/labels/merge", `{"from":"old","into":"new"}`)
	if merged.Code != http.StatusOK {
		t.Fatalf("label merge status = %d, body = %s", merged.Code, merged.Body)
	}
	if _, err := c.GetLabel(context.Background(), p.ID, "old"); err == nil {
		t.Fatal("merged label still exists")
	}

	board, err := c.SelectBoard(context.Background(), p.ID, "default")
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(context.Background(), p.ID, board.ID, core.NewCard{Title: "Held"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClaimCard(context.Background(), card.ID, 30*60*1000, false, ""); err != nil {
		t.Fatal(err)
	}
	stolen := request(http.MethodPost, "/api/p/P5TEST/b/default/cards/"+card.Ref+"/steal", `{"reason":"owner is inactive"}`)
	if stolen.Code != http.StatusOK {
		t.Fatalf("steal status = %d, body = %s", stolen.Code, stolen.Body)
	}
}
