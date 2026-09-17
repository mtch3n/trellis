package cli

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// review-http-events #9: the daemon's Core never received SetLeaseTTL,
// SetDefaultColumns or SetCardRequirements from the global config, so a web
// claim always got the built-in 30-minute lease and web card creation
// ignored tags.require_on_card / labels.require_on_card, no matter what the
// config file said. This drives a real daemon in-process, the way
// TestDaemonLifecycle drives one out-of-process, and checks both settings
// over the actual HTTP API.
func TestDaemonAppliesGlobalLeaseTTLAndCardRequirements(t *testing.T) {
	if testing.Short() {
		t.Skip("spins up a real HTTP server")
	}
	root := t.TempDir()
	t.Setenv("TRELLIS_HOME", root)
	t.Setenv("TRELLIS_PROJECT", "")

	write(t, filepath.Join(root, "config.yaml"), "lease:\n  ttl: 5h\ntags:\n  require_on_card: true\n")

	// createProject also seeds its own default board (named after the key,
	// lower-cased), alongside the "main" board seedProject asks for here, so
	// the card below is created on "main" by slug, not by list position.
	p := seedProject(t, "TEST", "main")
	func() {
		path, err := home.DBPath()
		if err != nil {
			t.Fatal(err)
		}
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		c := core.New(db, core.RealClock{}, "test", root)
		board, err := c.BoardBySlug(t.Context(), p.ID, "main")
		if err != nil {
			t.Fatalf("BoardBySlug(main): %v", err)
		}
		// requireTags is not primed yet at this point (that only happens once
		// the daemon starts), so this seed card needs no tag.
		if _, err := c.CreateCard(t.Context(), p.ID, board.ID, core.NewCard{Title: "seed"}); err != nil {
			t.Fatalf("CreateCard: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- runApplicationServerContext(ctx, "127.0.0.1", 0) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-daemonDone:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})

	if err := waitForDaemon(ctx, root, true); err != nil {
		t.Fatalf("daemon never answered: %v", err)
	}
	rawURL, healthy := daemonHealth(ctx, root)
	if !healthy || rawURL == "" {
		t.Fatalf("health = %q, %v", rawURL, healthy)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse daemon url %q: %v", rawURL, err)
	}
	base := "http://" + u.Host
	token := u.Query().Get("token")
	if token == "" {
		t.Fatalf("daemon url %q carries no token", rawURL)
	}

	call := func(method, path string, body []byte) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Trellis-Token", token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}

	// The claim must use the configured 5h lease, not the built-in 30m.
	before := time.Now().UnixMilli()
	status, body := call("POST", "/api/p/TEST/b/main/cards/TEST-1/claim", []byte("{}"))
	if status != http.StatusOK {
		t.Fatalf("claim: status %d, body %s", status, body)
	}
	var claimed struct {
		LeaseUntil *int64 `json:"lease_until"`
	}
	if err := json.Unmarshal(body, &claimed); err != nil || claimed.LeaseUntil == nil {
		t.Fatalf("claim body = %s: %v", body, err)
	}
	got := time.Duration(*claimed.LeaseUntil-before) * time.Millisecond
	if got < 4*time.Hour || got > 6*time.Hour {
		t.Errorf("claim lease = %v, want ~5h from the daemon's config", got)
	}

	// Card creation must honor tags.require_on_card.
	status, body = call("POST", "/api/p/TEST/b/main/cards", []byte(`{"title":"no tags"}`))
	if status != http.StatusBadRequest || !strings.Contains(string(body), `"code":"tag_required"`) {
		t.Errorf("card create without a tag: status %d, body %s, want 400 tag_required", status, body)
	}
}
