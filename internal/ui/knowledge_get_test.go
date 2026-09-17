package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// TestGetSingleKnowledge tests GET /api/p/{key}/knowledge/{slug}
func TestGetSingleKnowledge(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	projKey := fmt.Sprintf("GETSINGLE%d", rand.Intn(100000))
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

	// Create a knowledge entry
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge", `{"title":"Test Doc","body":"Test content"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create knowledge status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var doc core.Knowledge
	if err := json.Unmarshal(createResp.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}

	// Test GET single knowledge returns body
	getResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/"+doc.Slug, "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("get single knowledge status = %d, expected 200, body = %s", getResp.Code, getResp.Body)
	}

	var gotDoc knowledgeItem
	if err := json.Unmarshal(getResp.Body.Bytes(), &gotDoc); err != nil {
		t.Fatal(err)
	}

	if gotDoc.Knowledge.BodyMD == "" {
		t.Fatalf("expected body in single-knowledge response, got empty body")
	}
	if gotDoc.Knowledge.Title != "Test Doc" {
		t.Fatalf("expected title 'Test Doc', got %q", gotDoc.Knowledge.Title)
	}

	// Test GET non-existent knowledge returns 404
	notFoundResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/nonexistent", "")
	if notFoundResp.Code != http.StatusNotFound {
		t.Fatalf("get nonexistent knowledge status = %d, expected 404", notFoundResp.Code)
	}
}

// TestGetSingleKnowledgeWithEncodedSlug tests percent-encoded slugs
func TestGetSingleKnowledgeWithEncodedSlug(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	projKey := fmt.Sprintf("GETENC%d", rand.Intn(100000))
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

	// Create a knowledge entry with slash in title (becomes slash in slug)
	ctx := context.Background()
	doc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{
		Title: "Deployment/Steps",
		Body:  "How to deploy",
		Board: "board1",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	// Test GET with percent-encoded slug
	encodedSlug := url.PathEscape(doc.Slug)
	getResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/"+encodedSlug, "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("get with encoded slug status = %d, expected 200, body = %s", getResp.Code, getResp.Body)
	}

	var gotDoc knowledgeItem
	if err := json.Unmarshal(getResp.Body.Bytes(), &gotDoc); err != nil {
		t.Fatal(err)
	}

	if gotDoc.Knowledge.BodyMD == "" {
		t.Fatalf("expected body in response with encoded slug")
	}
}

// TestListKnowledgeExcludesBody tests that list endpoints don't include body
func TestListKnowledgeExcludesBody(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	projKey := fmt.Sprintf("LISTBODY%d", rand.Intn(100000))
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

	// Create a knowledge entry
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge", `{"title":"Test Doc","body":"Test content"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create knowledge status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var createdDoc core.Knowledge
	if err := json.Unmarshal(createResp.Body.Bytes(), &createdDoc); err != nil {
		t.Fatal(err)
	}

	// Test board knowledge list excludes body
	boardListResp := request(http.MethodGet, "/api/p/"+projKey+"/b/board1/knowledge", "")
	if boardListResp.Code != http.StatusOK {
		t.Fatalf("board list status = %d", boardListResp.Code)
	}

	var boardDocs []knowledgeItem
	if err := json.Unmarshal(boardListResp.Body.Bytes(), &boardDocs); err != nil {
		t.Fatal(err)
	}

	if len(boardDocs) == 0 {
		t.Fatal("expected at least one knowledge entry")
	}

	for i, doc := range boardDocs {
		if doc.Knowledge.BodyMD != "" {
			t.Fatalf("board list doc[%d] should not have body, got: %q", i, doc.Knowledge.BodyMD)
		}
	}

	// Test project knowledge list excludes body
	projListResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge", "")
	if projListResp.Code != http.StatusOK {
		t.Fatalf("project list status = %d", projListResp.Code)
	}

	var projDocs []knowledgeItem
	if err := json.Unmarshal(projListResp.Body.Bytes(), &projDocs); err != nil {
		t.Fatal(err)
	}

	if len(projDocs) == 0 {
		t.Fatal("expected at least one knowledge entry in project list")
	}

	for i, doc := range projDocs {
		if doc.Knowledge.BodyMD != "" {
			t.Fatalf("project list doc[%d] should not have body, got: %q", i, doc.Knowledge.BodyMD)
		}
	}
}

// TestPrivateKnowledgeListExcludesSummaryRecap tests that private entries don't include summary/recap in lists
func TestPrivateKnowledgeListExcludesSummaryRecap(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "PRIVLIST", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
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

	// Create a private knowledge entry with summary
	privateDoc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{
		Title:   "Private Doc",
		Summary: "Private summary",
		Body:    "Private content",
		Private: true,
		Board:   "board1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// PinKnowledge never stores a recap for a private entry (core's
	// TestPinOnPrivateStoresNoRecap covers that invariant directly), so
	// nothing in the normal lifecycle ever puts one on this row: asserting
	// Recap == nil below would hold no matter what the list handler does.
	// Seed one directly so the assertion actually depends on the handler's
	// own redaction (withoutContent), not on a recap never existing.
	if _, err := db.Exec(`UPDATE knowledge SET recap = ? WHERE id = ?`, "leaked recap text", privateDoc.ID); err != nil {
		t.Fatalf("seed recap: %v", err)
	}

	// Create a public knowledge entry with summary
	publicDoc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{
		Title:   "Public Doc",
		Summary: "Public summary",
		Body:    "Public content",
		Private: false,
		Board:   "board1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test board knowledge list
	boardListResp := request(http.MethodGet, "/api/p/PRIVLIST/b/board1/knowledge", "")
	if boardListResp.Code != http.StatusOK {
		t.Fatalf("board list status = %d", boardListResp.Code)
	}

	var boardDocs []knowledgeItem
	if err := json.Unmarshal(boardListResp.Body.Bytes(), &boardDocs); err != nil {
		t.Fatal(err)
	}

	// Find the private and public docs in the list
	var foundPrivate, foundPublic knowledgeItem
	for _, doc := range boardDocs {
		if doc.Knowledge.Slug == privateDoc.Slug {
			foundPrivate = doc
		}
		if doc.Knowledge.Slug == publicDoc.Slug {
			foundPublic = doc
		}
	}

	if foundPrivate.Knowledge.ID == "" {
		t.Fatal("private doc not found in list")
	}
	if foundPublic.Knowledge.ID == "" {
		t.Fatal("public doc not found in list")
	}

	// Private entry should have no summary/recap/body in list
	if foundPrivate.Knowledge.Summary != "" {
		t.Fatalf("private doc in list should not have summary, got: %q", foundPrivate.Knowledge.Summary)
	}
	if foundPrivate.Knowledge.Recap != nil {
		t.Fatalf("private doc in list should not have recap, got: %v", foundPrivate.Knowledge.Recap)
	}
	if foundPrivate.Knowledge.BodyMD != "" {
		t.Fatalf("private doc in list should not have body, got: %q", foundPrivate.Knowledge.BodyMD)
	}

	// Public entry should have no body but should have summary in list
	if foundPublic.Knowledge.BodyMD != "" {
		t.Fatalf("public doc in list should not have body, got: %q", foundPublic.Knowledge.BodyMD)
	}
	if foundPublic.Knowledge.Summary == "" {
		t.Fatalf("public doc in list should have summary")
	}
}

// TestGlobalKnowledgeListExcludesBody tests that global knowledge list excludes body
func TestGlobalKnowledgeListExcludesBody(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "GLOBTEST", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
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

	doc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{Title: "Shared runbook", Summary: "restart order", Body: "the body\n"})
	if err != nil {
		t.Fatal(err)
	}
	global, err := c.EscalateKnowledge(ctx, p.ID, doc.Slug, "used everywhere")
	if err != nil {
		t.Fatal(err)
	}

	list := func() []core.Knowledge {
		t.Helper()
		rec := request(http.MethodGet, "/api/global/knowledge", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("global list status = %d, body = %s", rec.Code, rec.Body)
		}
		var docs []core.Knowledge
		if err := json.Unmarshal(rec.Body.Bytes(), &docs); err != nil {
			t.Fatal(err)
		}
		if len(docs) != 1 {
			t.Fatalf("global list = %+v, want the one entry", docs)
		}
		return docs
	}

	docs := list()
	if docs[0].BodyMD != "" || docs[0].Summary != "restart order" {
		t.Fatalf("public global entry = %+v, want a summary and no body", docs[0])
	}

	// Marked private by hand: the mirror is stale until the file is read, and
	// the list must go by the file.
	raw, err := os.ReadFile(global.Path)
	if err != nil {
		t.Fatal(err)
	}
	marked := strings.Replace(string(raw), "---\n", "---\nprivate: true\n", 1)
	if err := os.WriteFile(global.Path, []byte(marked), 0o600); err != nil {
		t.Fatal(err)
	}
	docs = list()
	if docs[0].BodyMD != "" || docs[0].Summary != "" || docs[0].Recap != nil || !docs[0].Private {
		t.Fatalf("hand-privatized global entry = %+v, want private with no body, summary or recap", docs[0])
	}
}

// TestGlobalKnowledgeListNeverCarriesArtifacts guards the artifacts spec: an
// artifact belongs to a project, so the global list, which spans every
// project (and the vault, which has none), must never carry one, even for an
// entry that had an artifact linked before it was escalated.
func TestGlobalKnowledgeListNeverCarriesArtifacts(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "GLOBART", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "board1", true); err != nil {
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

	src := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(src, []byte("ID3 audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	art, err := c.CreateArtifact(ctx, p.ID, src)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{Title: "Shared runbook"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.LinkArtifactToDoc(ctx, p.ID, doc.Slug, art.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, doc.Slug, "used everywhere"); err != nil {
		t.Fatal(err)
	}

	rec := request(http.MethodGet, "/api/global/knowledge", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("global list status = %d, body = %s", rec.Code, rec.Body)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"artifacts"`)) {
		t.Fatalf("global list must never carry artifacts: %s", rec.Body)
	}
}

// TestGetKnowledgeFields tests that Fields are included in responses
func TestGetKnowledgeFields(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	projKey := fmt.Sprintf("FIELDS%d", rand.Intn(100000))
	p, err := c.CreateProject(ctx, projKey, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
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

	// Create a knowledge entry with Set fields
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge",
		`{"title":"Test Doc","body":"Content","set":{"owner":"alice","severity":"high"}}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var doc core.Knowledge
	if err := json.NewDecoder(createResp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode create response: %v\n%s", err, createResp.Body)
	}

	// GET the detail endpoint
	detailResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/"+doc.Slug, "")
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body)
	}

	var detailDoc core.Knowledge
	if err := json.NewDecoder(detailResp.Body).Decode(&detailDoc); err != nil {
		t.Fatalf("decode detail response: %v\n%s", err, detailResp.Body)
	}

	if detailDoc.Fields == nil {
		t.Error("Fields should be non-nil in detail endpoint")
	}
	if detailDoc.Fields["owner"] != "alice" {
		t.Errorf("Fields[owner] = %v, want alice", detailDoc.Fields["owner"])
	}
	if detailDoc.Fields["severity"] != "high" {
		t.Errorf("Fields[severity] = %v, want high", detailDoc.Fields["severity"])
	}
}

// TestGetKnowledgeFieldsPrivateList tests that Fields are empty for private entries in lists
func TestGetKnowledgeFieldsPrivateList(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test")
	projKey := fmt.Sprintf("PRIVLIST%d", rand.Intn(100000))
	p, err := c.CreateProject(ctx, projKey, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
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

	// Create a private knowledge entry with Set fields
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge",
		`{"title":"Private Doc","body":"Content","private":true,"set":{"owner":"alice"}}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body)
	}

	// List endpoints should have empty Fields for private entries
	listResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body)
	}

	var docs []core.Knowledge
	if err := json.NewDecoder(listResp.Body).Decode(&docs); err != nil {
		t.Fatalf("decode list response: %v\n%s", err, listResp.Body)
	}

	var privDoc core.Knowledge
	for _, doc := range docs {
		if doc.Title == "Private Doc" {
			privDoc = doc
			break
		}
	}

	if privDoc.Fields == nil {
		t.Error("Fields should be non-nil even for private entries in list")
	}
	if len(privDoc.Fields) != 0 {
		t.Errorf("private entry Fields in list = %v, want empty map", privDoc.Fields)
	}
}
