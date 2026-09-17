package ui

import (
	"encoding/json/v2"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func artifactTestServer(t *testing.T) (*Server, *core.Core, core.Project) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-artifact-test", dir)
	p, err := c.CreateProject(t.Context(), "ART", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(t.Context(), p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	return NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db")), c, p
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

func TestEntryListsCarryArtifacts(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "clip.mp3", []byte("ID3 audio"))

	with, err := c.CreateEntry(t.Context(), p.ID, core.NewEntry{Title: "With"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.LinkArtifactToEntry(t.Context(), p.ID, with.Slug, a.Name); err != nil {
		t.Fatal(err)
	}

	stub, err := c.CreateEntry(t.Context(), p.ID, core.NewEntry{Title: "Stub"})
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
	if err := os.WriteFile(stub.Path, []byte(core.RenderEntry(fm, body)), 0o600); err != nil {
		t.Fatal(err)
	}

	plain, err := c.CreateEntry(t.Context(), p.ID, core.NewEntry{Title: "Plain"})
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

var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\nimage-data")
	mp3Bytes  = []byte("ID3\x03\x00audio-data")
	mp4Bytes  = []byte("\x00\x00\x00\x10ftypmp42\x00\x00\x00\x00video-data")
	pdfBytes  = []byte("%PDF-1.7\npdf-data")
	htmlBytes = []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")
	textBytes = []byte("plain notes, long enough to take a range from")
	zipBytes  = []byte("PK\x03\x04zip-data")
)

func serve(s *Server, method, path string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestArtifactHeadersByType(t *testing.T) {
	cases := []struct {
		file        string
		content     []byte
		contentType string
		sandbox     bool
		disposition string
	}{
		{"shot.png", pngBytes, "image/png", true, "inline"},
		{"clip.mp3", mp3Bytes, "audio/mpeg", true, "inline"},
		{"clip.mp4", mp4Bytes, "video/mp4", true, "inline"},
		{"paper.pdf", pdfBytes, "application/pdf", false, "inline"},
		{"page.html", htmlBytes, "text/plain; charset=utf-8", true, "inline"},
		{"notes.txt", textBytes, "text/plain; charset=utf-8", true, "inline"},
		{"bundle.zip", zipBytes, "application/zip", true, "attachment"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			s, c, p := artifactTestServer(t)
			a := storeArtifact(t, c, p.ID, tc.file, tc.content)

			rec := serve(s, http.MethodGet, artifactURL("ART", a.Name), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			h := rec.Header()
			if got := h.Get("Content-Type"); got != tc.contentType {
				t.Errorf("Content-Type = %q, want %q (stored %q)", got, tc.contentType, a.MIME)
			}
			if h.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("missing nosniff")
			}
			if got := h.Get("Content-Security-Policy") == "sandbox"; got != tc.sandbox {
				t.Errorf("sandbox = %v, want %v", got, tc.sandbox)
			}
			disposition, params, err := mime.ParseMediaType(h.Get("Content-Disposition"))
			if err != nil {
				t.Fatalf("Content-Disposition %q: %v", h.Get("Content-Disposition"), err)
			}
			if disposition != tc.disposition || params["filename"] != a.Name {
				t.Errorf("Content-Disposition = %s %v, want %s filename=%s", disposition, params, tc.disposition, a.Name)
			}
			if rec.Body.String() != string(tc.content) {
				t.Error("body differs from the stored file")
			}
		})
	}
}

// Go's content sniffing never produces image/svg+xml, so an SVG upload is
// stored as text. A row that does carry the type — edited, restored, or from a
// future detector — must still be sandboxed, because SVG can carry script.
func TestAnSVGRowIsSandboxed(t *testing.T) {
	s, c, p := artifactTestServer(t)
	base := storeArtifact(t, c, p.ID, "base.png", pngBytes)
	svgPath := filepath.Join(filepath.Dir(base.Path), "drawing.svg")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if err := os.WriteFile(svgPath, svg, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO artifact (id, project_id, name, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, 'drawing.svg', 'image', 'image/svg+xml', ?, 'h', 1, 1)`,
		core.NewID(), p.ID, len(svg)); err != nil {
		t.Fatal(err)
	}

	rec := serve(s, http.MethodGet, artifactURL("ART", "drawing.svg"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}
	if rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Error("an SVG was served without sandbox")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
}

// Media must seek, and the frontend caps text previews at 256 KB with a Range
// request, so both must answer 206 with the requested bytes.
func TestArtifactRange(t *testing.T) {
	for _, tc := range []struct {
		file        string
		content     []byte
		contentType string
	}{
		{"clip.mp3", mp3Bytes, "audio/mpeg"},
		{"notes.txt", textBytes, "text/plain; charset=utf-8"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			s, c, p := artifactTestServer(t)
			a := storeArtifact(t, c, p.ID, tc.file, tc.content)

			rec := serve(s, http.MethodGet, artifactURL("ART", a.Name), http.Header{"Range": {"bytes=0-4"}})
			if rec.Code != http.StatusPartialContent {
				t.Fatalf("status = %d, want 206", rec.Code)
			}
			if rec.Body.String() != string(tc.content[:5]) {
				t.Errorf("body = %q, want %q", rec.Body.String(), tc.content[:5])
			}
			if want := "bytes 0-4/" + strconv.Itoa(len(tc.content)); rec.Header().Get("Content-Range") != want {
				t.Errorf("Content-Range = %q, want %q", rec.Header().Get("Content-Range"), want)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Errorf("206 Content-Type = %q, want %q", got, tc.contentType)
			}
		})
	}
}

func TestArtifactHead(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "shot.png", pngBytes)
	rec := serve(s, http.MethodHead, artifactURL("ART", a.Name), nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("HEAD = %d, %d bytes, %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
}

func TestArtifactNotFound(t *testing.T) {
	cases := map[string]func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string{
		"unknown project": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			return artifactURL("NOPE", a.Name)
		},
		"unknown name": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			return artifactURL("ART", "nope.png")
		},
		"file gone": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			if err := os.Remove(a.Path); err != nil {
				t.Fatal(err)
			}
			return artifactURL("ART", a.Name)
		},
		// Path is derived from name (TRELLIS-36), not stored, so the only way
		// left to make it resolve outside the artifacts directory is a name
		// that escapes it -- a corrupted or hand-edited row.
		"a name that resolves outside the directory": func(t *testing.T, s *Server, c *core.Core, p core.Project, a core.Artifact) string {
			outside := filepath.Join(filepath.Dir(filepath.Dir(a.Path)), "secret.txt")
			if err := os.WriteFile(outside, []byte("not yours"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE artifact SET name = ? WHERE id = ?`, "../secret.txt", a.ID); err != nil {
				t.Fatal(err)
			}
			return artifactURL("ART", "../secret.txt")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			s, c, p := artifactTestServer(t)
			a := storeArtifact(t, c, p.ID, "base.png", pngBytes)
			rec := serve(s, http.MethodGet, setup(t, s, c, p, a), nil)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "not yours") {
				t.Error("served a file from outside the artifact directory")
			}
		})
	}
}

// The route sits behind protectedHandler like every other /api/ route.
func TestArtifactRouteIsProtected(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "shot.png", pngBytes)
	h := s.protectedHandler("127.0.0.1:0")

	do := func(host, token string) int {
		req := httptest.NewRequest(http.MethodGet, artifactURL("ART", a.Name), nil)
		req.Host = host
		if token != "" {
			req.Header.Set("X-Trellis-Token", token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do("127.0.0.1:0", ""); got != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", got)
	}
	if got := do("127.0.0.1:0", s.token); got != http.StatusOK {
		t.Errorf("with token: %d, want 200", got)
	}
	if got := do("evil.example:0", s.token); got != http.StatusForbidden {
		t.Errorf("foreign host: %d, want 403", got)
	}
}

// A name shared by two artifacts is no longer constructible:
// UNIQUE(project_id, name) (TRELLIS-36) refuses the very insert that used to
// simulate it.

// protectedHandler denies a request whose Origin names a different host, even
// with a valid token, before the token is ever checked.
func TestArtifactRouteRejectsForeignOrigin(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "shot.png", pngBytes)
	h := s.protectedHandler("127.0.0.1:0")

	build := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, artifactURL("ART", a.Name), nil)
		req.Host = "127.0.0.1:0"
		req.Header.Set("X-Trellis-Token", s.token)
		return req
	}

	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, build())
	if ok.Code != http.StatusOK {
		t.Fatalf("setup: without a foreign origin = %d, want 200", ok.Code)
	}

	req := build()
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign origin: %d, want 403", rec.Code)
	}
}

// protectedHandler denies a request Fetch metadata marks as cross-site, even
// with a valid token.
func TestArtifactRouteRejectsCrossSite(t *testing.T) {
	s, c, p := artifactTestServer(t)
	a := storeArtifact(t, c, p.ID, "shot.png", pngBytes)
	h := s.protectedHandler("127.0.0.1:0")

	build := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, artifactURL("ART", a.Name), nil)
		req.Host = "127.0.0.1:0"
		req.Header.Set("X-Trellis-Token", s.token)
		return req
	}

	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, build())
	if ok.Code != http.StatusOK {
		t.Fatalf("setup: without the header = %d, want 200", ok.Code)
	}

	req := build()
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d, want 403", rec.Code)
	}
}

// A name is only a lookup key, but it is echoed into Content-Disposition, so a
// hostile one must not break the header.
func TestAFilenameCannotBreakTheDispositionHeader(t *testing.T) {
	s, c, p := artifactTestServer(t)
	base := storeArtifact(t, c, p.ID, "base.png", pngBytes)
	evil := "evil\".png\r\nX-Injected: yes"
	evilPath := filepath.Join(filepath.Dir(base.Path), evil)
	if err := os.WriteFile(evilPath, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO artifact (id, project_id, name, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1)`,
		core.NewID(), p.ID, evil, base.Kind, base.MIME, base.Size, base.ContentHash); err != nil {
		t.Fatal(err)
	}

	rec := serve(s, http.MethodGet, "/api/p/ART/artifacts/"+url.PathEscape(evil), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	v := rec.Header().Get("Content-Disposition")
	if strings.ContainsAny(v, "\r\n") {
		t.Fatalf("Content-Disposition carries a raw line break: %q", v)
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		t.Fatalf("Content-Disposition %q does not parse: %v", v, err)
	}
	if params["filename"] != evil {
		t.Errorf("filename = %q, want the name round-tripped", params["filename"])
	}
}
