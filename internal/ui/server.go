package ui

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/retrieval"
	frontend "github.com/mtch3n/trellis/web"
)

// Server serves the trellis UI API and embedded frontend.
type Server struct {
	core   *core.Core
	db     *sqlx.DB
	search *retrieval.Service
	mux    *http.ServeMux
	listen string
	token  string
}

// NewServer creates a new UI server.
func NewServer(c *core.Core, db *sqlx.DB, listen string) *Server {
	dbPath, _ := home.DBPath()
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Defaults()
	}
	return NewServerWithSearch(c, db, listen, retrieval.NewService(c, db, dbPath, cfg))
}

// NewServerWithSearch lets the daemon give HTTP and IPC the same long-lived
// retrieval service and provider lifecycle.
func NewServerWithSearch(c *core.Core, db *sqlx.DB, listen string, search *retrieval.Service) *Server {
	c.SetKnowledgeChanged(search.ReconcileProject)
	s := &Server{
		core:   c,
		db:     db,
		listen: listen,
		token:  rand.Text(),
		search: search,
		mux:    http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// API routes
	s.mux.HandleFunc("GET /api/projects", s.handleProjects)
	s.mux.HandleFunc("GET /api/p/{key}/boards", s.handleBoards)
	s.mux.HandleFunc("POST /api/p/{key}/boards", s.handleCreateBoard)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/cards", s.handleBoardCards)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/events", s.handleBoardEvents)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards", s.handleCreateCard)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/cards/{card}", s.handleCardDetail)
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}", s.handleCardDetail)
	s.mux.HandleFunc("PATCH /api/p/{key}/b/{board}/cards/{card}", s.handleUpdateCard)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/move", s.handleMoveCard)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/steal", s.handleStealCard)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/knowledge", s.handleKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge", s.handleProjectKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/artifacts/{name}", s.handleArtifact)
	s.mux.HandleFunc("GET /api/global/knowledge", s.handleGlobalKnowledgeList)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/knowledge", s.handleKnowledgeCreate)
	s.mux.HandleFunc("PATCH /api/p/{key}/b/{board}/knowledge/{slug}", s.handleKnowledgeEdit)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/graph/{entity}", s.handleGraph)
	s.mux.HandleFunc("GET /api/p/{key}/labels", s.handleLabels)
	s.mux.HandleFunc("POST /api/p/{key}/labels/merge", s.handleLabelMerge)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/activity", s.handleActivity)
	// SPA fallback
	s.mux.HandleFunc("/", s.handleSPA)
}

// projectInfo contains project info for the landing page.
type projectInfo struct {
	Key           string       `json:"key"`
	Name          string       `json:"name"`
	BoardCount    int          `json:"board_count"`
	InProgress    int          `json:"in_progress"`
	StaleLeases   int          `json:"stale_leases"`
	RecentChanges int          `json:"recent_changes"`
	Boards        []boardInfo  `json:"boards"`
	Columns       []columnInfo `json:"columns"`
}

type columnInfo struct {
	Name      string `json:"name"`
	CardCount int    `json:"card_count"`
	IsDone    bool   `json:"is_done"`
}

// handleProjects returns all projects with per-column counts.
func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	// Get all projects
	var projects []core.Project
	if err := s.db.SelectContext(ctx, &projects, `SELECT * FROM project ORDER BY created_at DESC`); err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var result []projectInfo
	for _, p := range projects {
		// Get boards for this project
		boards, err := s.core.ListBoards(ctx, p.ID)
		if err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Aggregate card counts per column across all boards
		columnCounts := make(map[string]int)
		columnDone := make(map[string]bool)
		for _, b := range boards {
			columns, err := s.core.ListColumns(ctx, b.ID)
			if err != nil {
				s.error(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, col := range columns {
				columnDone[col.Name] = col.IsDone
				var count int
				if err := s.db.GetContext(ctx, &count,
					`SELECT COUNT(*) FROM card WHERE column_id = ? AND archived_at IS NULL`,
					col.ID); err != nil {
					s.error(w, http.StatusInternalServerError, err.Error())
					return
				}
				columnCounts[col.Name] += count
			}
		}

		// Convert to column info
		var cols []columnInfo
		for name, count := range columnCounts {
			cols = append(cols, columnInfo{
				Name:      name,
				CardCount: count,
				IsDone:    columnDone[name],
			})
		}
		var inProgress, staleLeases, recentChanges int
		if err := s.db.GetContext(ctx, &inProgress, `
			SELECT COUNT(*) FROM card c JOIN column_ col ON col.id = c.column_id
			WHERE c.project_id = ? AND c.archived_at IS NULL AND col.is_done = 0`, p.ID); err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.db.GetContext(ctx, &staleLeases, `
			SELECT COUNT(*) FROM card
			WHERE project_id = ? AND owner IS NOT NULL AND (lease_until IS NULL OR lease_until < ?)`, p.ID, time.Now().UnixMilli()); err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.db.GetContext(ctx, &recentChanges, `
			SELECT COUNT(*) FROM event e
			WHERE e.ts > ? AND (
				EXISTS (SELECT 1 FROM card c WHERE c.id = e.entity_id AND c.project_id = ?) OR
				EXISTS (SELECT 1 FROM knowledge k WHERE k.id = e.entity_id AND k.project_id = ?))`,
			time.Now().Add(-24*time.Hour).UnixMilli(), p.ID, p.ID); err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}

		boardInfos := make([]boardInfo, 0, len(boards))
		for _, b := range boards {
			boardInfos = append(boardInfos, boardInfo{Name: b.Name, Slug: b.Slug})
		}
		result = append(result, projectInfo{
			Key:           p.Key,
			Name:          p.Name,
			BoardCount:    len(boards),
			InProgress:    inProgress,
			StaleLeases:   staleLeases,
			RecentChanges: recentChanges,
			Boards:        boardInfos,
			Columns:       cols,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(result)
	w.Write(b)
}

// boardInfo contains board summary info.
type boardInfo struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type activityInfo struct {
	Seq        int64  `db:"seq" json:"seq"`
	Timestamp  int64  `db:"ts" json:"timestamp"`
	Actor      string `db:"actor" json:"actor"`
	EntityType string `db:"entity_type" json:"entity_type"`
	Action     string `db:"action" json:"action"`
	Field      string `db:"field" json:"field,omitempty"`
	Title      string `db:"title" json:"title"`
	ProjectKey string `db:"project_key" json:"project"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, []core.SearchHit{})
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	hits, err := s.search.Search(ctx, "", query, core.SearchOpts{Limit: limit, AllProjects: true, Label: r.URL.Query().Get("label")})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	var events []activityInfo
	err := s.db.SelectContext(ctx, &events, `
		SELECT e.seq, e.ts, e.actor, e.entity_type, e.action,
		       COALESCE(e.field, '') AS field,
		       COALESCE(c.title, k.title, e.entity_id) AS title,
		       COALESCE(pc.key, pk.key, '') AS project_key
		FROM event e
		LEFT JOIN card c ON c.id = e.entity_id AND e.entity_type = 'card'
		LEFT JOIN project pc ON pc.id = c.project_id
		LEFT JOIN knowledge k ON k.id = e.entity_id AND e.entity_type = 'knowledge'
		LEFT JOIN project pk ON pk.id = k.project_id
		ORDER BY e.seq DESC LIMIT ?`, limit)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// handleBoards returns boards in a project.
func (s *Server) handleBoards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	projectKey := r.PathValue("key")

	// Get project by key
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, projectKey); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}

	// Get boards for this project
	boards, err := s.core.ListBoards(ctx, p.ID)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var result []boardInfo
	for _, b := range boards {
		result = append(result, boardInfo{
			Name: b.Name,
			Slug: b.Slug,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(result)
	w.Write(b)
}

type boardRequest struct {
	Name      string `json:"name"`
	NoColumns bool   `json:"no_columns"`
}

func (s *Server) projectAndBoard(ctx context.Context, key, slug string) (core.Project, core.Board, error) {
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, key); err != nil {
		return core.Project{}, core.Board{}, core.ErrNotFound("project_not_found", "project not found", "")
	}
	var b core.Board
	if err := s.db.GetContext(ctx, &b, `SELECT * FROM board WHERE project_id = ? AND slug = ?`, p.ID, slug); err != nil {
		return core.Project{}, core.Board{}, core.ErrNotFound("board_not_found", "board not found", "")
	}
	return p, b, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.UnmarshalRead(r.Body, dst); err != nil {
		return false
	}
	return true
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	key := r.PathValue("key")
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, key); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	var in boardRequest
	if !decodeJSON(w, r, &in) || in.Name == "" {
		s.error(w, http.StatusBadRequest, "board name required")
		return
	}
	b, err := s.core.CreateBoard(ctx, p.ID, in.Name, !in.NoColumns)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

// cardInfo contains card info for board view.
type cardInfo struct {
	ID       string  `json:"id"`
	Ref      string  `json:"ref"`
	Title    string  `json:"title"`
	Body     string  `json:"body"`
	Priority string  `json:"priority"`
	Version  int64   `json:"version"`
	Owner    *string `json:"owner,omitempty"`
}

// columnCardsInfo contains cards grouped by column.
type columnCardsInfo struct {
	Name  string     `json:"name"`
	Cards []cardInfo `json:"cards"`
}

// handleBoardCards returns cards on a board, grouped by column.
func (s *Server) handleBoardCards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	projectKey := r.PathValue("key")
	boardSlug := r.PathValue("board")

	p, b, err := s.projectAndBoard(ctx, projectKey, boardSlug)
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}

	// Get columns for this board
	columns, err := s.core.ListColumns(ctx, b.ID)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var result []columnCardsInfo
	for _, col := range columns {
		// Get cards in this column
		var cards []struct {
			ID       string        `db:"id"`
			Seq      int64         `db:"seq"`
			Title    string        `db:"title"`
			Body     string        `db:"body_md"`
			Priority core.Priority `db:"priority"`
			Owner    *string       `db:"owner"`
			Version  int64         `db:"version"`
		}
		if err := s.db.SelectContext(ctx, &cards,
			`SELECT id, seq, title, body_md, priority, owner, version FROM card WHERE column_id = ? AND archived_at IS NULL ORDER BY rank`,
			col.ID); err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}

		var cardInfos []cardInfo
		for _, c := range cards {
			cardInfos = append(cardInfos, cardInfo{
				ID:       c.ID,
				Ref:      p.Key + "-" + fmt.Sprintf("%d", c.Seq),
				Title:    c.Title,
				Body:     c.Body,
				Priority: c.Priority.String(),
				Owner:    c.Owner,
				Version:  c.Version,
			})
		}

		result = append(result, columnCardsInfo{
			Name:  col.Name,
			Cards: cardInfos,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(result)
	w.Write(data)
}

// handleBoardEvents provides a lightweight change signal. The event log is
// append-only, so clients only need to refetch the board when its watermark
// changes; the board API remains the single source of truth for the payload.
func (s *Server) handleBoardEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.error(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	last := int64(0)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var watermark int64
		if err := s.db.GetContext(r.Context(), &watermark, `
			SELECT COALESCE(MAX(e.seq), 0) FROM event e
			WHERE EXISTS (
				SELECT 1 FROM card c
				WHERE c.id = e.entity_id AND c.project_id = ? AND c.board_id = ?
			) OR EXISTS (
				SELECT 1 FROM knowledge k
				WHERE k.id = e.entity_id AND k.project_id = ? AND k.board_id = ?
			)`, p.ID, b.ID, p.ID, b.ID); err != nil {
			return
		}
		if watermark != last {
			last = watermark
			fmt.Fprintf(w, "event: changed\ndata: %d\n\n", watermark)
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

type cardRequest struct {
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Column   string         `json:"column"`
	Priority *core.Priority `json:"priority"`
	Labels   []string       `json:"labels"`
	Tags     []string       `json:"tags"`
}

type cardEvent struct {
	Seq       int64  `db:"seq" json:"seq"`
	Timestamp int64  `db:"ts" json:"timestamp"`
	Actor     string `db:"actor" json:"actor"`
	Action    string `db:"action" json:"action"`
	Field     string `db:"field" json:"field,omitempty"`
	OldValue  string `db:"old_value" json:"old_value,omitempty"`
	NewValue  string `db:"new_value" json:"new_value,omitempty"`
}

type cardDetail struct {
	Card     core.Card   `json:"card"`
	Notes    []core.Note `json:"notes"`
	Activity []cardEvent `json:"activity"`
}

func (s *Server) handleCardDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	var b core.Board
	var err error
	if r.PathValue("board") != "" {
		p, b, err = s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	} else if err = s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err == nil {
		// The project-scoped route resolves the card first; it is useful for
		// deep links where the caller does not know the board slug.
		b = core.Board{ID: "*"}
	}
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	card, err := s.core.GetCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")))
	if err != nil || (b.ID != "*" && card.BoardID != b.ID) {
		if err != nil {
			s.coreError(w, err)
		} else {
			s.error(w, http.StatusNotFound, "card not found on this board")
		}
		return
	}
	notes, err := s.core.GetNotesByCard(ctx, card.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	activity := []cardEvent{}
	if err := s.db.SelectContext(ctx, &activity, `
		SELECT seq, ts, actor, action, COALESCE(field, '') AS field,
		       COALESCE(old_value, '') AS old_value, COALESCE(new_value, '') AS new_value
		FROM event WHERE entity_type = 'card' AND entity_id = ? ORDER BY seq DESC LIMIT 100`, card.ID); err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cardDetail{Card: card, Notes: notes, Activity: activity})
}

type cardPatch struct {
	Title        *string        `json:"title"`
	Body         *string        `json:"body"`
	Priority     *core.Priority `json:"priority"`
	IfVersion    *int64         `json:"if_version"`
	AddLabels    []string       `json:"add_labels"`
	RemoveLabels []string       `json:"remove_labels"`
	AddTags      []string       `json:"add_tags"`
	RemoveTags   []string       `json:"remove_tags"`
}

type moveRequest struct {
	Column string `json:"column"`
	Before string `json:"before"`
}

type stealRequest struct {
	Reason string `json:"reason"`
}
type knowledgeRequest struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Summary  string `json:"summary"`
	Template string `json:"template"`
	Version  *int64 `json:"version"`
}
type labelMergeRequest struct {
	From string `json:"from"`
	Into string `json:"into"`
}

func (s *Server) handleStealCard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in stealRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Reason) == "" {
		s.error(w, http.StatusBadRequest, "steal reason required")
		return
	}
	card, err := s.core.GetCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")))
	if err != nil || card.BoardID != b.ID {
		if err != nil {
			s.coreError(w, err)
		} else {
			s.error(w, http.StatusNotFound, "card not found on this board")
		}
		return
	}
	claimed, err := s.core.ClaimCard(ctx, card.ID, 30*60*1000, true, in.Reason)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, claimed)
}

func (s *Server) handleKnowledgeList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	docs, err := s.core.ListKnowledge(ctx, p.ID, core.KnowledgeFilter{BoardID: b.ID})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, knowledgeItems(p.Key, docs))
}

func (s *Server) handleProjectKnowledgeList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	docs, err := s.core.ListKnowledge(ctx, p.ID, core.KnowledgeFilter{})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, knowledgeItems(p.Key, docs))
}

func (s *Server) handleGlobalKnowledgeList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var docs []core.Knowledge
	if err := s.db.SelectContext(ctx, &docs, `SELECT * FROM knowledge WHERE global = 1 ORDER BY updated_at DESC`); err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) handleKnowledgeCreate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in knowledgeRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Title) == "" {
		s.error(w, http.StatusBadRequest, "knowledge title required")
		return
	}
	doc, err := s.core.CreateKnowledge(ctx, p.ID, core.NewKnowledge{Title: in.Title, Body: in.Body, Summary: in.Summary, Template: in.Template, Board: b.Name})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) handleKnowledgeEdit(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, _, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in knowledgeRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid knowledge JSON")
		return
	}
	doc, err := s.core.EditKnowledge(ctx, p.ID, r.PathValue("slug"), in.Body, in.Version)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, _, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	entity := r.PathValue("entity")
	startID := ""
	if doc, docErr := s.core.LoadKnowledge(ctx, p.ID, entity); docErr == nil {
		startID = doc.ID
	} else if card, cardErr := s.core.GetCard(ctx, p.ID, core.ParseCardRef(entity)); cardErr == nil {
		startID = card.ID
	} else {
		s.error(w, http.StatusNotFound, "graph entity not found")
		return
	}
	depth := 2
	if raw := r.URL.Query().Get("depth"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
			depth = parsed
		}
	}
	graph, err := s.core.Traverse(ctx, startID, depth, nil, false)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) handleLabels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	labels, err := s.core.ListLabels(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) handleLabelMerge(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	var in labelMergeRequest
	if !decodeJSON(w, r, &in) || in.From == "" || in.Into == "" {
		s.error(w, http.StatusBadRequest, "from and into labels required")
		return
	}
	if err := s.core.MergeLabel(ctx, p.ID, in.From, in.Into); err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"merged": in.From, "into": in.Into})
}

func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in cardRequest
	if !decodeJSON(w, r, &in) || in.Title == "" {
		s.error(w, http.StatusBadRequest, "card title required")
		return
	}
	card, err := s.core.CreateCard(ctx, p.ID, b.ID, core.NewCard{
		Title: in.Title, Body: in.Body, Column: in.Column, Priority: in.Priority,
		Labels: in.Labels, Tags: in.Tags,
	})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

func (s *Server) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, _, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in cardPatch
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid card JSON")
		return
	}
	card, err := s.core.EditCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")), core.CardEdit{
		Title: in.Title, Body: in.Body, Priority: in.Priority, IfVersion: in.IfVersion,
		AddLabels: in.AddLabels, RemoveLabels: in.RemoveLabels,
		AddTags: in.AddTags, RemoveTags: in.RemoveTags,
	})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in moveRequest
	if !decodeJSON(w, r, &in) || in.Column == "" {
		s.error(w, http.StatusBadRequest, "column required")
		return
	}
	card, err := s.core.MoveCardBefore(ctx, p.ID, b.ID, core.ParseCardRef(r.PathValue("card")), in.Column, in.Before)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

// handleSPA serves the React app as an SPA fallback.
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	dist, err := fs.Sub(frontend.DistFS, "dist")
	if err != nil {
		s.error(w, http.StatusInternalServerError, "frontend bundle unavailable")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path != "" {
		if _, err := fs.Stat(dist, path); err == nil {
			http.FileServer(http.FS(dist)).ServeHTTP(w, r)
			return
		}
	}

	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		s.error(w, http.StatusInternalServerError, "frontend index unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}

// error writes a JSON error response.
func (s *Server) error(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(map[string]string{"error": msg})
	w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, value)
}

func (s *Server) coreError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if e, ok := err.(*core.Error); ok {
		switch e.Exit {
		case 2:
			status = http.StatusBadRequest
		case 3:
			status = http.StatusNotFound
		case 4:
			status = http.StatusConflict
		case 5:
			status = http.StatusForbidden
		}
	}
	s.error(w, status, err.Error())
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	listener, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	return s.Serve(listener)
}

// Serve lets the daemon own the listener and its lifecycle while keeping the
// UI server reusable in tests and by future Unix-socket transports.
func (s *Server) Serve(listener net.Listener) error {
	return s.ServeContext(context.Background(), listener)
}

const requestTimeout = 10 * time.Second
