package server

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
	"github.com/mtch3n/trellis/internal/platform"
)

type webEnv struct {
	t      *testing.T
	p      *platform.Platform
	db     *sqlx.DB
	url    string
	logins int
}

func webSetup(t *testing.T) *webEnv {
	t.Helper()
	db := pgtest.Open(t)
	if err := pgstore.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	p := platform.New(db)
	srv := httptest.NewServer(New(p))
	t.Cleanup(srv.Close)
	e := &webEnv{t: t, p: p, db: db, url: srv.URL}
	if _, err := p.CreateOrg(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(t.Context(), "acme", "APP", "App"); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *webEnv) webMember(handle string) {
	e.t.Helper()
	ctx := e.t.Context()
	if _, err := e.p.Member(ctx, "acme", handle); err == nil {
		return
	}
	if _, err := e.p.AddMember(ctx, "acme", handle, handle); err != nil {
		e.t.Fatal(err)
	}
	if err := e.p.AddProjectMember(ctx, "acme", "APP", handle, "member"); err != nil {
		e.t.Fatal(err)
	}
}

func (e *webEnv) webToken(handle, name, kind string) string {
	e.t.Helper()
	e.webMember(handle)
	token, err := e.p.IssueToken(e.t.Context(), "acme", handle, name, kind)
	if err != nil {
		e.t.Fatal(err)
	}
	return token
}

func (e *webEnv) webAgent(handle string) platform.Principal {
	e.t.Helper()
	id, err := e.p.VerifyToken(e.t.Context(), e.webToken(handle, "seed", platform.TokenAgent))
	if err != nil {
		e.t.Fatal(err)
	}
	return id.Principal("")
}

type webClient struct {
	e    *webEnv
	http *http.Client
	csrf string
}

func (e *webEnv) webClient() *webClient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return &webClient{e: e, http: &http.Client{Jar: jar}}
}

func (e *webEnv) webLogin(handle string) *webClient {
	e.t.Helper()
	c := e.webClient()
	e.logins++
	resp := c.post("/login", map[string]string{"token": e.webToken(handle, fmt.Sprintf("browser-%d", e.logins), platform.TokenPerson)})
	if resp.StatusCode/100 != 2 {
		e.t.Fatalf("login %s = %d", handle, resp.StatusCode)
	}
	var me webMe
	if code := c.getJSON("/api/me", &me); code != http.StatusOK {
		e.t.Fatalf("GET /api/me = %d", code)
	}
	c.csrf = me.CSRF
	return c
}

type webMe struct {
	Handle string `json:"handle"`
	CSRF   string `json:"csrf"`
}

type webError struct {
	Code    string `json:"code"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

func (c *webClient) post(path string, body any) *http.Response {
	c.e.t.Helper()
	return c.send(http.MethodPost, path, body, c.csrf)
}

func (c *webClient) send(method, path string, body any, csrf string) *http.Response {
	c.e.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		c.e.t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(c.e.t.Context(), method, c.e.url+path, bytes.NewReader(raw))
	if err != nil {
		c.e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-Trellis-CSRF", csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	c.e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (c *webClient) getJSON(path string, out any) int {
	c.e.t.Helper()
	resp, err := c.http.Get(c.e.url + path)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	webDecode(c.e.t, resp.Body, out)
	return resp.StatusCode
}

func webDecode(t *testing.T, r io.Reader, out any) {
	t.Helper()
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || len(raw) == 0 {
		return
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func (e *webEnv) webCount(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Get(&n, query, args...); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *webEnv) webProjectEntry(owner platform.Principal, slug string) {
	e.t.Helper()
	ctx := e.t.Context()
	if _, err := e.p.CreateEntry(ctx, owner, "APP", platform.NewEntry{
		Slug: slug, Title: "Rule " + slug, Summary: "The rule " + slug + ".", Body: "Follow the rule " + slug + " always.",
	}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.p.Publish(ctx, owner, "APP", slug); err != nil {
		e.t.Fatal(err)
	}
}

type webCardOut struct {
	Card platform.Card `json:"card"`
}

func TestWeb_WEB_C4_PersonCreatesCardOnPersonalBoardAsPerson(t *testing.T) {
	e := webSetup(t)
	ann := e.webLogin("ann")

	resp := ann.post("/api/p/APP/b/ann/cards", map[string]string{"title": "Write the login page"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST cards = %d", resp.StatusCode)
	}
	var out webCardOut
	webDecode(t, resp.Body, &out)
	if out.Card.Ref != "APP-1" || out.Card.Board != "ann" {
		t.Fatalf("card = %+v", out.Card)
	}
	if n := e.webCount(`SELECT count(*) FROM card c JOIN board b ON b.id = c.board_id JOIN member m ON m.id = b.owner_id
		WHERE c.id = $1 AND b.scope = 'personal' AND m.handle = 'ann'`, out.Card.ID); n != 1 {
		t.Fatal("the card is not on ann's personal board")
	}
	var actors []string
	if err := e.db.Select(&actors, `SELECT DISTINCT actor FROM event WHERE card_id = $1`, out.Card.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(actors, []string{"person:ann"}) {
		t.Fatalf("event actors = %v, want [person:ann]", actors)
	}
}

func TestWeb_WEB_C5_WriteWithoutOrWithWrongCSRFIsForbidden(t *testing.T) {
	e := webSetup(t)
	ann := e.webLogin("ann")

	for _, csrf := range []string{"", "wrong-value"} {
		resp := ann.send(http.MethodPost, "/api/p/APP/b/ann/cards", map[string]string{"title": "Sneaky"}, csrf)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("csrf %q: POST cards = %d, want 403", csrf, resp.StatusCode)
		}
	}
	if n := e.webCount(`SELECT count(*) FROM card`); n != 0 {
		t.Fatalf("%d cards written", n)
	}
}

func TestWeb_WEB_C6_OtherMembersPersonalBoardIsNotFound(t *testing.T) {
	e := webSetup(t)
	ann := e.webLogin("ann")
	if resp := ann.post("/api/p/APP/b/ann/cards", map[string]string{"title": "Private plan"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed card = %d", resp.StatusCode)
	}
	bob := e.webLogin("bob")

	for _, pair := range [][2]string{
		{"/api/p/APP/b/ann/cards", "/api/p/APP/b/nosuch/cards"},
		{"/api/p/APP/cards/APP-1", "/api/p/APP/cards/APP-999"},
	} {
		var hidden, missing webError
		if code := bob.getJSON(pair[0], &hidden); code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", pair[0], code)
		}
		if code := bob.getJSON(pair[1], &missing); code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", pair[1], code)
		}
		if hidden.Kind != "not_found" || hidden.Code == "" {
			t.Fatalf("GET %s error = %+v, want kind not_found", pair[0], hidden)
		}
		if hidden.Code != missing.Code || hidden.Kind != missing.Kind || hidden.Fix != missing.Fix {
			t.Fatalf("hidden %+v differs from missing %+v", hidden, missing)
		}
	}
}

type webShareOut struct {
	Slug     string     `json:"slug"`
	Scope    string     `json:"scope"`
	VerifyBy *time.Time `json:"verify_by"`
}

func TestWeb_WEB_C7_PersonPromotesNominatedEntryWithReason(t *testing.T) {
	e := webSetup(t)
	agent := e.webAgent("ann")
	e.webProjectEntry(agent, "tz-rule")
	if err := e.p.Nominate(t.Context(), agent, "APP", "tz-rule", "every service needs it"); err != nil {
		t.Fatal(err)
	}
	ann := e.webLogin("ann")

	resp := ann.post("/api/p/APP/vault/tz-rule/promote", map[string]string{"reason": "every service stores times"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promote = %d", resp.StatusCode)
	}
	var out webShareOut
	webDecode(t, resp.Body, &out)
	if out.Scope != "global" || out.VerifyBy == nil {
		t.Fatalf("promoted = %+v", out)
	}
	if ahead := time.Until(*out.VerifyBy); ahead < 179*24*time.Hour || ahead > 181*24*time.Hour {
		t.Fatalf("verify_by is %v ahead", ahead)
	}
	var waiting struct {
		Nominations []platform.Nomination `json:"nominations"`
	}
	if code := ann.getJSON("/api/p/APP/nominations", &waiting); code != http.StatusOK {
		t.Fatalf("GET nominations = %d", code)
	}
	for _, n := range waiting.Nominations {
		if n.Slug == "tz-rule" {
			t.Fatal("tz-rule is still nominated")
		}
	}
}

func TestWeb_WEB_C8_MemberVerifiesOverdueGlobalEntry(t *testing.T) {
	e := webSetup(t)
	agent := e.webAgent("ann")
	e.webProjectEntry(agent, "tz-rule")
	if _, err := e.p.Promote(t.Context(), agent, "APP", "tz-rule", "every service stores times"); err != nil {
		t.Fatal(err)
	}
	e.db.MustExec(`UPDATE entry SET verify_by = now() - interval '1 day' WHERE slug = 'tz-rule'`)
	bob := e.webLogin("bob")

	var before struct {
		Entries []platform.Entry `json:"entries"`
	}
	if code := bob.getJSON("/api/p/APP/unverified", &before); code != http.StatusOK {
		t.Fatalf("GET unverified = %d", code)
	}
	if len(before.Entries) != 1 || before.Entries[0].Slug != "tz-rule" {
		t.Fatalf("unverified before = %+v", before.Entries)
	}
	if resp := bob.post("/api/p/APP/vault/tz-rule/verify", struct{}{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("verify = %d", resp.StatusCode)
	}
	var after struct {
		Entries []platform.Entry `json:"entries"`
	}
	if code := bob.getJSON("/api/p/APP/unverified", &after); code != http.StatusOK {
		t.Fatalf("GET unverified = %d", code)
	}
	if len(after.Entries) != 0 {
		t.Fatalf("unverified after = %+v", after.Entries)
	}
}

func TestWeb_WEB_C9_PromoteWithoutReasonOrNotOwnedIsRefused(t *testing.T) {
	e := webSetup(t)
	e.webProjectEntry(e.webAgent("ann"), "tz-rule")
	e.webProjectEntry(e.webAgent("bob"), "bob-rule")
	ann := e.webLogin("ann")
	snapshot := func() string {
		var rows []string
		if err := e.db.Select(&rows, `SELECT slug || ':' || scope || ':' || version FROM entry ORDER BY slug`); err != nil {
			t.Fatal(err)
		}
		return fmtRows(rows)
	}
	before := snapshot()

	for _, tc := range []struct {
		path, reason, code, kind string
		status                   int
	}{
		{"/api/p/APP/vault/tz-rule/promote", "", "reason_required", "usage", http.StatusBadRequest},
		{"/api/p/APP/vault/bob-rule/promote", "it is good", "not_owner", "policy", http.StatusForbidden},
	} {
		resp := ann.post(tc.path, map[string]string{"reason": tc.reason})
		var got webError
		webDecode(t, resp.Body, &got)
		if resp.StatusCode != tc.status || got.Code != tc.code || got.Kind != tc.kind || got.Message == "" {
			t.Fatalf("POST %s = %d %+v, want %d %s", tc.path, resp.StatusCode, got, tc.status, tc.code)
		}
	}
	if after := snapshot(); after != before {
		t.Fatalf("entries changed: %s -> %s", before, after)
	}
}

func fmtRows(rows []string) string {
	raw, _ := json.Marshal(rows)
	return string(raw)
}
