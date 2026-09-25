package ui

import (
	"context"
	"net/http"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
)

// handlePins lists a project's pinned entries, newest first, with the recap
// each one carries and whether it has gone stale.
func (s *Server) handlePins(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	pins, err := s.core.Pins(ctx, p.ID, "", 0)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pins)
}

type pinRequest struct {
	// Recap is the line a session reads before the body. Trellis never
	// writes one for you: summarising is the caller's to do.
	Recap string `json:"recap"`
	// Board scopes the pin to one board; empty pins it for the project.
	Board string `json:"board"`
}

func (s *Server) handlePinEntry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in pinRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON; nothing changed")
		return
	}
	pin, err := s.writer(r).PinEntry(ctx, p.ID, r.PathValue("slug"), in.Recap, in.Board)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, pin)
}

func (s *Server) handleUnpinEntry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in pinRequest
	// The body is optional: a pin with no board is the common case.
	_ = decodeJSON(w, r, &in)
	if err := s.writer(r).UnpinEntry(ctx, p.ID, r.PathValue("slug"), in.Board); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// vaultHealth is everything a person looks at before deciding to tidy: the
// counts, then what each count is made of. Detection is free and runs on
// demand; every action on it is a person's (§10.10).
type vaultHealth struct {
	Health      []core.HealthLine       `json:"health"`
	Diagnostics []core.Diagnostic       `json:"diagnostics"`
	Duplicates  []core.DuplicateCluster `json:"duplicates"`
	// Cold entries are the ones nothing has read or cited lately.
	Cold []core.Entry `json:"cold"`
}

func (s *Server) handleVaultHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	health, err := s.core.Health(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	diagnostics, err := s.core.Lint(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	duplicates, err := s.core.Duplicates(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	cold, err := s.core.ColdEntries(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	withoutContent(cold)
	writeJSON(w, http.StatusOK, vaultHealth{Health: health, Diagnostics: diagnostics, Duplicates: duplicates, Cold: cold})
}

// handleNominations lists what agents have put forward for the global vault,
// ranked by the evidence behind each one. Nominating is the agent's argument
// and stays on the CLI; deciding is a person's, and this is what they read.
func (s *Server) handleNominations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	nominations, err := s.core.Nominations(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nominations)
}

// lifecycleRequest is what promoting, demoting and verifying ask for. The CLI
// refuses these three to an agent and points at a person instead, so the web
// UI is where they happen; it asks for the same two things the CLI's operator
// has to think about — the slug, retyped, and why.
type lifecycleRequest struct {
	Confirm string `json:"confirm"`
	Reason  string `json:"reason"`
}

// confirmed checks the retyped slug. Comparing the last segment keeps a
// directory out of what has to be typed: `ops/db/rollback` is confirmed by
// typing `rollback`, or the whole slug.
func confirmed(in lifecycleRequest, slug string) bool {
	typed := strings.TrimSpace(in.Confirm)
	if typed == "" {
		return false
	}
	leaf := slug
	if _, last, found := strings.CutLast(slug, "/"); found {
		leaf = last
	}
	return typed == slug || typed == leaf
}

func (s *Server) handlePromoteEntry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	slug := r.PathValue("slug")
	var in lifecycleRequest
	if !decodeJSON(w, r, &in) || !confirmed(in, slug) {
		s.error(w, http.StatusBadRequest, "retype the entry's name to confirm; nothing changed")
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		s.error(w, http.StatusBadRequest, "say why this belongs beyond its project; nothing changed")
		return
	}
	entry, err := s.writer(r).PromoteEntry(ctx, p.ID, slug, in.Reason)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleDemoteEntry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	slug := r.PathValue("slug")
	var in lifecycleRequest
	if !decodeJSON(w, r, &in) || !confirmed(in, slug) {
		s.error(w, http.StatusBadRequest, "retype the entry's name to confirm; nothing changed")
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		s.error(w, http.StatusBadRequest, "say why it does not belong globally; nothing changed")
		return
	}
	entry, err := s.writer(r).DemoteEntry(ctx, slug, in.Reason)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// handleVerifyEntry records that a global entry still holds, which resets its
// verify date. It needs no reason: the act is the statement.
func (s *Server) handleVerifyEntry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	slug := r.PathValue("slug")
	var in lifecycleRequest
	if !decodeJSON(w, r, &in) || !confirmed(in, slug) {
		s.error(w, http.StatusBadRequest, "retype the entry's name to confirm; nothing changed")
		return
	}
	if err := s.writer(r).VerifyEntry(ctx, slug); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUptake reports how often injected entries were opened afterwards, by
// provenance. Read-only: it answers whether writing entries is paying off.
func (s *Server) handleUptake(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	uptake, err := s.core.RecallUptake(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, uptake)
}
