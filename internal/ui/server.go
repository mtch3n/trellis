package ui

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/user"
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
	core *core.Core
	// write is core as the person at the browser. Reads use core; anything a
	// request changes uses this, so the event log names a human.
	write *core.Core
	// actor is who that is, which the browser needs: a card this identity
	// holds is one the person at the browser may edit.
	actor  string
	db     *sqlx.DB
	search *retrieval.Service
	mux    *http.ServeMux
	listen string
	token  string
}

// webActor names whoever is at the browser. The daemon writes as
// daemon:<pid>, which changes at every restart: a lease taken in the UI could
// then never be released, because releasing one requires being its owner. A
// person at this machine is the same principal across restarts. TRELLIS_AGENT
// still wins where it is set, so a scripted UI keeps the identity it was
// given.
func webActor() string {
	if actor := os.Getenv("TRELLIS_AGENT"); actor != "" {
		return actor
	}
	if who, err := user.Current(); err == nil && who.Username != "" {
		return "human:" + who.Username
	}
	return "human:web"
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
	c.SetDropDerived(search.DropProject)
	actor := webActor()
	s := &Server{
		core:   c,
		write:  c.WithActor(actor),
		actor:  actor,
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
	s.mux.HandleFunc("GET /api/me", s.handleMe)
	s.mux.HandleFunc("GET /api/projects", s.handleProjects)
	s.mux.HandleFunc("GET /api/templates", s.handleTemplates)
	s.mux.HandleFunc("DELETE /api/p/{key}", s.handleDeleteProject)
	s.mux.HandleFunc("GET /api/p/{key}/boards", s.handleBoards)
	s.mux.HandleFunc("GET /api/p/{key}/events", s.handleProjectEvents)
	s.mux.HandleFunc("POST /api/p/{key}/boards", s.handleCreateBoard)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/cards", s.handleBoardCards)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/events", s.handleBoardEvents)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards", s.handleCreateCard)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/cards/{card}", s.handleCardDetail)
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}", s.handleCardDetail)
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}/history", s.handleCardHistory)
	s.mux.HandleFunc("GET /api/p/{key}/cards/{card}/diff", s.handleCardDiff)
	s.mux.HandleFunc("PATCH /api/p/{key}/b/{board}/cards/{card}", s.handleUpdateCard)
	s.mux.HandleFunc("DELETE /api/p/{key}/b/{board}/cards/{card}", s.handleDeleteCard)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/move", s.handleMoveCard)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/steal", s.handleStealCard)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/claim", s.handleClaimCard)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/release", s.handleReleaseCard)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/comments", s.handleCreateComment)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/cards/{card}/relations", s.handleCreateCardRelation)
	s.mux.HandleFunc("DELETE /api/p/{key}/b/{board}/cards/{card}/relations/{rel}/{ref}", s.handleDeleteCardRelation)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/knowledge", s.handleKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge", s.handleProjectKnowledgeList)
	s.mux.HandleFunc("GET /api/p/{key}/links/knowledge", s.handleKnowledgeLinks)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge/{slug}/history", s.handleKnowledgeHistory)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge/{slug}/diff", s.handleKnowledgeDiff)
	s.mux.HandleFunc("GET /api/p/{key}/knowledge/{slug}", s.handleGetKnowledge)
	s.mux.HandleFunc("GET /api/p/{key}/artifacts/{name}", s.handleArtifact)
	s.mux.HandleFunc("GET /api/global/knowledge", s.handleGlobalKnowledgeList)
	s.mux.HandleFunc("POST /api/p/{key}/b/{board}/knowledge", s.handleKnowledgeCreate)
	s.mux.HandleFunc("PATCH /api/p/{key}/b/{board}/knowledge/{slug}", s.handleKnowledgeEdit)
	s.mux.HandleFunc("DELETE /api/p/{key}/b/{board}/knowledge/{slug}", s.handleDeleteKnowledge)
	s.mux.HandleFunc("GET /api/p/{key}/b/{board}/graph/{entity}", s.handleGraph)
	s.mux.HandleFunc("GET /api/p/{key}/labels", s.handleLabels)
	s.mux.HandleFunc("POST /api/p/{key}/labels", s.handleCreateLabel)
	s.mux.HandleFunc("DELETE /api/p/{key}/labels/{name}", s.handleDeleteLabel)
	s.mux.HandleFunc("POST /api/p/{key}/labels/merge", s.handleLabelMerge)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/activity", s.handleActivity)
	// SPA fallback
	s.mux.HandleFunc("/", s.handleSPA)
}

// handleMe says which principal this server writes as, so the browser can
// tell a lease it holds from one an agent holds.
func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Actor string `json:"actor"`
	}{s.actor})
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
			boardInfos = append(boardInfos, boardInfo{Name: b.Name, Slug: b.Slug, IsDefault: b.IsDefault})
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
	// The board a project opens on, so the web UI lands where the CLI does.
	IsDefault bool `json:"is_default"`
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

// projectEvent is one entry of a project's history, as the timeline reads
// it. Old and new values are kept only for column moves: those carry column
// names, while a body or title edit would carry the text itself.
type projectEvent struct {
	Seq    int64  `json:"seq"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Action string `json:"action"`
	Field  string `json:"field,omitempty"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new,omitempty"`
}

const (
	projectEventsPage    = 1000
	projectEventsPageMax = 5000
)

// handleProjectEvents returns the events of a project's cards and knowledge,
// oldest first, a page at a time. The event log only shrinks through
// maintenance, so the caller pages forward with ?after=<next> and can poll the
// same way for what is new.
func (s *Server) handleProjectEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}

	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.error(w, http.StatusBadRequest, "after must be an integer")
			return
		}
		after = v
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			s.error(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = v
	}

	events, next, err := s.core.EventFeed(ctx, core.EventQuery{ProjectID: p.ID, After: after, Limit: limit})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Events []core.FeedEvent `json:"events"`
		Next   *int64           `json:"next"`
	}{events, next})
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
	// A deleted entity has no row left to join, so its event falls back to the
	// name it recorded when it went. project narrows the feed to one
	// project's events, which is what an overview asks for; it filters on
	// the event's own project_id rather than a live-row join, so a deleted
	// card or entry, and label and comment events (never joined below), stay
	// in a project-scoped feed instead of only the unfiltered one.
	project := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("project")))
	events := []activityInfo{}
	err := s.db.SelectContext(ctx, &events, `
		SELECT e.seq, e.ts, e.actor, e.entity_type, e.action,
		       COALESCE(e.field, '') AS field,
		       COALESCE(c.title, k.title, b.name, pp.key,
		                CASE WHEN e.action = 'deleted' THEN NULLIF(e.old_value, '') END,
		                e.entity_id) AS title,
		       COALESCE(pc.key, pk.key, pb.key, pp.key, '') AS project_key
		FROM event e
		LEFT JOIN card c ON c.id = e.entity_id AND e.entity_type = 'card'
		LEFT JOIN project pc ON pc.id = c.project_id
		LEFT JOIN knowledge k ON k.id = e.entity_id AND e.entity_type = 'knowledge'
		LEFT JOIN project pk ON pk.id = k.project_id
		LEFT JOIN board b ON b.id = e.entity_id AND e.entity_type = 'board'
		LEFT JOIN project pb ON pb.id = b.project_id
		LEFT JOIN project pp ON pp.id = e.entity_id AND e.entity_type = 'project'
		WHERE ? = '' OR e.project_id = (SELECT id FROM project WHERE key = ?)
		ORDER BY e.seq DESC LIMIT ?`, project, project, limit)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

type deleteProjectRequest struct {
	Confirm string `json:"confirm"`
}

// handleDeleteProject removes a project. The caller must send the key back
// retyped: a delete this large should never be one stray request away.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	key := strings.ToUpper(r.PathValue("key"))
	var in deleteProjectRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON; nothing changed")
		return
	}
	if strings.ToUpper(strings.TrimSpace(in.Confirm)) != key {
		s.error(w, http.StatusBadRequest, "retype the project key to confirm; nothing changed")
		return
	}
	if err := s.write.DeleteProject(ctx, key); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
			Name:      b.Name,
			Slug:      b.Slug,
			IsDefault: b.IsDefault,
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
	b, err := s.write.CreateBoard(ctx, p.ID, in.Name, !in.NoColumns)
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
	// Unix milliseconds, as the single-card endpoint reports them. The
	// overview's timeline places each card on the day it was created.
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
	Labels    []string `json:"labels"`
	Tags      []string `json:"tags"`
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

	_, b, err := s.projectAndBoard(ctx, projectKey, boardSlug)
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

	// Get all cards for this board
	var allCards []struct {
		ID        string        `db:"id"`
		ColumnID  string        `db:"column_id"`
		Ref       string        `db:"ref"`
		Title     string        `db:"title"`
		Body      string        `db:"body_md"`
		Priority  core.Priority `db:"priority"`
		Owner     *string       `db:"owner"`
		Version   int64         `db:"version"`
		CreatedAt int64         `db:"created_at"`
		UpdatedAt int64         `db:"updated_at"`
	}
	if err := s.db.SelectContext(ctx, &allCards,
		`SELECT id, column_id, ref, title, body_md, priority, owner, version, created_at, updated_at FROM card WHERE board_id = ? AND archived_at IS NULL ORDER BY priority, rank`,
		b.ID); err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Collect all card IDs
	cardIDMap := make(map[string]bool)
	for _, c := range allCards {
		cardIDMap[c.ID] = true
	}

	// Fetch labels for all cards in one query
	type cardLabel struct {
		CardID string `db:"card_id"`
		Name   string `db:"name"`
	}
	var labels []cardLabel
	if len(cardIDMap) > 0 {
		cardIDList := make([]string, 0, len(cardIDMap))
		for id := range cardIDMap {
			cardIDList = append(cardIDList, id)
		}
		query, args, err := sqlx.In(
			`SELECT cl.card_id, l.name FROM card_label cl
			 JOIN label l ON cl.label_id = l.id
			 WHERE cl.card_id IN (?)
			 ORDER BY l.name`,
			cardIDList,
		)
		if err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.db.SelectContext(ctx, &labels, s.db.Rebind(query), args...); err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Fetch tags for all cards in one query
	type cardTag struct {
		CardID string `db:"card_id"`
		Name   string `db:"name"`
	}
	var tags []cardTag
	if len(cardIDMap) > 0 {
		cardIDList := make([]string, 0, len(cardIDMap))
		for id := range cardIDMap {
			cardIDList = append(cardIDList, id)
		}
		query, args, err := sqlx.In(
			`SELECT ct.card_id, t.name FROM card_tag ct
			 JOIN tag t ON ct.tag_id = t.id
			 WHERE ct.card_id IN (?)
			 ORDER BY t.name`,
			cardIDList,
		)
		if err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.db.SelectContext(ctx, &tags, s.db.Rebind(query), args...); err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Build maps from card ID to labels/tags
	labelsByCard := make(map[string][]string)
	for _, l := range labels {
		labelsByCard[l.CardID] = append(labelsByCard[l.CardID], l.Name)
	}
	tagsByCard := make(map[string][]string)
	for _, t := range tags {
		tagsByCard[t.CardID] = append(tagsByCard[t.CardID], t.Name)
	}

	// Build result
	var result []columnCardsInfo
	for _, col := range columns {
		var cardInfos []cardInfo
		for _, c := range allCards {
			if c.ColumnID != col.ID {
				continue
			}
			cardLabels := labelsByCard[c.ID]
			if cardLabels == nil {
				cardLabels = []string{}
			}
			cardTags := tagsByCard[c.ID]
			if cardTags == nil {
				cardTags = []string{}
			}
			cardInfos = append(cardInfos, cardInfo{
				ID:        c.ID,
				Ref:       c.Ref,
				Title:     c.Title,
				Body:      c.Body,
				Priority:  c.Priority.String(),
				Owner:     c.Owner,
				Version:   c.Version,
				CreatedAt: c.CreatedAt,
				UpdatedAt: c.UpdatedAt,
				Labels:    cardLabels,
				Tags:      cardTags,
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
	Card      core.Card           `json:"card"`
	Comments  []core.Comment      `json:"comments"`
	Activity  []cardEvent         `json:"activity"`
	Relations []core.CardRelation `json:"relations"`
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
	comments, err := s.core.GetCommentsByCard(ctx, card.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	relations, err := s.core.CardRelations(ctx, card.ID)
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
	writeJSON(w, http.StatusOK, cardDetail{Card: card, Comments: comments, Activity: activity, Relations: relations})
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
type claimRequest struct {
	TTLMinutes *int64 `json:"ttl_minutes"`
}
type commentRequest struct {
	Body string `json:"body"`
}
type knowledgeRequest struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Summary  string `json:"summary"`
	Template string `json:"template"`
	// Private marks the entry from its first write, so a private entry
	// never has a window where its body already reached the embedder under
	// vector search before a follow-up PATCH could mark it private.
	Private bool   `json:"private"`
	Version *int64 `json:"version"`
	// Sources and Set carry what a template may require, so a browser can
	// satisfy a template that rejects an entry without them. Without these
	// the only way to create from such a template would be the CLI.
	Sources []string          `json:"sources"`
	Set     map[string]string `json:"set"`
	// Dir places the entry in a vault directory; empty is the root.
	Dir string `json:"dir"`
}
type labelMergeRequest struct {
	From string `json:"from"`
	Into string `json:"into"`
}

type labelCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
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
	claimed, err := s.write.ClaimCard(ctx, card.ID, 30*60*1000, true, in.Reason)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, claimed)
}

func (s *Server) handleClaimCard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in claimRequest
	decodeJSON(w, r, &in)
	// TTL is optional; defaults to configured lease TTL
	var ttlMS int64 = 0
	if in.TTLMinutes != nil && *in.TTLMinutes > 0 {
		ttlMS = *in.TTLMinutes * 60 * 1000
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
	claimed, err := s.write.ClaimCard(ctx, card.ID, ttlMS, false, "")
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, claimed)
}

func (s *Server) handleReleaseCard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
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
	if err := s.write.ReleaseCard(ctx, card.ID); err != nil {
		s.coreError(w, err)
		return
	}
	// Return the released card
	released, err := s.core.GetCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")))
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, released)
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in commentRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Body) == "" {
		s.error(w, http.StatusBadRequest, "comment body required")
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
	comment, err := s.write.CreateComment(ctx, card.ID, in.Body)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (s *Server) handleCreateCardRelation(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in struct {
		Rel string `json:"rel"`
		Ref string `json:"ref"`
	}
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Rel) == "" || strings.TrimSpace(in.Ref) == "" {
		s.error(w, http.StatusBadRequest, "rel and ref required")
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
	if err := s.write.RelateCards(ctx, p.ID, core.ParseCardRef(card.Ref), in.Rel, core.ParseCardRef(in.Ref)); err != nil {
		s.coreError(w, err)
		return
	}
	// Return the card's relations
	relations, err := s.core.CardRelations(ctx, card.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, relations)
}

func (s *Server) handleDeleteCardRelation(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}

	rel, ref := r.PathValue("rel"), r.PathValue("ref")

	card, err := s.core.GetCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")))
	if err != nil || card.BoardID != b.ID {
		if err != nil {
			s.coreError(w, err)
		} else {
			s.error(w, http.StatusNotFound, "card not found on this board")
		}
		return
	}
	if err := s.write.UnrelateCards(ctx, p.ID, core.ParseCardRef(card.Ref), rel, core.ParseCardRef(ref)); err != nil {
		s.coreError(w, err)
		return
	}
	// Return the card's relations
	relations, err := s.core.CardRelations(ctx, card.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, relations)
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
	withoutContent(docs)
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
	withoutContent(docs)
	writeJSON(w, http.StatusOK, knowledgeItems(p.Key, docs))
}

func (s *Server) handleKnowledgeLinks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	links, err := s.core.KnowledgeLinks(ctx, p.ID)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, links)
}

// handleTemplates lists every knowledge template, built-in and the user's.
// Templates belong to the Trellis home, not to a project.
func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	templates, err := s.core.ListTemplates(ctx)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (s *Server) handleGlobalKnowledgeList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	docs, err := s.core.ListGlobalKnowledge(ctx)
	if err != nil {
		s.coreError(w, err)
		return
	}
	withoutContent(docs)
	// An artifact belongs to a project; the global list spans every project
	// (and the vault, which has none), so it never carries artifacts.
	for i := range docs {
		docs[i].Artifacts = nil
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) handleGetKnowledge(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	doc, err := s.core.ReadKnowledge(ctx, p.ID, r.PathValue("slug"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	// Build the knowledgeItem response with artifacts
	item := knowledgeItem{Knowledge: doc}
	for _, a := range doc.Artifacts {
		artifactItem := artifactItem{ArtifactRef: a}
		if !a.Missing {
			artifactItem.URL = artifactURL(p.Key, a.Name)
		}
		item.Artifacts = append(item.Artifacts, artifactItem)
	}
	writeJSON(w, http.StatusOK, item)
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
	doc, err := s.write.CreateKnowledge(ctx, p.ID, core.NewKnowledge{
		Title: in.Title, Body: in.Body, Summary: in.Summary, Template: in.Template,
		Private: in.Private, Board: b.Name, Sources: in.Sources, Set: in.Set, Dir: in.Dir,
	})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// knowledgePatch is a save from the editor. A field that is absent keeps its
// value; one that is present replaces it.
type knowledgePatch struct {
	Title    *string   `json:"title"`
	Summary  *string   `json:"summary"`
	Body     *string   `json:"body"`
	Template *string   `json:"template"`
	Private  *bool     `json:"private"`
	Tags     *[]string `json:"tags"`
	Labels   *[]string `json:"labels"`
	// Sources and Set let a browser meet the template it switches to in the
	// same save.
	Sources *[]string         `json:"sources"`
	Set     map[string]string `json:"set"`
	Version *int64            `json:"version"`
}

func (s *Server) handleKnowledgeEdit(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, _, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	var in knowledgePatch
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid knowledge JSON")
		return
	}
	doc, err := s.write.EditKnowledgeFields(ctx, p.ID, r.PathValue("slug"), core.KnowledgeEdit{
		Title: in.Title, Summary: in.Summary, Body: in.Body, IfVersion: in.Version,
		Template: in.Template, Private: in.Private, Tags: in.Tags, Labels: in.Labels,
		Sources: in.Sources, Set: in.Set,
	})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleDeleteKnowledge(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, _, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	slug := r.PathValue("slug")
	if err := s.write.DeleteKnowledge(ctx, p.ID, slug); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	var in labelCreateRequest
	if !decodeJSON(w, r, &in) || in.Name == "" {
		s.error(w, http.StatusBadRequest, "label name required")
		return
	}
	label, err := s.write.CreateLabel(ctx, p.ID, in.Name, in.Description)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, label)
}

func (s *Server) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var p core.Project
	if err := s.db.GetContext(ctx, &p, `SELECT * FROM project WHERE key = ?`, r.PathValue("key")); err != nil {
		s.error(w, http.StatusNotFound, "project not found")
		return
	}
	name := r.PathValue("name")
	if err := s.write.DeleteLabel(ctx, p.ID, name); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	if err := s.write.MergeLabel(ctx, p.ID, in.From, in.Into); err != nil {
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
	card, err := s.write.CreateCard(ctx, p.ID, b.ID, core.NewCard{
		Title: in.Title, Body: in.Body, Column: in.Column, Priority: in.Priority,
		Labels: in.Labels, Tags: in.Tags,
	})
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

// handleDeleteCard removes a card outright, the way `trellis card rm` does.
// A card an agent holds right now is refused by core.
func (s *Server) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, b, err := s.projectAndBoard(ctx, r.PathValue("key"), r.PathValue("board"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	ref := core.ParseCardRef(r.PathValue("card"))
	card, err := s.core.GetCard(ctx, p.ID, ref)
	if err != nil {
		s.coreError(w, err)
		return
	}
	if card.BoardID != b.ID {
		s.error(w, http.StatusNotFound, "card not found on this board")
		return
	}
	if err := s.write.DeleteCard(ctx, p.ID, ref); err != nil {
		s.coreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	card, err := s.write.EditCard(ctx, p.ID, core.ParseCardRef(r.PathValue("card")), core.CardEdit{
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
	card, err := s.write.MoveCardBefore(ctx, p.ID, b.ID, core.ParseCardRef(r.PathValue("card")), in.Column, in.Before)
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

// withoutContent removes body and (for private entries) summary/recap from knowledge
// entries, matching the CLI's withholdContent behavior for listings.
func withoutContent(docs []core.Knowledge) {
	for i := range docs {
		docs[i].BodyMD = ""
		if docs[i].Private {
			docs[i].Summary = ""
			docs[i].Recap = nil
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, value)
}

// coreError answers with a core error's message, code and problems. The
// error's fix is a CLI command, which means nothing to a browser user, so it
// is left out.
func (s *Server) coreError(w http.ResponseWriter, err error) {
	e, ok := errors.AsType[*core.Error](err)
	if !ok {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := http.StatusInternalServerError
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
	body := map[string]any{"error": e.Msg, "code": e.Code}
	if len(e.Problems) > 0 {
		body["problems"] = e.Problems
	}
	writeJSON(w, status, body)
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
