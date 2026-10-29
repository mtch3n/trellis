package e2e

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
	_ "modernc.org/sqlite"
)

type card struct {
	Ref       string  `json:"ref"`
	Title     string  `json:"title"`
	ClaimedBy *string `json:"claimed_by"`
}

type cardOut struct {
	Card card `json:"card"`
}

type entry struct {
	Slug  string `json:"slug"`
	Scope string `json:"scope"`
	Owner string `json:"owner"`
}

type notice struct {
	Card  string   `json:"card"`
	With  string   `json:"with"`
	Words []string `json:"words"`
}

type apiErr struct {
	Code string `json:"code"`
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type harness struct {
	t   *testing.T
	bin string
	env []string
}

func (h *harness) cmd(extra []string, args ...string) *exec.Cmd {
	c := exec.Command(h.bin, args...)
	c.Env = append(slices.Clone(h.env), extra...)
	c.Dir = h.t.TempDir()
	return c
}

func (h *harness) run(extra []string, args ...string) []byte {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	c := h.cmd(extra, args...)
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		h.t.Fatalf("trellis %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.Bytes()
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "trellis")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/mtch3n/trellis/cmd/trellis").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func localHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	schema, err := os.ReadFile(filepath.Join("..", "importer", "testdata", "local.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlx.Open("sqlite", filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []struct {
		q    string
		args []any
	}{
		{string(schema), nil},
		{`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'LEG', 'Legacy', 1000)`, nil},
		{`INSERT INTO board (id, project_id, name, slug, created_at) VALUES ('b1', 'p1', 'main', 'main', 1000)`, nil},
		{`INSERT INTO column_ (id, board_id, name, position, is_done) VALUES ('b1-backlog', 'b1', 'backlog', 0, 0), ('b1-done', 'b1', 'done', 1, 1)`, nil},
		{`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, body_md, priority, version, created_at, updated_at, ref)
			VALUES ('c1', 'p1', 'b1', 1, 'b1-backlog', 'm', 'migrate the legacy cron jobs', 'from the old host', 1, 3, 1000, 2000, 'LEG-1')`, nil},
		{`INSERT INTO entry (id, project_id, slug, title, template, summary, content_hash, mtime, size, version, created_at, updated_at, provenance)
			VALUES ('e1', 'p1', 'cron-hosts', 'Cron hosts', '', 'where the cron jobs run', 'h', 1, 1, 1, 1000, 2000, 'authored')`, nil},
	}
	for _, s := range stmts {
		if _, err := db.Exec(s.q, s.args...); err != nil {
			t.Fatalf("%s: %v", s.q, err)
		}
	}
	file := filepath.Join(dir, "projects", "LEG", "vault", "cron-hosts.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntitle: Cron hosts\nsummary: where the cron jobs run\nprovenance: authored\n---\n\nThe cron jobs run on the old host.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type headerTransport map[string]string

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h {
		r.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func mcpCall(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (*mcp.CallToolResult, []byte) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				return res, []byte(tc.Text)
			}
		}
		return res, nil
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return res, raw
}

func mcpOK[T any](t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) T {
	t.Helper()
	res, raw := mcpCall(t, cs, tool, args)
	if res.IsError {
		t.Fatalf("%s %v: %s", tool, args, raw)
	}
	return decode[T](t, raw)
}

func mcpFails(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) apiErr {
	t.Helper()
	res, raw := mcpCall(t, cs, tool, args)
	if !res.IsError {
		t.Fatalf("%s %v succeeded with %s, want a tool error", tool, args, raw)
	}
	return decode[apiErr](t, raw)
}

type web struct {
	t    *testing.T
	base string
	c    *http.Client
	csrf string
}

func (w *web) do(method, path string, body any) (int, []byte) {
	w.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			w.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(w.t.Context(), method, w.base+path, rd)
	if err != nil {
		w.t.Fatal(err)
	}
	if w.csrf != "" {
		req.Header.Set("X-Trellis-CSRF", w.csrf)
	}
	res, err := w.c.Do(req)
	if err != nil {
		w.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		w.t.Fatal(err)
	}
	return res.StatusCode, raw
}

func (w *web) want(method, path string, body any, status int) []byte {
	w.t.Helper()
	got, raw := w.do(method, path, body)
	if got != status {
		w.t.Fatalf("%s %s = %d %s, want %d", method, path, got, raw, status)
	}
	return raw
}

func TestE2E_WholeServer(t *testing.T) {
	dbURL := pgtest.URL(t)
	bin := buildBinary(t)
	home := t.TempDir()
	h := &harness{t: t, bin: bin, env: append(os.Environ(),
		"TRELLIS_DATABASE_URL="+dbURL,
		"TRELLIS_HOME="+home,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
		"TRELLIS_URL=",
		"TRELLIS_TOKEN=",
		"TRELLIS_PROJECT=",
		"TRELLIS_SESSION=",
	)}
	tokens := map[string]string{}

	t.Run("a admin creates org, members, project and tokens", func(t *testing.T) {
		h.run(nil, "admin", "org", "new", "acme")
		for _, m := range []string{"ann", "bob"} {
			h.run(nil, "admin", "member", "add", m, "--org", "acme", "--name", strings.ToUpper(m[:1])+m[1:])
		}
		h.run(nil, "admin", "project", "new", "APP", "--org", "acme", "--name", "App")
		for _, m := range []string{"ann", "bob"} {
			h.run(nil, "admin", "project", "add-member", "APP", m, "--org", "acme")
			out := decode[map[string]string](t, h.run(nil, "admin", "token", "issue", m, "laptop", "--org", "acme"))
			tokens[m] = out["token"]
		}
		out := decode[map[string]string](t, h.run(nil, "admin", "token", "issue", "ann", "browser", "--org", "acme", "--kind", "person"))
		tokens["ann-web"] = out["token"]
		members := decode[[]map[string]string](t, h.run(nil, "admin", "project", "members", "APP", "--org", "acme"))
		if len(members) != 2 {
			t.Fatalf("APP members = %v", members)
		}
		for k, v := range tokens {
			if v == "" {
				t.Fatalf("token %s is empty", k)
			}
		}
	})

	t.Run("b import copies a local home into a second project", func(t *testing.T) {
		h.run(nil, "import", "--home", localHome(t), "--org", "acme", "--member", "ann")
		db, err := sqlx.Open("pgx", dbURL)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var keys []string
		if err := db.Select(&keys, `SELECT p.key FROM project p JOIN org o ON o.id = p.org_id WHERE o.name = 'acme' ORDER BY p.key`); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(keys, []string{"APP", "LEG"}) {
			t.Fatalf("projects = %v, want APP and LEG", keys)
		}
		var cards int
		if err := db.Get(&cards, `SELECT count(*) FROM card c JOIN project p ON p.id = c.project_id WHERE p.key = 'LEG'`); err != nil {
			t.Fatal(err)
		}
		if cards != 1 {
			t.Fatalf("LEG has %d cards, want 1", cards)
		}
	})

	addr := freeAddr(t)
	base := "http://" + addr
	var serveLog syncBuffer
	serve := h.cmd([]string{"TRELLIS_ADDR=" + addr, "TRELLIS_COORD_INTERVAL=1s"}, "serve")
	serve.Stdout, serve.Stderr = &serveLog, &serveLog
	served := make(chan error, 1)
	t.Cleanup(func() {
		if serve.ProcessState == nil && serve.Process != nil {
			serve.Process.Kill()
			<-served
		}
	})

	t.Run("c serve answers healthz", func(t *testing.T) {
		if err := serve.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { served <- serve.Wait() }()
		deadline := time.Now().Add(30 * time.Second)
		for {
			res, err := http.Get(base + "/healthz")
			if err == nil {
				res.Body.Close()
				if res.StatusCode == http.StatusOK {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("no healthz after 30s: %v\n%s", err, serveLog.String())
			}
			select {
			case err := <-served:
				t.Fatalf("serve exited: %v\n%s", err, serveLog.String())
			case <-time.After(200 * time.Millisecond):
			}
		}
	})

	annEnv := []string{"TRELLIS_URL=" + base, "TRELLIS_TOKEN=" + tokens["ann"], "TRELLIS_PROJECT=APP"}
	call := func(tool string, args string) []byte {
		t.Helper()
		if args == "" {
			return h.run(annEnv, "call", tool)
		}
		return h.run(annEnv, "call", tool, args)
	}
	var annCard card

	t.Run("d thin client creates, claims, comments and reminds", func(t *testing.T) {
		annCard = decode[cardOut](t, call("card_write", `{"action":"new","title":"postgres backup credentials rotation"}`)).Card
		if annCard.Ref == "" {
			t.Fatal("the new card has no ref")
		}
		claimed := decode[cardOut](t, call("card_claim", `{"action":"claim","ref":"`+annCard.Ref+`"}`)).Card
		if claimed.ClaimedBy == nil || !strings.Contains(*claimed.ClaimedBy, "ann") {
			t.Fatalf("claimed_by = %v", claimed.ClaimedBy)
		}
		type reminder struct {
			Cards []card `json:"claimed_without_comment"`
		}
		before := decode[reminder](t, call("remind", ""))
		if len(before.Cards) != 1 || before.Cards[0].Ref != annCard.Ref {
			t.Fatalf("remind before the comment = %+v, want %s", before, annCard.Ref)
		}
		call("card_write", `{"action":"comment","ref":"`+annCard.Ref+`","body":"started on the rotation script"}`)
		after := decode[reminder](t, call("remind", ""))
		if len(after.Cards) != 0 {
			t.Fatalf("remind after the comment = %+v, want no cards", after)
		}
	})

	bob := func() *mcp.ClientSession {
		c := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "v0"}, nil)
		cs, err := c.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint: base + "/mcp",
			HTTPClient: &http.Client{Transport: headerTransport{
				"Authorization": "Bearer " + tokens["bob"], "X-Trellis-Project": "APP",
			}},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}()
	const slug = "backup-rotation"
	var bobCard card

	t.Run("e MCP client as bob writes, publishes and nominates", func(t *testing.T) {
		list, err := bob.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range list.Tools {
			names = append(names, tool.Name)
		}
		for _, want := range []string{"remind", "coordination", "card_write", "vault_share"} {
			if !slices.Contains(names, want) {
				t.Fatalf("tools %v lack %s", names, want)
			}
		}
		bobCard = mcpOK[cardOut](t, bob, "card_write", map[string]any{"action": "new", "title": "rotate the postgres backup credentials"}).Card
		mcpOK[map[string]any](t, bob, "vault_write", map[string]any{
			"action": "new", "slug": slug, "title": "Backup credential rotation",
			"summary": "How we rotate the postgres backup credentials", "body": "Rotate them every 90 days with the vault CLI.",
		})
		published := mcpOK[struct {
			Result entry `json:"result"`
		}](t, bob, "vault_share", map[string]any{"action": "publish", "slug": slug})
		if published.Result.Scope != "project" {
			t.Fatalf("publish scope = %q", published.Result.Scope)
		}
		mcpOK[map[string]any](t, bob, "vault_share", map[string]any{"action": "nominate", "slug": slug, "reason": "every service needs this"})
		mcpOK[map[string]any](t, bob, "vault_write", map[string]any{
			"action": "new", "slug": "deploy-keys", "title": "Deploy keys", "summary": "the deploy key",
			"body": "Deploy notes\n\naws_access_key_id = AKIAZ3MSJV4WWNKPQX7B\n",
		})
		if e := mcpFails(t, bob, "vault_share", map[string]any{"action": "publish", "slug": "deploy-keys"}); e.Code != "gate_failed" {
			t.Fatalf("publish with a secret = %q, want gate_failed", e.Code)
		}
	})

	t.Run("f coordination notices ann about bob", func(t *testing.T) {
		deadline := time.Now().Add(20 * time.Second)
		for {
			brief := decode[struct {
				Notices []notice `json:"notices"`
			}](t, call("board_brief", ""))
			for _, n := range brief.Notices {
				if n.With == "bob" {
					if n.Card == bobCard.Ref {
						t.Fatalf("the notice names bob's card %s: %+v", bobCard.Ref, n)
					}
					if n.Card != annCard.Ref {
						t.Fatalf("the notice card = %s, want ann's %s", n.Card, annCard.Ref)
					}
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("no notice naming bob after 20s: %+v\n%s", brief, serveLog.String())
			}
			time.Sleep(500 * time.Millisecond)
		}
	})

	t.Run("g web login, review and promote", func(t *testing.T) {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		w := &web{t: t, base: base, c: &http.Client{Jar: jar}}
		w.want(http.MethodPost, "/login", map[string]string{"token": tokens["ann-web"]}, http.StatusNoContent)
		me := decode[map[string]string](t, w.want(http.MethodGet, "/api/me", nil, http.StatusOK))
		if me["handle"] != "ann" || me["org"] != "acme" || me["csrf"] == "" {
			t.Fatalf("/api/me = %v", me)
		}
		w.csrf = me["csrf"]
		waiting := decode[struct {
			Nominations []struct {
				Slug string `json:"slug"`
			} `json:"nominations"`
		}](t, w.want(http.MethodGet, "/api/p/APP/nominations", nil, http.StatusOK))
		if len(waiting.Nominations) != 1 || waiting.Nominations[0].Slug != slug {
			t.Fatalf("nominations = %+v", waiting)
		}
		raw := w.want(http.MethodPost, "/api/p/APP/vault/"+slug+"/promote", map[string]string{"reason": "looks right"}, http.StatusForbidden)
		if e := decode[apiErr](t, raw); e.Code != "not_owner" {
			t.Fatalf("ann's promote = %s, want not_owner", raw)
		}
		promoted := mcpOK[struct {
			Result entry `json:"result"`
		}](t, bob, "vault_share", map[string]any{"action": "promote", "slug": slug, "reason": "every service needs this"})
		if promoted.Result.Scope != "global" {
			t.Fatalf("promote scope = %q", promoted.Result.Scope)
		}
		vault := decode[struct {
			Entries []entry `json:"entries"`
		}](t, w.want(http.MethodGet, "/api/p/APP/vault", nil, http.StatusOK))
		i := slices.IndexFunc(vault.Entries, func(e entry) bool { return e.Slug == slug })
		if i < 0 || vault.Entries[i].Scope != "global" {
			t.Fatalf("ann's vault = %+v, want %s global", vault.Entries, slug)
		}
		res, err := http.Get(base + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		page, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Type"), "text/html") || !bytes.Contains(page, []byte("<html")) {
			t.Fatalf("GET / = %d %s %.200s", res.StatusCode, res.Header.Get("Content-Type"), page)
		}
	})

	t.Run("h serve stops cleanly on SIGINT", func(t *testing.T) {
		if serve.Process == nil {
			t.Fatal("serve did not start")
		}
		bob.Close()
		if err := serve.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		select {
		case err := <-served:
			if exit, ok := errors.AsType[*exec.ExitError](err); ok {
				t.Fatalf("serve exited %d\n%s", exit.ExitCode(), serveLog.String())
			}
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatalf("serve did not stop within 20s\n%s", serveLog.String())
		}
		if code := serve.ProcessState.ExitCode(); code != 0 {
			t.Fatalf("exit code %d", code)
		}
	})
}
