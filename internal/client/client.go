package client

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/version"
)

const (
	HeaderProject = "X-Trellis-Project"
	HeaderSession = "X-Trellis-Session"
)

var ErrNoServer = errors.New("no Trellis server is configured")

type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trellis", "client.json"), nil
}

func Load(getenv func(string) string) (Config, error) {
	var saved Config
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &saved); err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return Config{}, err
	}
	cfg := Config{
		URL:   cmp.Or(getenv("TRELLIS_URL"), saved.URL),
		Token: cmp.Or(getenv("TRELLIS_TOKEN"), saved.Token),
	}
	if cfg.URL == "" || cfg.Token == "" {
		return Config{}, ErrNoServer
	}
	return cfg, nil
}

func Save(cfg Config) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".client-*.json")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

type Request struct {
	Project string
	Session string
	Tool    string
	Args    map[string]any
}

type Result struct {
	JSON    []byte
	Refused bool
}

type headers struct {
	set  map[string]string
	base http.RoundTripper
}

func (h headers) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.set {
		r.Header.Set(k, v)
	}
	return h.base.RoundTrip(r)
}

func Call(ctx context.Context, cfg Config, req Request) (Result, error) {
	set := map[string]string{"Authorization": "Bearer " + cfg.Token}
	if req.Project != "" {
		set[HeaderProject] = req.Project
	}
	if req.Session != "" {
		set[HeaderSession] = req.Session
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   strings.TrimSuffix(cfg.URL, "/") + "/mcp",
		HTTPClient: &http.Client{Transport: headers{set: set, base: http.DefaultTransport}},
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "trellis", Version: version.Version}, nil)
	cs, err := c.Connect(ctx, transport, nil)
	if err != nil {
		return Result{}, fmt.Errorf("connect to %s: %w", transport.Endpoint, err)
	}
	defer cs.Close()
	args := req.Args
	if args == nil {
		args = map[string]any{}
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: req.Tool, Arguments: args})
	if err != nil {
		return Result{}, err
	}
	if res.IsError || res.StructuredContent == nil {
		return Result{JSON: []byte(text(res)), Refused: res.IsError}, nil
	}
	raw, err := json.Marshal(res.StructuredContent)
	return Result{JSON: raw}, err
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func ProjectKey(flag string, getenv func(string) string, dir string) (string, error) {
	if key := cmp.Or(strings.TrimSpace(flag), strings.TrimSpace(getenv("TRELLIS_PROJECT"))); key != "" {
		return strings.ToUpper(strings.TrimPrefix(key, "/")), nil
	}
	marker, found, err := resolve.FindMarker(dir)
	if err != nil || !found {
		return "", err
	}
	return marker.Target.Project, nil
}
