package ui

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// entryItem is an entry as the UI list endpoints return it. Its
// Artifacts field shadows the embedded one; encoding/json/v2 resolves that in
// favour of the shallower field.
type entryItem struct {
	core.Entry
	Artifacts []artifactItem `json:"artifacts,omitempty"`
	// Backlinks are what points at this entry: cards that cite it, and
	// entries that link to it. Only the single-entry route fills them.
	Backlinks []core.Backlink `json:"backlinks,omitempty"`
}

func entryItems(projectKey string, entries []core.Entry) []entryItem {
	out := make([]entryItem, len(entries))
	for i, e := range entries {
		out[i].Entry = e
		for _, a := range e.Artifacts {
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
		s.error(w, http.StatusInternalServerError, "artifact file unreadable")
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

// artifactUploadLimit caps an upload. Artifacts are local files a person
// chooses in their own browser, so the limit is generous; it exists so a
// runaway upload cannot fill the disk through a single request.
const artifactUploadLimit = 64 << 20

// handleProjectArtifacts lists every artifact a project holds, so a card can
// link one that is already stored.
func (s *Server) handleProjectArtifacts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	artifacts, err := s.core.ListArtifacts(ctx, p.ID, "", "")
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactItems(p.Key, artifacts))
}

// artifactItems is a list of stored artifacts with the URL to fetch each one.
func artifactItems(projectKey string, artifacts []core.Artifact) []artifactItem {
	out := make([]artifactItem, 0, len(artifacts))
	for _, a := range artifacts {
		out = append(out, artifactItem{
			ArtifactRef: core.ArtifactRef{Name: a.Name, Kind: a.Kind, MIME: a.MIME, Size: a.Size},
			URL:         artifactURL(projectKey, a.Name),
		})
	}
	return out
}

// handleArtifactUpload stores a file the browser sends and, when the form
// names a card, links it to that card in the same request. The bytes go to a
// temporary file first: core.CreateArtifact takes a path, sniffs the type and
// copies it into the project's artifact directory under a derived name.
//
// This is the one route that takes something other than JSON; see
// protectedHandler, which still requires the session token, a loopback host
// and a same-origin request.
func (s *Server) handleArtifactUpload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		s.error(w, http.StatusBadRequest, "the upload could not be read: "+err.Error())
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("file")
	if err != nil {
		s.error(w, http.StatusBadRequest, "the upload needs a file field")
		return
	}
	defer file.Close()

	name := filepath.Base(filepath.FromSlash(header.Filename))
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" || name == ".." {
		s.error(w, http.StatusBadRequest, "the upload needs a file name")
		return
	}
	dir, err := os.MkdirTemp("", "trellis-upload-")
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(dir)
	staged := filepath.Join(dir, name)
	out, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := io.Copy(out, io.LimitReader(file, artifactUploadLimit)); err != nil {
		out.Close()
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := out.Close(); err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	artifact, err := s.write.CreateArtifact(ctx, p.ID, staged)
	if err != nil {
		s.coreError(w, err)
		return
	}
	// A card given in the form links the new artifact in the same request, so
	// an upload from a card never leaves a stored file linked to nothing.
	if ref := strings.TrimSpace(r.FormValue("card")); ref != "" {
		card, err := s.core.GetCard(ctx, p.ID, core.ParseCardRef(ref))
		if err != nil {
			s.coreError(w, err)
			return
		}
		if err := s.write.LinkArtifactToCard(ctx, p.ID, card.ID, artifact.ID); err != nil {
			s.coreError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, artifactItem{
		ArtifactRef: core.ArtifactRef{Name: artifact.Name, Kind: artifact.Kind, MIME: artifact.MIME, Size: artifact.Size},
		URL:         artifactURL(p.Key, artifact.Name),
	})
}

// handleDeleteArtifact removes an artifact and its file. An entry that names
// it keeps the name as a broken reference, the way a wikilink to a deleted
// entry stays a stub.
func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	artifact, err := s.core.ResolveArtifact(ctx, p.ID, r.PathValue("name"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	if err := s.write.DeleteArtifact(ctx, p.ID, artifact.ID); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type artifactLinkRequest struct {
	Name string `json:"name"`
}

// handleLinkCardArtifact links an artifact the project already holds to a
// card; handleUnlinkCardArtifact unlinks it, leaving the file in place.
func (s *Server) handleLinkCardArtifact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, card, err := s.cardOnBoard(ctx, r)
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in artifactLinkRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Name) == "" {
		s.error(w, http.StatusBadRequest, "the artifact to link is required")
		return
	}
	artifact, err := s.core.ResolveArtifact(ctx, p.ID, in.Name)
	if err != nil {
		s.coreError(w, err)
		return
	}
	if err := s.write.LinkArtifactToCard(ctx, p.ID, card.ID, artifact.ID); err != nil {
		s.coreError(w, err)
		return
	}
	s.writeCardArtifacts(ctx, w, http.StatusCreated, p, card.ID)
}

func (s *Server) handleUnlinkCardArtifact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, card, err := s.cardOnBoard(ctx, r)
	if err != nil {
		s.coreError(w, err)
		return
	}
	artifact, err := s.core.ResolveArtifact(ctx, p.ID, r.PathValue("name"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	if err := s.write.UnlinkArtifactFromCard(ctx, p.ID, card.ID, artifact.ID); err != nil {
		s.coreError(w, err)
		return
	}
	s.writeCardArtifacts(ctx, w, http.StatusOK, p, card.ID)
}

func (s *Server) writeCardArtifacts(ctx context.Context, w http.ResponseWriter, status int, p core.Project, cardID string) {
	artifacts, err := s.core.ListArtifacts(ctx, p.ID, cardID, "")
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, status, artifactItems(p.Key, artifacts))
}
