package ui

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/mtch3n/trellis/internal/core"
)

func (s *Server) projectByKey(ctx context.Context, key string) (core.Project, error) {
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, key); err != nil {
		return core.Project{}, core.ErrNotFound("project_not_found", "project not found", "")
	}
	return p, nil
}

// parseDiffRange reads ?from=&to=, both optional. A present-but-invalid value
// is rejected here rather than silently treated as absent.
func parseDiffRange(r *http.Request) (from, to int64, err error) {
	if v := r.URL.Query().Get("from"); v != "" {
		if from, err = strconv.ParseInt(v, 10, 64); err != nil {
			return 0, 0, fmt.Errorf("invalid from: %q", v)
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if to, err = strconv.ParseInt(v, 10, 64); err != nil {
			return 0, 0, fmt.Errorf("invalid to: %q", v)
		}
	}
	return from, to, nil
}

func (s *Server) handleEntryHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	revs, err := s.core.ListEntryRevisions(ctx, p.ID, r.PathValue("slug"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, revs)
}

func (s *Server) handleEntryDiff(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	from, to, err := parseDiffRange(r)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := s.core.DiffEntry(ctx, p.ID, r.PathValue("slug"), from, to)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleCardHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	revs, err := s.core.ListCardRevisions(ctx, p.ID, core.ParseCardRef(r.PathValue("card")))
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, revs)
}

func (s *Server) handleCardDiff(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	from, to, err := parseDiffRange(r)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := s.core.DiffCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")), from, to)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
