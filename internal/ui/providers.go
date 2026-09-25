package ui

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/provider"
)

// providerView is a provider as the browser sees it: everything but the key,
// which is reported only as present or not.
type providerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	APIKeyEnv string `json:"api_key_env"`
	Command   string `json:"command"`
	Dir       string `json:"dir"`
	HasKey    bool   `json:"has_key"`
}

type providersResponse struct {
	File      string                `json:"file"`
	Default   string                `json:"default_provider"`
	Providers []providerView        `json:"providers"`
	Kinds     []config.ProviderKind `json:"kinds"`
	Efforts   []string              `json:"efforts"`
}

func viewProvider(p config.Provider) providerView {
	return providerView{
		ID: p.ID, Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL, Model: p.Model, Effort: p.Effort,
		APIKeyEnv: p.APIKeyEnv, Command: p.Command, Dir: p.Dir, HasKey: p.Key() != "",
	}
}

func (s *Server) writeProviders(w http.ResponseWriter, ai config.AIConfig) {
	views := make([]providerView, 0, len(ai.Providers))
	for _, p := range ai.Providers {
		views = append(views, viewProvider(p))
	}
	writeJSON(w, http.StatusOK, providersResponse{
		File: config.Path(s.root), Default: ai.DefaultProvider, Providers: views, Kinds: config.ProviderKinds(), Efforts: config.Efforts,
	})
}

// handleProviders lists the configured providers and every kind one can be.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeProviders(w, cfg.AI)
}

// saveAI writes the section and answers with the list it now holds, or with
// every problem that stopped it.
func (s *Server) saveAI(w http.ResponseWriter, ai config.AIConfig) {
	cfg, err := config.SetAI(s.root, ai)
	if err != nil {
		if ise, ok := errors.AsType[*config.InvalidSettingsError](err); ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "invalid provider", "code": "invalid_settings", "problems": ise.Problems,
			})
			return
		}
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeProviders(w, cfg.AI)
}

type providerRequest struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	APIKeyEnv string `json:"api_key_env"`
	Command   string `json:"command"`
	Dir       string `json:"dir"`
	// APIKey absent keeps the stored key; an empty string removes it.
	APIKey *string `json:"api_key"`
}

// handlePutProvider creates the provider named in the path, or replaces its
// settings. The first provider becomes the default.
func (s *Server) handlePutProvider(w http.ResponseWriter, r *http.Request) {
	var in providerRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	cfg, err := config.Load(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := r.PathValue("id")
	next := config.Provider{
		ID: id, Name: strings.TrimSpace(in.Name), Kind: in.Kind, BaseURL: strings.TrimSpace(in.BaseURL),
		Model: strings.TrimSpace(in.Model), Effort: in.Effort, APIKeyEnv: strings.TrimSpace(in.APIKeyEnv),
		Command: strings.TrimSpace(in.Command), Dir: strings.TrimSpace(in.Dir),
	}
	ai := cfg.AI
	ai.Providers = slices.Clone(ai.Providers)
	if i := slices.IndexFunc(ai.Providers, func(p config.Provider) bool { return p.ID == id }); i >= 0 {
		next.APIKey = ai.Providers[i].APIKey
		if in.APIKey != nil {
			next.APIKey = strings.TrimSpace(*in.APIKey)
		}
		ai.Providers[i] = next
	} else {
		if in.APIKey != nil {
			next.APIKey = strings.TrimSpace(*in.APIKey)
		}
		ai.Providers = append(ai.Providers, next)
	}
	if ai.DefaultProvider == "" {
		ai.DefaultProvider = id
	}
	s.saveAI(w, ai)
}

// handleDeleteProvider removes a provider. Removing the default leaves the
// first remaining provider as the default.
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := r.PathValue("id")
	ai := cfg.AI
	if _, ok := ai.Provider(id); !ok {
		s.error(w, http.StatusNotFound, "no provider "+id)
		return
	}
	ai.Providers = slices.DeleteFunc(slices.Clone(ai.Providers), func(p config.Provider) bool { return p.ID == id })
	if ai.DefaultProvider == id {
		ai.DefaultProvider = ""
		if len(ai.Providers) > 0 {
			ai.DefaultProvider = ai.Providers[0].ID
		}
	}
	s.saveAI(w, ai)
}

// handlePatchProviders changes which provider the chat opens with.
func (s *Server) handlePatchProviders(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Default string `json:"default_provider"`
	}
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	cfg, err := config.Load(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	ai := cfg.AI
	ai.DefaultProvider = in.Default
	s.saveAI(w, ai)
}

// handleTestProvider checks that a provider answers with its saved settings.
func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := s.lookupProvider(w, r)
	if !ok {
		return
	}
	if err := provider.Test(r.Context(), p); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Connected."})
}

// handleProviderProxy forwards the browser's AI SDK request to the
// provider's API with the provider's credential.
func (s *Server) handleProviderProxy(w http.ResponseWriter, r *http.Request) {
	p, ok := s.lookupProvider(w, r)
	if !ok {
		return
	}
	proxy, err := provider.Proxy(p, r.PathValue("rest"))
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) lookupProvider(w http.ResponseWriter, r *http.Request) (config.Provider, bool) {
	cfg, err := config.Load(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return config.Provider{}, false
	}
	p, ok := cfg.AI.Provider(r.PathValue("id"))
	if !ok {
		s.error(w, http.StatusNotFound, "no provider "+r.PathValue("id"))
		return config.Provider{}, false
	}
	return p, true
}

// chatHeader marks a request a chat's tool made on the model's behalf. Its
// value is the provider's id, and the write is recorded as that agent's
// rather than as the person at the browser's.
const chatHeader = "X-Trellis-Chat"

// writer is the Core a request's writes go through: the person at the
// browser, or the chat agent acting for them. The header only relabels a
// write the session could make anyway, and only as a provider that exists,
// so it cannot name an arbitrary actor.
func (s *Server) writer(r *http.Request) *core.Core {
	id := r.Header.Get(chatHeader)
	if id == "" || !config.ValidProviderID(id) {
		return s.write
	}
	if cfg, err := config.Load(s.root); err == nil {
		if _, ok := cfg.AI.Provider(id); ok {
			return s.write.WithActor("agent:chat-" + id)
		}
	}
	return s.write
}

type chatRequest struct {
	Provider string `json:"provider"`
	Prompt   string `json:"prompt"`
	Session  string `json:"session"`
	// Effort overrides the provider's own for this turn; empty keeps it.
	Effort string `json:"effort"`
}

// handleChat runs one turn of a local agent in the project and streams what
// it does as an AI SDK UI message stream. API providers never come here:
// the browser drives them itself, through the proxy.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var in chatRequest
	if !decodeJSON(w, r, &in) || strings.TrimSpace(in.Prompt) == "" {
		s.error(w, http.StatusBadRequest, "a provider and a prompt are required")
		return
	}
	p, err := s.projectByKey(r.Context(), r.PathValue("key"))
	if err != nil {
		s.error(w, http.StatusNotFound, err.Error())
		return
	}
	cfg, err := config.Load(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	prov, ok := cfg.AI.Provider(in.Provider)
	if !ok {
		s.error(w, http.StatusNotFound, "no provider "+in.Provider)
		return
	}
	if !config.ValidEffort(in.Effort) {
		s.error(w, http.StatusBadRequest, "effort must be one of "+strings.Join(config.Efforts, ", "))
		return
	}
	if in.Effort != "" {
		prov.Effort = in.Effort
	}
	if !provider.IsLocal(prov) {
		s.error(w, http.StatusBadRequest, "provider "+prov.ID+" is an API; the browser talks to it through the proxy")
		return
	}

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Vercel-Ai-Ui-Message-Stream", "v1")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	emit := func(c provider.Chunk) error {
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	turn := provider.Turn{
		Provider: prov, Project: p.Key, Prompt: in.Prompt, Session: in.Session,
		Root: s.root, Actor: "agent:chat-" + prov.ID,
	}
	if err := provider.Stream(r.Context(), turn, emit); err != nil {
		if r.Context().Err() == nil {
			_ = emit(provider.Chunk{"type": "error", "errorText": err.Error()})
			_ = emit(provider.Chunk{"type": "finish"})
		}
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
