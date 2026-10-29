package client_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"maps"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
	"github.com/mtch3n/trellis/internal/platform"
	"github.com/mtch3n/trellis/internal/server"
)

var clientBinDir string

func TestMain(m *testing.M) {
	code := m.Run()
	if clientBinDir != "" {
		os.RemoveAll(clientBinDir)
	}
	os.Exit(code)
}

var clientBinary = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "trellis-client-bin-")
	if err != nil {
		return "", err
	}
	clientBinDir = dir
	bin := filepath.Join(dir, "trellis")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/mtch3n/trellis/cmd/trellis").CombinedOutput()
	if err != nil {
		return "", errors.New(string(out))
	}
	return bin, nil
})

type clientServer struct {
	url   string
	token string
	db    *sqlx.DB
}

func clientStart(t *testing.T) clientServer {
	t.Helper()
	db := pgtest.Open(t)
	if err := pgstore.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	p := platform.New(db)
	srv := httptest.NewServer(server.New(p))
	t.Cleanup(srv.Close)
	ctx := t.Context()
	if _, err := p.CreateOrg(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, "acme", "APP", "App"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddMember(ctx, "acme", "ming", "Ming"); err != nil {
		t.Fatal(err)
	}
	if err := p.AddProjectMember(ctx, "acme", "APP", "ming", "member"); err != nil {
		t.Fatal(err)
	}
	token, err := p.IssueToken(ctx, "acme", "ming", "claude-laptop", platform.TokenAgent)
	if err != nil {
		t.Fatal(err)
	}
	return clientServer{url: srv.URL, token: token, db: db}
}

type clientResult struct {
	stdout string
	stderr string
	code   int
}

func clientWorkdir(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if marker != "" {
		if err := os.WriteFile(filepath.Join(dir, ".trellis"), []byte(marker+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func clientRun(t *testing.T, dir string, env map[string]string, args ...string) clientResult {
	t.Helper()
	bin, err := clientBinary()
	if err != nil {
		t.Fatalf("build trellis: %v", err)
	}
	home := env["HOME"]
	if home == "" {
		home = t.TempDir()
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TRELLIS_HOME=" + filepath.Join(home, "trellis-home")}
	for k, v := range env {
		if k != "HOME" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	code := 0
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return clientResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func (s clientServer) env(extra map[string]string) map[string]string {
	env := map[string]string{"TRELLIS_URL": s.url, "TRELLIS_TOKEN": s.token}
	maps.Copy(env, extra)
	return env
}

type clientCard struct {
	Card platform.Card `json:"card"`
}

func clientDecode[T any](t *testing.T, r clientResult) T {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
	var out T
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("stdout is not the JSON result: %v\n%s", err, r.stdout)
	}
	return out
}

func TestClient_CLIENT_C8_CallCreatesACardOnThePersonalBoardOfTheMarkersProject(t *testing.T) {
	s := clientStart(t)
	dir := clientWorkdir(t, "/APP")
	got := clientDecode[clientCard](t, clientRun(t, dir, s.env(nil), "call", "card_write", `{"action":"new","title":"x"}`))
	if got.Card.Ref != "APP-1" || got.Card.Title != "x" {
		t.Fatalf("card = %+v, want APP-1 titled x", got.Card)
	}
	var row struct {
		Owner string `db:"handle"`
		Scope string `db:"scope"`
		Key   string `db:"key"`
	}
	if err := s.db.Get(&row, `SELECT m.handle, b.scope, p.key FROM card c JOIN board b ON b.id = c.board_id
		JOIN member m ON m.id = b.owner_id JOIN project p ON p.id = c.project_id WHERE c.ref = 'APP-1'`); err != nil {
		t.Fatal(err)
	}
	if row.Owner != "ming" || row.Scope != platform.ScopePersonal || row.Key != "APP" {
		t.Fatalf("card is on %+v, want ming's personal board in APP", row)
	}
}

func TestClient_CLIENT_C9_LoginWritesAPrivateConfigThatCallUses(t *testing.T) {
	s := clientStart(t)
	home := t.TempDir()
	dir := clientWorkdir(t, "")
	env := map[string]string{"HOME": home}
	if r := clientRun(t, dir, env, "login", "--url", s.url, "--token", s.token); r.code != 0 {
		t.Fatalf("login exit %d: %s%s", r.code, r.stdout, r.stderr)
	}
	got := clientDecode[struct {
		Projects []platform.Project `json:"projects"`
	}](t, clientRun(t, dir, env, "call", "project_list"))
	if len(got.Projects) != 1 || got.Projects[0].Key != "APP" {
		t.Fatalf("projects = %+v, want APP", got.Projects)
	}
	info, err := os.Stat(filepath.Join(home, ".config", "trellis", "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("client.json mode = %o, want 600", mode)
	}
}

func TestClient_CLIENT_C10_CallWithNoServerIsAUsageErrorNamingLogin(t *testing.T) {
	r := clientRun(t, clientWorkdir(t, "/APP"), nil, "call", "project_list")
	if r.code != 2 {
		t.Fatalf("exit %d, want 2 (usage)\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "trellis login") {
		t.Fatalf("stderr %q does not name trellis login", r.stderr)
	}
}

func TestClient_CLIENT_C11_ARefusedCallPrintsTheErrorJSONAndExitsNonZero(t *testing.T) {
	s := clientStart(t)
	r := clientRun(t, clientWorkdir(t, "/APP"), s.env(nil), "call", "card_show", `{"ref":"APP-999"}`)
	if r.code == 0 {
		t.Fatalf("exit 0, want non-zero\nstdout: %s", r.stdout)
	}
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Fix     string `json:"fix"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &e); err != nil {
		t.Fatalf("stdout is not {code, message, fix}: %v\n%s", err, r.stdout)
	}
	if e.Code != "card_not_found" || e.Message == "" || e.Fix == "" {
		t.Fatalf("error = %+v, want card_not_found with a message and a fix", e)
	}
}

func TestClient_CLIENT_C12_TheSessionVariableEndsTheClaimant(t *testing.T) {
	s := clientStart(t)
	dir := clientWorkdir(t, "/APP")
	env := s.env(map[string]string{"TRELLIS_SESSION": "s1"})
	card := clientDecode[clientCard](t, clientRun(t, dir, env, "call", "card_write", `{"action":"new","title":"x"}`)).Card
	got := clientDecode[clientCard](t, clientRun(t, dir, env, "call", "card_claim", `{"action":"claim","ref":"`+card.Ref+`"}`)).Card
	if got.ClaimedBy == nil || !strings.HasSuffix(*got.ClaimedBy, "/s1") {
		t.Fatalf("claimant = %v, want one that ends with /s1", got.ClaimedBy)
	}
}
