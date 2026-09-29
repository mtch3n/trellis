package ui

import (
	"context"
	"net/http"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
)

// trashItem is one trashed thing as the web UI lists it: what it was, when
// it went, and when the purge takes it.
type trashItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	TrashedAt int64  `json:"trashed_at"`
	TrashedBy string `json:"trashed_by"`
	PurgeAt   int64  `json:"purge_at"`
}

func (s *Server) trashItems(items []core.TrashItem) []trashItem {
	retention := s.core.TrashRetention().Milliseconds()
	out := make([]trashItem, 0, len(items))
	for _, it := range items {
		out = append(out, trashItem{ID: it.ID, Kind: it.Kind, Name: it.Name, Title: it.Title,
			TrashedAt: it.TrashedAt, TrashedBy: it.TrashedBy, PurgeAt: it.TrashedAt + retention})
	}
	return out
}

// handleProjectTrash lists a project's trash, newest first.
func (s *Server) handleProjectTrash(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	items, err := s.core.Trash(ctx, p.ID, "")
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.trashItems(items))
}

// handleRestoreTrash restores one item from a project's trash.
func (s *Server) handleRestoreTrash(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	if _, err := s.writer(r).RestoreTrashItem(ctx, p.ID, r.PathValue("id")); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTrashedProjects lists the projects in the trash.
func (s *Server) handleTrashedProjects(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	projects, err := s.core.TrashedProjects(ctx)
	if err != nil {
		s.coreError(w, err)
		return
	}
	retention := s.core.TrashRetention().Milliseconds()
	out := make([]trashItem, 0, len(projects))
	for _, p := range projects {
		out = append(out, trashItem{Kind: core.TrashProject, Name: p.Key, Title: p.Description,
			TrashedAt: *p.TrashedAt, PurgeAt: *p.TrashedAt + retention})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRestoreProject restores a trashed project.
func (s *Server) handleRestoreProject(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	if _, err := s.writer(r).RestoreProject(ctx, strings.ToUpper(r.PathValue("key"))); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
