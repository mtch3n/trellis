package ui

import (
	"context"
	"net/http"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
)

// cardOnBoard resolves the project, the board and the card of a
// /api/p/{key}/b/{board}/cards/{card} request. Every card route needs the same
// three lookups and the same two refusals, so they live here once: a project or
// board that does not exist, and a card that exists on another board, are both
// "not found" to the caller.
func (s *Server) cardOnBoard(ctx context.Context, r *http.Request) (core.Project, core.Card, error) {
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		return p, core.Card{}, err
	}
	card, err := s.core.GetCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")))
	if err != nil {
		return p, core.Card{}, err
	}
	if card.BoardID != b.ID {
		return p, core.Card{}, core.ErrNotFound("card_not_found", "card not found on this board", "")
	}
	return p, card, nil
}

// handleArchiveCard takes a card off the board without deleting it, and
// handleRestoreCard puts it back. Archiving releases any claim, so the card
// comes back as unclaimed work.
func (s *Server) handleArchiveCard(w http.ResponseWriter, r *http.Request) {
	s.archiveOrRestore(w, r, true)
}

func (s *Server) handleRestoreCard(w http.ResponseWriter, r *http.Request) {
	s.archiveOrRestore(w, r, false)
}

func (s *Server) archiveOrRestore(w http.ResponseWriter, r *http.Request, archive bool) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, card, err := s.cardOnBoard(ctx, r)
	if err != nil {
		s.coreError(w, err)
		return
	}
	ref := core.ParseCardRef(card.Ref)
	changed := s.write.RestoreCard
	if archive {
		changed = s.write.ArchiveCard
	}
	updated, err := changed(ctx, p.ID, ref)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

type cardLinkRequest struct {
	// Target is an entry: a slug, a slug with an anchor, or an address that
	// may name another project, exactly as `trellis link` takes it.
	Target string `json:"target"`
}

// handleLinkCardToEntry records that a card cites an entry, and
// handleUnlinkCardFromEntry takes that back. Both answer with the card's links
// so the browser never has to guess what the list became.
func (s *Server) handleLinkCardToEntry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, card, err := s.cardOnBoard(ctx, r)
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in cardLinkRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Target) == "" {
		s.error(w, http.StatusBadRequest, "an entry to link to is required")
		return
	}
	if err := s.write.LinkCardToEntry(ctx, p.ID, core.ParseCardRef(card.Ref), in.Target); err != nil {
		s.coreError(w, err)
		return
	}
	s.writeCardLinks(w, ctx, http.StatusCreated, card.ID)
}

func (s *Server) handleUnlinkCardFromEntry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, card, err := s.cardOnBoard(ctx, r)
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in cardLinkRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Target) == "" {
		s.error(w, http.StatusBadRequest, "the entry to unlink is required")
		return
	}
	if err := s.write.UnlinkCardFromEntry(ctx, p.ID, core.ParseCardRef(card.Ref), in.Target); err != nil {
		s.coreError(w, err)
		return
	}
	s.writeCardLinks(w, ctx, http.StatusOK, card.ID)
}

func (s *Server) writeCardLinks(w http.ResponseWriter, ctx context.Context, status int, cardID string) {
	links, err := s.core.CardLinks(ctx, cardID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, status, links)
}

// handleImportCards writes a whole plan at once, the way `card import` does:
// one transaction, so a refusal halfway down leaves the board untouched.
func (s *Server) handleImportCards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in []core.ImportCard
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "the import must be a JSON array of cards; nothing changed")
		return
	}
	cards, err := s.write.ImportCards(ctx, p.ID, b.ID, in)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, cards)
}
