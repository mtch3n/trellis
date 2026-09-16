package ui

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

func artifactTestServer(t *testing.T) (*Server, *core.Core, core.Project) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-artifact-test").WithKBRoot(t.TempDir())
	p, err := c.EnsureProject(t.Context(), resolve.Identity{Kind: "test", Value: "art", SuggestedKey: "ART"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(t.Context(), p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	return NewServer(c, db, "127.0.0.1:0"), c, p
}

func storeArtifact(t *testing.T, c *core.Core, projectID, filename string, content []byte) core.Artifact {
	t.Helper()
	src := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := c.CreateArtifact(t.Context(), projectID, src)
	if err != nil {
		t.Fatalf("CreateArtifact %s: %v", filename, err)
	}
	return a
}

func getJSON(t *testing.T, s *Server, path string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
	}
	var items []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return items
}

func itemBySlug(t *testing.T, items []map[string]any, slug string) map[string]any {
	t.Helper()
	for _, it := range items {
		if it["slug"] == slug {
			return it
		}
	}
	t.Fatalf("no item with slug %q", slug)
	return nil
}

func TestKnowledgeListsCarryArtifacts(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "clip.mp3", []byte("ID3 audio"))

	with, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "With"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.LinkArtifactToDoc(t.Context(), p.ID, with.Slug, a.Name); err != nil {
		t.Fatal(err)
	}

	stub, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "Stub"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(stub.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, body, err := core.SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	fm.Artifacts = []string{"absent.pdf"}
	if err := os.WriteFile(stub.Path, []byte(core.RenderDoc(fm, body)), 0o600); err != nil {
		t.Fatal(err)
	}

	plain, err := c.CreateKnowledge(t.Context(), p.ID, core.NewKnowledge{Title: "Plain"})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/p/ART/knowledge", "/api/p/ART/b/default/knowledge"} {
		t.Run(path, func(t *testing.T) {
			items := getJSON(t, s, path)

			arts, ok := itemBySlug(t, items, with.Slug)["artifacts"].([]any)
			if !ok || len(arts) != 1 {
				t.Fatalf("with: artifacts = %v", itemBySlug(t, items, with.Slug)["artifacts"])
			}
			got := arts[0].(map[string]any)
			if got["name"] != a.Name || got["kind"] != "audio" || got["missing"] != false ||
				got["url"] != "/api/p/ART/artifacts/"+a.Name {
				t.Errorf("resolved artifact = %v", got)
			}

			stubArts, ok := itemBySlug(t, items, stub.Slug)["artifacts"].([]any)
			if !ok || len(stubArts) != 1 {
				t.Fatalf("stub: artifacts = %v", itemBySlug(t, items, stub.Slug)["artifacts"])
			}
			missing := stubArts[0].(map[string]any)
			if missing["missing"] != true {
				t.Errorf("stub = %v, want missing: true", missing)
			}
			for _, k := range []string{"url", "kind", "mime", "size"} {
				if _, present := missing[k]; present {
					t.Errorf("stub carries %q: %v", k, missing)
				}
			}

			if _, present := itemBySlug(t, items, plain.Slug)["artifacts"]; present {
				t.Errorf("an entry with no artifacts carries the field")
			}
		})
	}
}
