package ui

import (
	"context"
	"net/http"
	"strings"
)

// boardPatch renames a board, or makes it the one the project opens on. Both
// in one request, because a board settings form has both on screen.
type boardPatch struct {
	Name      *string `json:"name"`
	IsDefault *bool   `json:"is_default"`
}

func (s *Server) handleUpdateBoard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in boardPatch
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON; nothing changed")
		return
	}
	if in.Name == nil && in.IsDefault == nil {
		s.error(w, http.StatusBadRequest, "name what to change: name or is_default")
		return
	}
	board := b
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			s.error(w, http.StatusBadRequest, "a board needs a name")
			return
		}
		board, err = s.write.RenameBoard(ctx, p.ID, board.Name, *in.Name)
		if err != nil {
			s.coreError(w, err)
			return
		}
	}
	// Only making a board the default means anything; a project always has
	// one, so unsetting it would leave none.
	if in.IsDefault != nil && *in.IsDefault {
		board, err = s.write.SetDefaultBoard(ctx, p.ID, board.Name)
		if err != nil {
			s.coreError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, boardInfo{Name: board.Name, Slug: board.Slug, IsDefault: board.IsDefault})
}

// handleDeleteBoard removes a board. A board with cards on it needs
// ?force=1, and the last board of a project is refused: a project always has
// somewhere to put a card.
func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	if err := s.write.DeleteBoard(ctx, p.ID, b.Name, truthy(r.URL.Query().Get("force"))); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleColumns lists a board's columns in their order, with which of them
// counts as done. The board's cards route carries the names; this carries
// what a person needs to change them.
func (s *Server) handleColumns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	_, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	columns, err := s.core.ListColumns(ctx, b.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, columns)
}

type columnRequest struct {
	Name string `json:"name"`
	// After places the column behind that one; empty puts it last.
	After string `json:"after"`
	// Done marks it terminal. It is a flag, never a name match: a terminal
	// column may be called anything.
	Done bool `json:"done"`
}

func (s *Server) handleCreateColumn(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	_, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in columnRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Name) == "" {
		s.error(w, http.StatusBadRequest, "a column needs a name")
		return
	}
	column, err := s.write.AddColumn(ctx, b.ID, in.Name, in.After, in.Done)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, column)
}

// columnPatch renames a column or moves it along the board. Reordering is
// "after this one", as the CLI takes it, so the caller never sends positions.
type columnPatch struct {
	Name *string `json:"name"`
	// After names the column this one now follows; "" means first.
	After *string `json:"after"`
}

func (s *Server) handleUpdateColumn(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	_, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	var in columnPatch
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON; nothing changed")
		return
	}
	name := r.PathValue("column")
	if in.After != nil {
		if err := s.write.MoveColumn(ctx, b.ID, name, *in.After); err != nil {
			s.coreError(w, err)
			return
		}
	}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			s.error(w, http.StatusBadRequest, "a column needs a name")
			return
		}
		if err := s.write.RenameColumn(ctx, b.ID, name, *in.Name); err != nil {
			s.coreError(w, err)
			return
		}
	}
	if in.Name == nil && in.After == nil {
		s.error(w, http.StatusBadRequest, "name what to change: name or after")
		return
	}
	columns, err := s.core.ListColumns(ctx, b.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, columns)
}

// handleDeleteColumn removes a column. One with cards in it is refused unless
// the request says where those cards go, because the cards are the work.
func (s *Server) handleDeleteColumn(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	_, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	if err := s.write.DeleteColumn(ctx, b.ID, r.PathValue("column"), r.URL.Query().Get("move_cards_to")); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLabel answers with one label, so the labels view can read a
// description back after a write without listing everything again.
func (s *Server) handleLabel(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	label, err := s.core.GetLabel(ctx, p.ID, r.PathValue("name"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, label)
}
