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

// TestGetSingleEntry tests GET /api/p/{key}/knowledge/{slug}
func TestGetSingleEntry(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	projKey := fmt.Sprintf("GETSINGLE%d", rand.Intn(100000))
	p, err := c.CreateProject(context.Background(), projKey, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(context.Background(), p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create an entry
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge", `{"title":"Test Entry","body":"Test content"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create entry status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var entry core.Entry
	if err := json.Unmarshal(createResp.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	// Test GET single entry returns body
	getResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/"+entry.Slug, "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("get single entry status = %d, expected 200, body = %s", getResp.Code, getResp.Body)
	}

	var gotEntry entryItem
	if err := json.Unmarshal(getResp.Body.Bytes(), &gotEntry); err != nil {
		t.Fatal(err)
	}

	if gotEntry.Entry.BodyMD == "" {
		t.Fatalf("expected body in single-entry response, got empty body")
	}
	if gotEntry.Entry.Title != "Test Entry" {
		t.Fatalf("expected title 'Test Entry', got %q", gotEntry.Entry.Title)
	}

	// Test GET non-existent entry returns 404
	notFoundResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/nonexistent", "")
	if notFoundResp.Code != http.StatusNotFound {
		t.Fatalf("get nonexistent entry status = %d, expected 404", notFoundResp.Code)
	}
}

// TestGetSingleEntryWithEncodedSlug tests percent-encoded slugs
func TestGetSingleEntryWithEncodedSlug(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	projKey := fmt.Sprintf("GETENC%d", rand.Intn(100000))
	p, err := c.CreateProject(context.Background(), projKey, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(context.Background(), p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create an entry with slash in title (becomes slash in slug)
	ctx := context.Background()
	entry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{
		Title: "Deployment/Steps",
		Body:  "How to deploy",
		Board: "board1",
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	// Test GET with percent-encoded slug
	encodedSlug := url.PathEscape(entry.Slug)
	getResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/"+encodedSlug, "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("get with encoded slug status = %d, expected 200, body = %s", getResp.Code, getResp.Body)
	}

	var gotEntry entryItem
	if err := json.Unmarshal(getResp.Body.Bytes(), &gotEntry); err != nil {
		t.Fatal(err)
	}

	if gotEntry.Entry.BodyMD == "" {
		t.Fatalf("expected body in response with encoded slug")
	}
}

// TestListEntriesExcludesBody tests that list endpoints don't include body
func TestListEntriesExcludesBody(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	projKey := fmt.Sprintf("LISTBODY%d", rand.Intn(100000))
	p, err := c.CreateProject(context.Background(), projKey, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(context.Background(), p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create an entry
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge", `{"title":"Test Entry","body":"Test content"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create entry status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var createdEntry core.Entry
	if err := json.Unmarshal(createResp.Body.Bytes(), &createdEntry); err != nil {
		t.Fatal(err)
	}

	// Test board entry list excludes body
	boardListResp := request(http.MethodGet, "/api/p/"+projKey+"/b/board1/knowledge", "")
	if boardListResp.Code != http.StatusOK {
		t.Fatalf("board list status = %d", boardListResp.Code)
	}

	var boardEntries []entryItem
	if err := json.Unmarshal(boardListResp.Body.Bytes(), &boardEntries); err != nil {
		t.Fatal(err)
	}

	if len(boardEntries) == 0 {
		t.Fatal("expected at least one entry")
	}

	for i, entry := range boardEntries {
		if entry.Entry.BodyMD != "" {
			t.Fatalf("board list entry[%d] should not have body, got: %q", i, entry.Entry.BodyMD)
		}
	}

	// Test project entry list excludes body
	projListResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge", "")
	if projListResp.Code != http.StatusOK {
		t.Fatalf("project list status = %d", projListResp.Code)
	}

	var projEntries []entryItem
	if err := json.Unmarshal(projListResp.Body.Bytes(), &projEntries); err != nil {
		t.Fatal(err)
	}

	if len(projEntries) == 0 {
		t.Fatal("expected at least one entry in project list")
	}

	for i, entry := range projEntries {
		if entry.Entry.BodyMD != "" {
			t.Fatalf("project list entry[%d] should not have body, got: %q", i, entry.Entry.BodyMD)
		}
	}
}

// TestPrivateEntryListExcludesSummaryRecap tests that private entries don't include summary/recap in lists
func TestPrivateEntryListExcludesSummaryRecap(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "PRIVLIST", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create a private entry with summary
	privateEntry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{
		Title:   "Private Entry",
		Summary: "Private summary",
		Body:    "Private content",
		Private: true,
		Board:   "board1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// PinEntry never stores a recap for a private entry (core's
	// TestPinOnPrivateStoresNoRecap covers that invariant directly), so
	// nothing in the normal lifecycle ever puts one on this row: asserting
	// Recap == nil below would hold no matter what the list handler does.
	// Seed one directly so the assertion actually depends on the handler's
	// own redaction (withoutContent), not on a recap never existing.
	if _, err := db.Exec(`UPDATE entry SET recap = ? WHERE id = ?`, "leaked recap text", privateEntry.ID); err != nil {
		t.Fatalf("seed recap: %v", err)
	}

	// Create a public entry with summary
	publicEntry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{
		Title:   "Public Entry",
		Summary: "Public summary",
		Body:    "Public content",
		Private: false,
		Board:   "board1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test board entry list
	boardListResp := request(http.MethodGet, "/api/p/PRIVLIST/b/board1/knowledge", "")
	if boardListResp.Code != http.StatusOK {
		t.Fatalf("board list status = %d", boardListResp.Code)
	}

	var boardEntries []entryItem
	if err := json.Unmarshal(boardListResp.Body.Bytes(), &boardEntries); err != nil {
		t.Fatal(err)
	}

	// Find the private and public entries in the list
	var foundPrivate, foundPublic entryItem
	for _, entry := range boardEntries {
		if entry.Entry.Slug == privateEntry.Slug {
			foundPrivate = entry
		}
		if entry.Entry.Slug == publicEntry.Slug {
			foundPublic = entry
		}
	}

	if foundPrivate.Entry.ID == "" {
		t.Fatal("private entry not found in list")
	}
	if foundPublic.Entry.ID == "" {
		t.Fatal("public entry not found in list")
	}

	// Private entry should have no summary/recap/body in list
	if foundPrivate.Entry.Summary != "" {
		t.Fatalf("private entry in list should not have summary, got: %q", foundPrivate.Entry.Summary)
	}
	if foundPrivate.Entry.Recap != nil {
		t.Fatalf("private entry in list should not have recap, got: %v", foundPrivate.Entry.Recap)
	}
	if foundPrivate.Entry.BodyMD != "" {
		t.Fatalf("private entry in list should not have body, got: %q", foundPrivate.Entry.BodyMD)
	}

	// Public entry should have no body but should have summary in list
	if foundPublic.Entry.BodyMD != "" {
		t.Fatalf("public entry in list should not have body, got: %q", foundPublic.Entry.BodyMD)
	}
	if foundPublic.Entry.Summary == "" {
		t.Fatalf("public entry in list should have summary")
	}
}

// TestGlobalEntryListExcludesBody tests that global entry list excludes body
func TestGlobalEntryListExcludesBody(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "GLOBTEST", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	entry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{Title: "Shared runbook", Summary: "restart order", Body: "the body\n"})
	if err != nil {
		t.Fatal(err)
	}
	global, err := c.PromoteEntry(ctx, p.ID, entry.Slug, "used everywhere")
	if err != nil {
		t.Fatal(err)
	}

	list := func() []core.Entry {
		t.Helper()
		rec := request(http.MethodGet, "/api/global/knowledge", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("global list status = %d, body = %s", rec.Code, rec.Body)
		}
		var entries []core.Entry
		if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("global list = %+v, want the one entry", entries)
		}
		return entries
	}

	entries := list()
	if entries[0].BodyMD != "" || entries[0].Summary != "restart order" {
		t.Fatalf("public global entry = %+v, want a summary and no body", entries[0])
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
	entries = list()
	if entries[0].BodyMD != "" || entries[0].Summary != "" || entries[0].Recap != nil || !entries[0].Private {
		t.Fatalf("hand-privatized global entry = %+v, want private with no body, summary or recap", entries[0])
	}
}

// TestGlobalEntryListNeverCarriesArtifacts guards the artifacts spec: an
// artifact belongs to a project, so the global list, which spans every
// project (and the vault, which has none), must never carry one, even for an
// entry that had an artifact linked before it was promoted.
func TestGlobalEntryListNeverCarriesArtifacts(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "GLOBART", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "board1", true); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
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

	entry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{Title: "Shared runbook"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.LinkArtifactToEntry(ctx, p.ID, entry.Slug, art.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromoteEntry(ctx, p.ID, entry.Slug, "used everywhere"); err != nil {
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

// TestGetEntryFields tests that Fields are included in responses
func TestGetEntryFields(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	projKey := fmt.Sprintf("FIELDS%d", rand.Intn(100000))
	p, err := c.CreateProject(ctx, projKey, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create an entry with Set fields
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge",
		`{"title":"Test Entry","body":"Content","set":{"owner":"alice","severity":"high"}}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var entry core.Entry
	if err := json.NewDecoder(createResp.Body).Decode(&entry); err != nil {
		t.Fatalf("decode create response: %v\n%s", err, createResp.Body)
	}

	// GET the detail endpoint
	detailResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge/"+entry.Slug, "")
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body)
	}

	var detailEntry core.Entry
	if err := json.NewDecoder(detailResp.Body).Decode(&detailEntry); err != nil {
		t.Fatalf("decode detail response: %v\n%s", err, detailResp.Body)
	}

	if detailEntry.Fields == nil {
		t.Error("Fields should be non-nil in detail endpoint")
	}
	if detailEntry.Fields["owner"] != "alice" {
		t.Errorf("Fields[owner] = %v, want alice", detailEntry.Fields["owner"])
	}
	if detailEntry.Fields["severity"] != "high" {
		t.Errorf("Fields[severity] = %v, want high", detailEntry.Fields["severity"])
	}
}

// TestGetEntryFieldsPrivateList tests that Fields are empty for private entries in lists
func TestGetEntryFieldsPrivateList(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	projKey := fmt.Sprintf("PRIVLIST%d", rand.Intn(100000))
	p, err := c.CreateProject(ctx, projKey, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(ctx, p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create a private entry with Set fields
	createResp := request(http.MethodPost, "/api/p/"+projKey+"/b/board1/knowledge",
		`{"title":"Private Entry","body":"Content","private":true,"set":{"owner":"alice"}}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body)
	}

	// List endpoints should have empty Fields for private entries
	listResp := request(http.MethodGet, "/api/p/"+projKey+"/knowledge", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body)
	}

	var entries []core.Entry
	if err := json.NewDecoder(listResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode list response: %v\n%s", err, listResp.Body)
	}

	var privEntry core.Entry
	for _, entry := range entries {
		if entry.Title == "Private Entry" {
			privEntry = entry
			break
		}
	}

	if privEntry.Fields == nil {
		t.Error("Fields should be non-nil even for private entries in list")
	}
	if len(privEntry.Fields) != 0 {
		t.Errorf("private entry Fields in list = %v, want empty map", privEntry.Fields)
	}
}
