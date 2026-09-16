package ui

import (
	"context"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
)

// artifactItem is an entry's artifact as the UI receives it: the core reference
// plus the URL to fetch it from. Core does not know the UI's routes, so the URL
// is added here. A missing artifact has no URL.
type artifactItem struct {
	core.ArtifactRef
	URL string `json:"url,omitempty"`
}

// knowledgeItem is a knowledge entry as the UI list endpoints return it. Its
// Artifacts field shadows the embedded one; encoding/json/v2 resolves that in
// favour of the shallower field.
type knowledgeItem struct {
	core.Knowledge
	Artifacts []artifactItem `json:"artifacts,omitempty"`
}

func knowledgeItems(projectKey string, docs []core.Knowledge) []knowledgeItem {
	out := make([]knowledgeItem, len(docs))
	for i, d := range docs {
		out[i].Knowledge = d
		for _, a := range d.Artifacts {
			item := artifactItem{ArtifactRef: a}
			if !a.Missing {
				item.URL = artifactURL(projectKey, a.Name)
			}
			out[i].Artifacts = append(out[i].Artifacts, item)
		}
	}
	return out
}

func artifactURL(projectKey, name string) string {
	return "/api/p/" + url.PathEscape(projectKey) + "/artifacts/" + url.PathEscape(name)
}

// handleArtifact serves an artifact's bytes for preview. It is registered under
// /api/, so protectedHandler has already checked the host, the origin and the
// session token before this runs.
func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	a, path, err := s.core.ArtifactFile(ctx, p.ID, r.PathValue("name"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.error(w, http.StatusNotFound, "artifact file not found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Headers first: ServeContent sniffs the body only when Content-Type is
	// unset, and sniffing is exactly what must not happen here.
	setArtifactHeaders(w.Header(), a)
	http.ServeContent(w, r, a.Name, st.ModTime(), f)
}

// setArtifactHeaders chooses what a browser may do with an artifact. Artifacts
// are arbitrary user files served from the UI's own origin, and the accepted
// types include HTML and SVG, both of which can carry script:
//
//   - text of any kind, HTML included, is sent as plain text, so it never
//     renders as a page;
//   - every response except PDF is sandboxed, so anything that does render as
//     a document runs in an opaque origin with no reach into the UI. A
//     resource's CSP applies only when it renders as a document, so <img>,
//     <audio> and <video> previews are unaffected;
//   - PDF is not sandboxed, because browsers refuse to start their PDF viewer
//     in a sandboxed document, and they render PDF in an isolated viewer;
//   - archives, and anything unrecognised, are downloads.
func setArtifactHeaders(h http.Header, a core.Artifact) {
	media, _, err := mime.ParseMediaType(a.MIME)
	if err != nil {
		media = ""
	}
	contentType, disposition, sandbox := media, "inline", true
	switch {
	case media == "application/pdf":
		sandbox = false
	case strings.HasPrefix(media, "text/"):
		contentType = "text/plain; charset=utf-8"
	case strings.HasPrefix(media, "image/"),
		strings.HasPrefix(media, "audio/"),
		strings.HasPrefix(media, "video/"):
	case media == "application/zip", media == "application/gzip", media == "application/x-tar":
		disposition = "attachment"
	default:
		contentType, disposition = "application/octet-stream", "attachment"
	}
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	if sandbox {
		h.Set("Content-Security-Policy", "sandbox")
	}
	// FormatMediaType quotes the filename and falls back to RFC 2231 encoding
	// for anything unsafe, so a hostile name cannot break the header. It
	// returns "" only for an invalid disposition token, which ours never is.
	if v := mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}); v != "" {
		h.Set("Content-Disposition", v)
	} else {
		h.Set("Content-Disposition", disposition)
	}
}
