package ui

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func TestEntryHistoryAndDiffRoutes(t *testing.T) {
	s, c, p := artifactTestServer(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, core.NewEntry{Title: "Notes", Body: "line one\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "line one\nline two\n", &entry.Version); err != nil {
		t.Fatal(err)
	}

	history := serve(s, http.MethodGet, "/api/p/"+p.Key+"/vault/"+entry.Slug+"/history", nil)
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d, body = %s", history.Code, history.Body)
	}
	var revs []core.RevisionInfo
	if err := json.Unmarshal(history.Body.Bytes(), &revs); err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Version != 2 || revs[1].Version != 1 {
		t.Fatalf("revisions = %+v, want [2 1]", revs)
	}

	diff := serve(s, http.MethodGet, "/api/p/"+p.Key+"/vault/"+entry.Slug+"/diff", nil)
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), "line two") {
		t.Fatalf("diff status = %d, body = %s", diff.Code, diff.Body)
	}

	explicit := serve(s, http.MethodGet, "/api/p/"+p.Key+"/vault/"+entry.Slug+"/diff?from=1&to=2", nil)
	if explicit.Code != http.StatusOK || !strings.Contains(explicit.Body.String(), `"from":1`) {
		t.Fatalf("explicit diff status = %d, body = %s", explicit.Code, explicit.Body)
	}

	badRange := serve(s, http.MethodGet, "/api/p/"+p.Key+"/vault/"+entry.Slug+"/diff?from=9&to=9", nil)
	if badRange.Code != http.StatusBadRequest {
		t.Errorf("unretained version status = %d, want 400", badRange.Code)
	}
}

func TestCardHistoryAndDiffRoutes(t *testing.T) {
	s, c, p := artifactTestServer(t)
	board, err := c.SelectBoard(t.Context(), p.ID, "default")
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(t.Context(), p.ID, board.ID, core.NewCard{Title: "Ship", Body: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	newBody := "final"
	if _, err := c.EditCard(t.Context(), p.ID, core.CardRef{UUID: card.ID}, core.CardEdit{Body: &newBody, IfVersion: &card.Version}); err != nil {
		t.Fatal(err)
	}

	history := serve(s, http.MethodGet, "/api/p/"+p.Key+"/cards/"+card.Ref+"/history", nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"actor"`) {
		t.Fatalf("history status = %d, body = %s", history.Code, history.Body)
	}

	diff := serve(s, http.MethodGet, "/api/p/"+p.Key+"/cards/"+card.Ref+"/diff", nil)
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), "-draft") || !strings.Contains(diff.Body.String(), "+final") {
		t.Fatalf("diff status = %d, body = %s", diff.Code, diff.Body)
	}
}

// The four routes sit behind protectedHandler like every other /api/ route.
func TestHistoryRoutesAreProtected(t *testing.T) {
	s, c, p := artifactTestServer(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, core.NewEntry{Title: "Guarded"})
	if err != nil {
		t.Fatal(err)
	}
	h := s.protectedHandler("127.0.0.1:0")

	do := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/p/"+p.Key+"/vault/"+entry.Slug+"/history", nil)
		req.Host = "127.0.0.1:0"
		if token != "" {
			req.Header.Set("X-Trellis-Token", token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do(""); got != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", got)
	}
	if got := do(s.token); got != http.StatusOK {
		t.Errorf("with token: %d, want 200", got)
	}
}
