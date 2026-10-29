package server

import (
	"net/http"
	"testing"

	"github.com/mtch3n/trellis/internal/platform"
)

func TestWeb_WEB_C1_PersonTokenSignsInWithStrictHttpOnlyCookie(t *testing.T) {
	e := webSetup(t)
	c := e.webClient()

	resp := c.post("/login", map[string]string{"token": e.webToken("ann", "browser", platform.TokenPerson)})
	if resp.StatusCode/100 != 2 {
		t.Fatalf("POST /login = %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v, want one", cookies)
	}
	if ck := cookies[0]; !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Value == "" {
		t.Fatalf("cookie = %+v, want HttpOnly and SameSite=Strict", ck)
	}
	var me webMe
	if code := c.getJSON("/api/me", &me); code != http.StatusOK {
		t.Fatalf("GET /api/me = %d", code)
	}
	if me.Handle != "ann" || me.CSRF == "" {
		t.Fatalf("me = %+v", me)
	}
}

func TestWeb_WEB_C2_AgentRevokedOrWrongTokenIsRefusedAtLogin(t *testing.T) {
	e := webSetup(t)
	agent := e.webToken("ann", "claude-laptop", platform.TokenAgent)
	revoked := e.webToken("ann", "old-browser", platform.TokenPerson)
	if err := e.p.RevokeToken(t.Context(), "acme", "ann", "old-browser"); err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{"agent": agent, "revoked": revoked, "wrong": "not-a-token"} {
		resp := e.webClient().post("/login", map[string]string{"token": token})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s token: POST /login = %d, want 401", name, resp.StatusCode)
		}
		if len(resp.Cookies()) != 0 {
			t.Fatalf("%s token: cookies set %v", name, resp.Cookies())
		}
	}
}

func TestWeb_WEB_C3_ExpiredOrEndedSessionIsUnauthorized(t *testing.T) {
	e := webSetup(t)
	old := e.webLogin("ann")
	e.db.MustExec(`UPDATE web_session SET created_at = now() - interval '12 hours 1 minute'`)
	if code := old.getJSON("/api/me", nil); code != http.StatusUnauthorized {
		t.Fatalf("12-hour-old session: GET /api/me = %d, want 401", code)
	}

	ended := e.webLogin("ann")
	if resp := ended.post("/api/logout", struct{}{}); resp.StatusCode/100 != 2 {
		t.Fatalf("POST /api/logout = %d", resp.StatusCode)
	}
	if code := ended.getJSON("/api/me", nil); code != http.StatusUnauthorized {
		t.Fatalf("ended session: GET /api/me = %d, want 401", code)
	}
}
