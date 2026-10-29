package server

import (
	"context"
	"crypto/subtle"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/mtch3n/trellis/internal/platform"
)

const headerCSRF = "X-Trellis-CSRF"

type webAPI struct {
	p *platform.Platform
}

type webCall struct {
	w      http.ResponseWriter
	r      *http.Request
	s      platform.WebSession
	secret string
}

func (c webCall) pr() platform.Principal { return c.s.Principal() }

func (c webCall) key() string { return c.r.PathValue("key") }

type apiError struct {
	Code    string `json:"code"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
	Detail  any    `json:"detail,omitempty"`
}

func registerWeb(mux *http.ServeMux, p *platform.Platform) {
	a := &webAPI{p: p}
	mux.HandleFunc("POST /login", a.login)
	routes := []struct {
		pattern string
		status  int
		h       func(webCall) (any, error)
	}{
		{"GET /api/me", http.StatusOK, a.me},
		{"POST /api/logout", http.StatusOK, a.logout},
		{"GET /api/projects", http.StatusOK, a.projects},
		{"GET /api/p/{key}/boards", http.StatusOK, a.boards},
		{"POST /api/p/{key}/boards", http.StatusCreated, a.newBoard},
		{"GET /api/p/{key}/b/{board}/cards", http.StatusOK, a.cards},
		{"POST /api/p/{key}/b/{board}/cards", http.StatusCreated, a.newCard},
		{"GET /api/p/{key}/cards/{ref}", http.StatusOK, a.card},
		{"PATCH /api/p/{key}/cards/{ref}", http.StatusOK, a.editCard},
		{"POST /api/p/{key}/cards/{ref}/move", http.StatusOK, a.moveCard},
		{"POST /api/p/{key}/cards/{ref}/comments", http.StatusCreated, a.commentCard},
		{"POST /api/p/{key}/cards/{ref}/claim", http.StatusOK, a.claimCard},
		{"POST /api/p/{key}/cards/{ref}/release", http.StatusOK, a.releaseCard},
		{"GET /api/p/{key}/vault", http.StatusOK, a.entries},
		{"POST /api/p/{key}/vault", http.StatusCreated, a.newEntry},
		{"GET /api/p/{key}/vault/{slug}", http.StatusOK, a.entry},
		{"PATCH /api/p/{key}/vault/{slug}", http.StatusOK, a.editEntry},
		{"POST /api/p/{key}/vault/{slug}/publish", http.StatusOK, a.share(a.p.Publish)},
		{"POST /api/p/{key}/vault/{slug}/promote", http.StatusOK, a.promote},
		{"POST /api/p/{key}/vault/{slug}/demote", http.StatusOK, a.share(a.p.Demote)},
		{"POST /api/p/{key}/vault/{slug}/verify", http.StatusOK, a.share(a.p.Verify)},
		{"GET /api/p/{key}/search", http.StatusOK, a.search},
		{"GET /api/p/{key}/nominations", http.StatusOK, a.nominations},
		{"GET /api/p/{key}/unverified", http.StatusOK, a.unverified},
	}
	for _, rt := range routes {
		mux.Handle(rt.pattern, a.guard(rt.status, rt.h))
	}
}

func (a *webAPI) guard(status int, h func(webCall) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(sessionCookie)
		if err != nil {
			writeErrorStatus(w, http.StatusUnauthorized, platform.ErrPolicy("no_session", "this request carries no session", "sign in at /login"))
			return
		}
		s, err := a.p.Session(r.Context(), ck.Value)
		if platform.IsCode(err, "bad_session") {
			writeErrorStatus(w, http.StatusUnauthorized, err)
			return
		}
		if err != nil {
			writeError(w, err)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get(headerCSRF)), []byte(s.CSRF)) != 1 {
			writeError(w, platform.ErrPolicy("bad_csrf", "a write needs the header "+headerCSRF+" equal to the csrf value of /api/me", "GET /api/me, then send its csrf value"))
			return
		}
		out, err := h(webCall{w: w, r: r, s: s, secret: ck.Value})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, status, out)
	})
}

func readJSON(r *http.Request, v any) error {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return platform.ErrUsage("bad_json", "the request body is not the JSON this route takes: "+err.Error(), "send a JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.MarshalWrite(w, v)
}

func writeError(w http.ResponseWriter, err error) {
	writeErrorStatus(w, 0, err)
}

func writeErrorStatus(w http.ResponseWriter, status int, err error) {
	e, ok := errors.AsType[*platform.Error](err)
	if !ok {
		e = &platform.Error{Code: "internal", Msg: err.Error(), Fix: "retry, or report the error"}
	}
	if status == 0 {
		status = kindStatus[e.Kind]
	}
	writeJSON(w, status, apiError{Code: e.Code, Kind: string(e.Kind), Message: e.Msg, Fix: e.Fix, Detail: e.Detail})
}

var kindStatus = map[platform.Kind]int{
	platform.KindUsage:    http.StatusBadRequest,
	platform.KindNotFound: http.StatusNotFound,
	platform.KindConflict: http.StatusConflict,
	platform.KindPolicy:   http.StatusForbidden,
	"":                    http.StatusInternalServerError,
}

type nameIn struct {
	Name string `json:"name"`
}

type cardIn struct {
	Title     *string `json:"title,omitempty"`
	Body      *string `json:"body,omitempty"`
	Column    string  `json:"column,omitempty"`
	IfVersion int64   `json:"if_version,omitempty"`
}

type claimIn struct {
	Steal      bool   `json:"steal,omitempty"`
	Reason     string `json:"reason,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

type entryIn struct {
	Slug      string    `json:"slug,omitempty"`
	Title     *string   `json:"title,omitempty"`
	Summary   *string   `json:"summary,omitempty"`
	Body      *string   `json:"body,omitempty"`
	Template  string    `json:"template,omitempty"`
	Private   *bool     `json:"private,omitempty"`
	Tags      *[]string `json:"tags,omitempty"`
	IfVersion int64     `json:"if_version,omitempty"`
}

type reasonIn struct {
	Reason string `json:"reason"`
}

func deref[T any](v *T) T {
	if v == nil {
		var zero T
		return zero
	}
	return *v
}

func (a *webAPI) projects(c webCall) (any, error) {
	projects, err := a.p.ListProjects(c.r.Context(), c.pr())
	return map[string]any{"projects": projects}, err
}

func (a *webAPI) boards(c webCall) (any, error) {
	boards, err := a.p.ListBoards(c.r.Context(), c.pr(), c.key())
	return map[string]any{"boards": boards}, err
}

func (a *webAPI) newBoard(c webCall) (any, error) {
	var in nameIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	b, err := a.p.NewBoard(c.r.Context(), c.pr(), c.key(), in.Name)
	return map[string]any{"board": b}, err
}

func (a *webAPI) cards(c webCall) (any, error) {
	cards, err := a.p.ListCards(c.r.Context(), c.pr(), c.key(), c.r.PathValue("board"))
	return map[string]any{"cards": cards}, err
}

func (a *webAPI) newCard(c webCall) (any, error) {
	var in cardIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	card, err := a.p.CreateCard(c.r.Context(), c.pr(), c.key(), platform.NewCard{
		Board: c.r.PathValue("board"), Title: deref(in.Title), Body: deref(in.Body), Column: in.Column,
	})
	return map[string]any{"card": card}, err
}

func (a *webAPI) card(c webCall) (any, error) {
	card, err := a.p.ShowCard(c.r.Context(), c.pr(), c.key(), c.r.PathValue("ref"))
	return map[string]any{"card": card}, err
}

func (a *webAPI) editCard(c webCall) (any, error) {
	var in cardIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	card, err := a.p.EditCard(c.r.Context(), c.pr(), c.key(), c.r.PathValue("ref"),
		platform.CardEdit{Title: in.Title, Body: in.Body, IfVersion: in.IfVersion})
	return map[string]any{"card": card}, err
}

func (a *webAPI) moveCard(c webCall) (any, error) {
	var in cardIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	card, err := a.p.MoveCard(c.r.Context(), c.pr(), c.key(), c.r.PathValue("ref"), in.Column)
	return map[string]any{"card": card}, err
}

func (a *webAPI) commentCard(c webCall) (any, error) {
	var in cardIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	ctx, ref := c.r.Context(), c.r.PathValue("ref")
	comment, err := a.p.CommentCard(ctx, c.pr(), c.key(), ref, deref(in.Body))
	if err != nil {
		return nil, err
	}
	card, err := a.p.ShowCard(ctx, c.pr(), c.key(), ref)
	return map[string]any{"card": card, "comment": comment}, err
}

func (a *webAPI) claimCard(c webCall) (any, error) {
	var in claimIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	card, err := a.p.ClaimCard(c.r.Context(), c.pr(), c.key(), c.r.PathValue("ref"), platform.ClaimOpts{
		TTL: time.Duration(in.TTLSeconds) * time.Second, Steal: in.Steal, Reason: in.Reason,
	})
	return map[string]any{"card": card}, err
}

func (a *webAPI) releaseCard(c webCall) (any, error) {
	card, err := a.p.ReleaseClaim(c.r.Context(), c.pr(), c.key(), c.r.PathValue("ref"))
	return map[string]any{"card": card}, err
}

func (a *webAPI) entries(c webCall) (any, error) {
	entries, err := a.p.ListEntries(c.r.Context(), c.pr(), c.key())
	return map[string]any{"entries": entries}, err
}

func (a *webAPI) newEntry(c webCall) (any, error) {
	var in entryIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	e, err := a.p.CreateEntry(c.r.Context(), c.pr(), c.key(), platform.NewEntry{
		Slug: in.Slug, Title: deref(in.Title), Summary: deref(in.Summary), Body: deref(in.Body),
		Template: in.Template, Private: deref(in.Private), Tags: deref(in.Tags),
	})
	return map[string]any{"entry": e}, err
}

func (a *webAPI) entry(c webCall) (any, error) {
	e, err := a.p.ShowEntry(c.r.Context(), c.pr(), c.key(), c.r.PathValue("slug"))
	return map[string]any{"entry": e}, err
}

func (a *webAPI) editEntry(c webCall) (any, error) {
	var in entryIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	e, err := a.p.EditEntry(c.r.Context(), c.pr(), c.key(), c.r.PathValue("slug"), platform.EntryEdit{
		Title: in.Title, Summary: in.Summary, Body: in.Body, Private: in.Private, Tags: in.Tags, IfVersion: in.IfVersion,
	})
	return map[string]any{"entry": e}, err
}

type shareFunc func(ctx context.Context, pr platform.Principal, project, slug string) (platform.ShareResult, error)

func (a *webAPI) share(f shareFunc) func(webCall) (any, error) {
	return func(c webCall) (any, error) {
		return f(c.r.Context(), c.pr(), c.key(), c.r.PathValue("slug"))
	}
}

func (a *webAPI) promote(c webCall) (any, error) {
	var in reasonIn
	if err := readJSON(c.r, &in); err != nil {
		return nil, err
	}
	return a.p.Promote(c.r.Context(), c.pr(), c.key(), c.r.PathValue("slug"), in.Reason)
}

func (a *webAPI) search(c webCall) (any, error) {
	q := c.r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	hits, err := a.p.Search(c.r.Context(), c.pr(), c.key(), q.Get("q"),
		platform.SearchOpts{Limit: limit, Method: q.Get("method"), Label: q.Get("label")})
	return map[string]any{"hits": hits}, err
}

func (a *webAPI) nominations(c webCall) (any, error) {
	waiting, err := a.p.Nominations(c.r.Context(), c.pr(), c.key())
	return map[string]any{"nominations": waiting}, err
}

func (a *webAPI) unverified(c webCall) (any, error) {
	entries, err := a.p.UnverifiedEntries(c.r.Context(), c.pr(), c.key())
	if err != nil {
		return nil, err
	}
	return map[string]any{"entries": entries}, nil
}
