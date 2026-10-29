package mcpserver_test

import (
	"encoding/json/v2"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
	"github.com/mtch3n/trellis/internal/platform"
	"github.com/mtch3n/trellis/internal/server"
)

type env struct {
	t   *testing.T
	p   *platform.Platform
	db  *sqlx.DB
	url string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := pgtest.Open(t)
	if err := pgstore.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	p := platform.New(db)
	srv := httptest.NewServer(server.New(p))
	t.Cleanup(srv.Close)
	e := &env{t: t, p: p, db: db, url: srv.URL + "/mcp"}
	e.must(p.CreateOrg(t.Context(), "acme"))
	e.must(p.CreateProject(t.Context(), "acme", "APP", "App"))
	return e
}

func (e *env) must(_ any, err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) member(handle, tokenName string) string {
	e.t.Helper()
	ctx := e.t.Context()
	if _, err := e.p.Member(ctx, "acme", handle); err != nil {
		e.must(e.p.AddMember(ctx, "acme", handle, handle))
		if err := e.p.AddProjectMember(ctx, "acme", "APP", handle, "member"); err != nil {
			e.t.Fatal(err)
		}
	}
	token, err := e.p.IssueToken(ctx, "acme", handle, tokenName, platform.TokenAgent)
	if err != nil {
		e.t.Fatal(err)
	}
	return token
}

type headers map[string]string

func (h headers) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h {
		r.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (e *env) connect(token string, h headers, version string) *mcp.ClientSession {
	e.t.Helper()
	h = h.with("Authorization", "Bearer "+token, true)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(e.t.Context(), &mcp.StreamableClientTransport{Endpoint: e.url, HTTPClient: &http.Client{Transport: h}},
		&mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { cs.Close() })
	return cs
}

func (e *env) agent(handle, tokenName string, h headers) *mcp.ClientSession {
	e.t.Helper()
	return e.connect(e.member(handle, tokenName), h.with("X-Trellis-Project", "APP", false), "")
}

func (h headers) with(k, v string, replace bool) headers {
	out := maps.Clone(h)
	if out == nil {
		out = headers{}
	}
	if _, set := out[k]; replace || !set {
		out[k] = v
	}
	return out
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

func ok[T any](t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) T {
	t.Helper()
	res := call(t, cs, tool, args)
	if res.IsError {
		t.Fatalf("%s %v: %s", tool, args, text(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: decode %s: %v", tool, raw, err)
	}
	return out
}

type toolErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

func fails(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) toolErr {
	t.Helper()
	res := call(t, cs, tool, args)
	if !res.IsError {
		t.Fatalf("%s %v succeeded, want a tool error", tool, args)
	}
	return decodeErr(t, res)
}

func decodeErr(t *testing.T, res *mcp.CallToolResult) toolErr {
	t.Helper()
	var e toolErr
	if err := json.Unmarshal([]byte(text(res)), &e); err != nil {
		t.Fatalf("the error is not JSON {code, message, fix}: %q", text(res))
	}
	return e
}

func text(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func (e *env) count(table string) int {
	e.t.Helper()
	var n int
	if err := e.db.Get(&n, `SELECT count(*) FROM `+table); err != nil {
		e.t.Fatal(err)
	}
	return n
}
