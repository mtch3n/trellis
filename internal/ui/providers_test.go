package ui

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvidersNeverAnswerWithTheKey(t *testing.T) {
	s := settingsTestServer(t)

	rec := request(t, s, http.MethodPut, "/api/providers/work",
		`{"kind":"openai","model":"gpt-5","api_key":"sk-secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatalf("key echoed: %s", rec.Body)
	}
	var out providersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Default != "work" || len(out.Providers) != 1 || !out.Providers[0].HasKey {
		t.Fatalf("providers = %+v", out)
	}

	// Saving without api_key keeps the stored one.
	rec = request(t, s, http.MethodPut, "/api/providers/work", `{"kind":"openai","model":"gpt-5-mini"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"has_key":true`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	// An empty api_key removes it.
	rec = request(t, s, http.MethodPut, "/api/providers/work", `{"kind":"openai","model":"gpt-5-mini","api_key":""}`)
	if !strings.Contains(rec.Body.String(), `"has_key":false`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestPutProviderReportsEveryProblem(t *testing.T) {
	s := settingsTestServer(t)
	rec := request(t, s, http.MethodPut, "/api/providers/work", `{"kind":"azure"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "base URL") || !strings.Contains(rec.Body.String(), "model") {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestDeletingTheDefaultProviderPromotesTheNext(t *testing.T) {
	s := settingsTestServer(t)
	request(t, s, http.MethodPut, "/api/providers/a", `{"kind":"claude-code"}`)
	request(t, s, http.MethodPut, "/api/providers/b", `{"kind":"codex"}`)
	rec := request(t, s, http.MethodDelete, "/api/providers/a", "")
	if !strings.Contains(rec.Body.String(), `"default_provider":"b"`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestProxyAddsTheCredentialAndDropsTheBrowsers(t *testing.T) {
	var got *http.Request
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Set-Cookie", "upstream=1")
		io.WriteString(w, "data: hello\n\n")
	}))
	defer upstream.Close()

	s := settingsTestServer(t)
	rec := request(t, s, http.MethodPut, "/api/providers/az",
		`{"kind":"azure","model":"gpt-5","api_key":"az-key","base_url":"`+upstream.URL+`/openai"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/providers/az/proxy/responses?x=1", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", "from-browser")
	req.Header.Set("Cookie", "trellis_session=abc")
	req.Header.Set("X-Trellis-Token", "abc")
	out := httptest.NewRecorder()
	s.mux.ServeHTTP(out, req)

	if out.Code != http.StatusOK || out.Body.String() != "data: hello\n\n" {
		t.Fatalf("status = %d, body = %q", out.Code, out.Body)
	}
	if out.Header().Get("Set-Cookie") != "" {
		t.Error("upstream cookie passed to the browser")
	}
	if got.URL.Path != "/openai/v1/responses" || got.URL.RawQuery != "x=1" {
		t.Errorf("upstream got %s?%s", got.URL.Path, got.URL.RawQuery)
	}
	if got.Header.Get("Api-Key") != "az-key" {
		t.Errorf("api-key = %q", got.Header.Get("Api-Key"))
	}
	if got.Header.Get("Cookie") != "" || got.Header.Get("X-Trellis-Token") != "" {
		t.Error("browser credentials reached the provider")
	}
	if gotBody != `{"model":"gpt-5"}` {
		t.Errorf("body = %q", gotBody)
	}
}

func TestProxyRefusesALocalAgent(t *testing.T) {
	s := settingsTestServer(t)
	request(t, s, http.MethodPut, "/api/providers/claude", `{"kind":"claude-code"}`)
	rec := request(t, s, http.MethodPost, "/api/providers/claude/proxy/messages", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestAChatToolWritesAsTheChatAgent(t *testing.T) {
	s := settingsTestServer(t)
	p, err := s.core.CreateProject(t.Context(), "TEST", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.core.CreateBoard(t.Context(), p.ID, "main", true); err != nil {
		t.Fatal(err)
	}
	request(t, s, http.MethodPut, "/api/providers/work", `{"kind":"claude-code"}`)
	send := func(path, body, chat string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if chat != "" {
			req.Header.Set(chatHeader, chat)
		}
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}
	rec := send("/api/p/TEST/b/main/cards", `{"title":"From chat"}`, "")
	var card struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	for chat, want := range map[string]string{"work": "agent:chat-work", "Not Valid": s.actor, "unknown": s.actor} {
		rec = send("/api/p/TEST/b/main/cards/"+card.Ref+"/comments", `{"body":"hi"}`, chat)
		var comment struct {
			Actor string `json:"actor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &comment); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
		if comment.Actor != want {
			t.Errorf("chat %q wrote as %q, want %q", chat, comment.Actor, want)
		}
	}
}
