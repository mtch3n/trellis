package ui

import (
	"context"
	"net/http"
)

// handleActivity answers who acted on a project in the last 30 days.
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	activity, err := s.core.Activity(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, activity)
}
