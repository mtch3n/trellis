package ui

import (
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

// The web lists templates from the server, the user's included, with the
// rules a form needs.
func TestTemplatesRouteListsRules(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", root)
	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	get := func() []core.TemplateInfo {
		t.Helper()
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/templates", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		var out []core.TemplateInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("not JSON: %v, body = %.200s", err, rec.Body)
		}
		return out
	}

	byName := map[string]core.TemplateInfo{}
	for _, tmpl := range get() {
		byName[tmpl.Name] = tmpl
	}
	dec, ok := byName["decision"]
	if !ok || dec.Enforce != "reject" || !dec.BuiltIn || len(dec.Required) != 1 || dec.Required[0] != "sources" || len(dec.Sections) == 0 {
		t.Fatalf("decision = %+v", dec)
	}
	if _, ok := byName["note"]; ok {
		t.Error("the note template is still listed")
	}

	templatesDir := filepath.Join(root, "templates")
	custom := "---\nrequired: [owner]\nchoices:\n  severity: [low, high]\n---\n# {{title}}\n"
	if err := os.WriteFile(filepath.Join(templatesDir, "incident.md"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tmpl := range get() {
		if tmpl.Name == "incident" {
			if tmpl.BuiltIn || tmpl.Enforce != "warn" || len(tmpl.Choices["severity"]) != 2 {
				t.Errorf("incident = %+v", tmpl)
			}
			return
		}
	}
	t.Error("a user template is not listed")
}

// templateCRUDServer builds a server backed by a fresh KB root, for the
// per-template routes: get, put, post, delete and reinstall.
func templateCRUDServer(t *testing.T) (*Server, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "trellis.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", root)
	return NewServer(c, db, "127.0.0.1:0", dbPath), root
}

func TestTemplateShowRoute(t *testing.T) {
	s, _ := templateCRUDServer(t)
	rec := request(t, s, http.MethodGet, "/api/templates/decision", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Name    string `json:"name"`
		BuiltIn bool   `json:"builtin"`
		Enforce string `json:"enforce"`
		Raw     string `json:"raw"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.Name != "decision" || !out.BuiltIn || out.Raw == "" {
		t.Errorf("show decision = %+v", out)
	}
}

func TestTemplatePutRoute(t *testing.T) {
	s, root := templateCRUDServer(t)
	// Seed the templates directory the way ShowTemplate/ListTemplates would.
	if rec := request(t, s, http.MethodGet, "/api/templates/decision", ""); rec.Code != http.StatusOK {
		t.Fatalf("seed status = %d", rec.Code)
	}

	good := `{"raw":"---\nenforce: warn\n---\n# {{title}}\n\ncustom body\n"}`
	rec := request(t, s, http.MethodPut, "/api/templates/decision", good)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if !strings.Contains(out.Raw, "custom body") {
		t.Errorf("raw = %q, want the new body", out.Raw)
	}

	before, err := os.ReadFile(filepath.Join(root, "templates", "decision.md"))
	if err != nil {
		t.Fatal(err)
	}
	bad := `{"raw":"---\nenforce: bogus\n---\n# {{title}}\n"}`
	badRec := request(t, s, http.MethodPut, "/api/templates/decision", bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad put status = %d, body = %s", badRec.Code, badRec.Body)
	}
	after, err := os.ReadFile(filepath.Join(root, "templates", "decision.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a rejected template write changed the file on disk")
	}
}

func TestTemplatePostRoute(t *testing.T) {
	s, _ := templateCRUDServer(t)
	rec := request(t, s, http.MethodPost, "/api/templates", `{"name":"incident"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.Name != "incident" {
		t.Errorf("name = %q, want incident", out.Name)
	}

	badName := request(t, s, http.MethodPost, "/api/templates", `{"name":"Not Valid!"}`)
	if badName.Code != http.StatusBadRequest {
		t.Fatalf("bad name status = %d, body = %s", badName.Code, badName.Body)
	}

	conflict := request(t, s, http.MethodPost, "/api/templates", `{"name":"incident"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body)
	}
}

func TestTemplateDeleteAndReinstallRoutes(t *testing.T) {
	s, _ := templateCRUDServer(t)
	if rec := request(t, s, http.MethodGet, "/api/templates/decision", ""); rec.Code != http.StatusOK {
		t.Fatalf("seed status = %d", rec.Code)
	}

	del := request(t, s, http.MethodDelete, "/api/templates/decision", "")
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", del.Code, del.Body)
	}

	missing := request(t, s, http.MethodGet, "/api/templates/decision", "")
	if missing.Code == http.StatusOK {
		t.Fatal("decision still shows after delete")
	}

	reinstall := request(t, s, http.MethodPost, "/api/templates/decision/reinstall", `{}`)
	if reinstall.Code != http.StatusOK {
		t.Fatalf("reinstall status = %d, body = %s", reinstall.Code, reinstall.Body)
	}
	var out struct {
		Name    string `json:"name"`
		BuiltIn bool   `json:"builtin"`
	}
	if err := json.Unmarshal(reinstall.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, reinstall.Body)
	}
	if out.Name != "decision" || !out.BuiltIn {
		t.Errorf("reinstall = %+v", out)
	}

	notBuiltin := request(t, s, http.MethodPost, "/api/templates/nonexistent/reinstall", `{}`)
	if notBuiltin.Code != http.StatusBadRequest {
		t.Fatalf("reinstall unknown status = %d, body = %s", notBuiltin.Code, notBuiltin.Body)
	}
}
