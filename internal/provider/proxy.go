// Package provider connects the web UI's chat to a configured provider. An API
// provider is reached through Proxy, which adds the credential on the way out
// so the key never reaches the browser. A local agent is run by Run.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/mtch3n/trellis/internal/config"
)

// upstream returns p's API root as a URL, the form every request it forwards
// is built on.
func upstream(p config.Provider) (*url.URL, error) {
	raw := strings.TrimRight(p.Upstream(), "/")
	if raw == "" {
		return nil, fmt.Errorf("provider %s has no base URL", p.ID)
	}
	// Azure's v1 API lives under /openai/v1, and the SDK in the browser,
	// seeing a proxy rather than an Azure host, sends bare v1 paths.
	if p.Kind == "azure" && strings.HasSuffix(raw, "/openai") {
		raw += "/v1"
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("provider %s: base URL %q is not an http(s) URL", p.ID, raw)
	}
	return u, nil
}

// credentialHeaders are the headers a provider could read a key from. Every
// one is removed from what the browser sent and the provider's own is set.
var credentialHeaders = []string{"Authorization", "Api-Key", "X-Api-Key", "X-Goog-Api-Key"}

// browserHeaders are the browser's own: its session with Trellis and where
// the request came from. None of them belongs upstream.
var browserHeaders = []string{"Cookie", "X-Trellis-Token", "Origin", "Referer",
	"Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Sec-Fetch-User"}

// authorize sets p's credential on h, in the header p's kind reads.
func authorize(h http.Header, p config.Provider) {
	for _, name := range credentialHeaders {
		h.Del(name)
	}
	key := p.Key()
	if key == "" {
		return
	}
	switch p.Kind {
	case "azure":
		h.Set("Api-Key", key)
	case "anthropic":
		h.Set("X-Api-Key", key)
	case "google":
		h.Set("X-Goog-Api-Key", key)
	default:
		h.Set("Authorization", "Bearer "+key)
	}
}

// Proxy forwards a request for rest (the path under the provider's API root)
// to p, with p's credential. The response streams back as it arrives, so a
// model's server-sent events reach the browser token by token.
func Proxy(p config.Provider, rest string) (http.Handler, error) {
	if kind, ok := config.LookupProviderKind(p.Kind); !ok || kind.Local {
		return nil, fmt.Errorf("provider %s is not an API provider", p.ID)
	}
	target, err := upstream(p)
	if err != nil {
		return nil, err
	}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL.Scheme = target.Scheme
			out.URL.Host = target.Host
			out.URL.Path = target.Path + "/" + strings.TrimLeft(rest, "/")
			out.URL.RawPath = ""
			out.URL.RawQuery = pr.In.URL.RawQuery
			out.Host = target.Host
			for _, name := range browserHeaders {
				out.Header.Del(name)
			}
			authorize(out.Header, p)
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			return nil
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "{\"error\":%q}", "provider unreachable: "+err.Error())
		},
	}, nil
}

// Test checks that p answers: an API provider must list its models with the
// configured credential, and a local agent's program must run.
func Test(ctx context.Context, p config.Provider) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	kind, ok := config.LookupProviderKind(p.Kind)
	if !ok {
		return fmt.Errorf("unknown kind %q", p.Kind)
	}
	if kind.Local {
		out, err := exec.CommandContext(ctx, p.Executable(), "--version").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s --version: %w: %s", p.Executable(), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if kind.NeedsKey && p.Key() == "" {
		return errors.New("no API key is set")
	}
	target, err := upstream(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String()+"/models", nil)
	if err != nil {
		return err
	}
	authorize(req.Header, p)
	if p.Kind == "anthropic" {
		req.Header.Set("Anthropic-Version", "2023-06-01")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s answered %s: %s", target.Host, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
