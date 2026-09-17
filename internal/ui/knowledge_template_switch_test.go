package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// A browser switches an entry to a strict template and meets it in the same
// save, reads a refusal as a list, and creates an entry inside a directory.
func TestKnowledgeTemplateSwitchFromTheWeb(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 3_000_000}, "ui-switch-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "SWITCH", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	s := NewServer(c, db, "127.0.0.1:0")
	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/p/SWITCH/b/default/knowledge"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	rec := send(http.MethodPost, "", `{"title":"Rollback","dir":"ops","body":"plain\n"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create in ops: %d %s", rec.Code, rec.Body)
	}
	var doc core.Knowledge
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Slug != "ops/rollback" {
		t.Fatalf("slug = %q, want ops/rollback", doc.Slug)
	}
	path := "/ops%2Frollback"

	rec = send(http.MethodPatch, path, `{"template":"decision","version":1}`)
	var refusal struct {
		Error    string   `json:"error"`
		Code     string   `json:"code"`
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("refusal is not JSON: %v, %s", err, rec.Body)
	}
	if rec.Code != http.StatusBadRequest || refusal.Code != "template_violation" ||
		!strings.Contains(strings.Join(refusal.Problems, "|"), "missing required field sources") ||
		strings.Contains(refusal.Error, "run:") || strings.Contains(refusal.Error, "\n") {
		t.Fatalf("refusal = %d %+v", rec.Code, refusal)
	}

	body := "# Rollback\n\n## Context\n\nc\n\n## Options considered\n\no\n\n## Decision\n\nd\n\n## Consequences\n\nq\n"
	patch, _ := json.Marshal(map[string]any{
		"template": "decision", "version": 1, "body": body,
		"sources": []string{"https://example.com/postmortem"}, "set": map[string]string{"owner": "ops"},
	})
	rec = send(http.MethodPatch, path, string(patch))
	if rec.Code != http.StatusOK {
		t.Fatalf("switch with sources: %d %s", rec.Code, rec.Body)
	}
	got, err := c.ReadKnowledge(ctx, p.ID, "ops/rollback")
	if err != nil {
		t.Fatal(err)
	}
	if got.Template != "decision" || len(got.Sources) != 1 {
		t.Fatalf("after the switch: template %q, sources %v", got.Template, got.Sources)
	}
}
