package ui

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// The web lists templates from the server, the user's included, with the
// rules a form needs.
func TestTemplatesRouteListsRules(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test").WithKBRoot(root)
	s := NewServer(c, db, "127.0.0.1:0")
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

	dir := filepath.Join(root, "templates")
	custom := "---\nrequired: [owner]\nchoices:\n  severity: [low, high]\n---\n# {{title}}\n"
	if err := os.WriteFile(filepath.Join(dir, "incident.md"), []byte(custom), 0o600); err != nil {
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
