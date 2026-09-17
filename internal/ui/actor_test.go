package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// A write from the browser is a person's. The daemon's own actor carries its
// pid and changes at every restart, which would strand a lease taken in the
// UI: releasing one requires being its owner.
func TestServerWritesAsAHumanNotTheDaemon(t *testing.T) {
	t.Setenv("TRELLIS_AGENT", "")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 6_000_000}, "daemon:4242", dir)
	p, err := c.CreateProject(ctx, "ACTOR", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "default", true); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := request(http.MethodPost, "/api/p/ACTOR/b/default/cards", `{"title":"from the browser"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body)
	}
	rec := request(http.MethodPost, "/api/p/ACTOR/b/default/cards/ACTOR-1/steal", `{"reason":"mine now"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("steal = %d: %s", rec.Code, rec.Body)
	}

	who, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	want := "human:" + who.Username

	var actors []string
	if err := db.Select(&actors, `SELECT DISTINCT actor FROM event WHERE entity_type = 'card'`); err != nil {
		t.Fatal(err)
	}
	if len(actors) != 1 || actors[0] != want {
		t.Fatalf("card events written by %v, want only %q", actors, want)
	}

	// The lease is the reason this matters: its owner must be the same
	// principal the next request arrives as.
	var owner string
	if err := db.Get(&owner, `SELECT claimed_by FROM card WHERE seq = 1`); err != nil {
		t.Fatal(err)
	}
	if owner != want {
		t.Fatalf("lease owner = %q, want %q", owner, want)
	}
}

// A scripted UI keeps the identity it was given.
func TestServerWritesAsTrellisAgentWhenSet(t *testing.T) {
	t.Setenv("TRELLIS_AGENT", "agent:scripted")
	if webActor() != "agent:scripted" {
		t.Fatalf("webActor = %q, want agent:scripted", webActor())
	}
}

// The board list says which board a project opens on, so the web UI lands
// where the CLI does.
func TestServerBoardsSayWhichIsDefault(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 7_000_000}, "ui-boards-test", dir)
	p, err := c.CreateProject(ctx, "BOARDS", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "first", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(ctx, p.ID, "second", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetDefaultBoard(ctx, p.ID, "second"); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/p/BOARDS/boards", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("boards = %d: %s", rec.Code, rec.Body)
	}
	var boards []boardInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &boards); err != nil {
		t.Fatal(err)
	}
	var marked []string
	for _, b := range boards {
		if b.IsDefault {
			marked = append(marked, b.Slug)
		}
	}
	if len(marked) != 1 || marked[0] != "second" {
		t.Fatalf("default boards = %v, want only second", marked)
	}
}
