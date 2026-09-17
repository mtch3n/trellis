package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestTUIWorkflowAndConflict(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO project (id, key, name, created_at) VALUES ('p', 'TEST', 'test', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	c := core.New(db, core.RealClock{}, "tui:test")
	b, err := c.CreateBoard(t.Context(), "p", "main", true)
	if err != nil {
		t.Fatal(err)
	}
	s := tuiSession{app: &appCtx{Core: c, Project: core.Project{ID: "p", Key: "TEST"}, Board: b, db: db}, seen: make(map[string]int64)}
	run := func(command string) string {
		t.Helper()
		out, err := s.execute(t.Context(), command)
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		return out
	}
	run("/new A task with spaces")
	run("/show 1")
	card, err := c.GetCard(t.Context(), "p", core.ParseCardRef("1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.EditCard(t.Context(), "p", core.ParseCardRef("1"), core.CardEdit{Title: new("Concurrent edit"), IfVersion: new(card.Version)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.execute(t.Context(), "/title 1 Stale edit"); err == nil {
		t.Fatal("stale edit succeeded")
	}
	run("/show 1")
	run("/title 1 Updated task")
	run(`/body 1 First line\nSecond line`)
	run("/move 1 in-progress")
	run("/comment 1 Work started")
	if out := run("/board"); !strings.Contains(out, "Updated task") {
		t.Fatal(out)
	}
	if out := run("/show 1"); !strings.Contains(out, "First line\nSecond line") || !strings.Contains(out, "in-progress") {
		t.Fatal(out)
	}
	if out := run("Updated"); !strings.Contains(out, "Updated task") {
		t.Fatal(out)
	}
	if _, err := c.CreateBoard(t.Context(), "p", "second", true); err != nil {
		t.Fatal(err)
	}
	if out := run("/board second"); !strings.Contains(out, "0 cards") {
		t.Fatal(out)
	}
	if _, err := s.execute(t.Context(), "/board missing"); err == nil {
		t.Fatal("missing board accepted")
	}
	if s.app.Board.Slug != "second" {
		t.Fatal("failed switch changed board")
	}
	for _, command := range []string{"/new", "/show", "/move 1", "/search", "/bogus"} {
		if _, err := s.execute(t.Context(), command); err == nil {
			t.Errorf("accepted %q", command)
		}
	}
}

func TestTUICompletionAndSafeOutput(t *testing.T) {
	line, pos, ok := completeTUI("/sho", 4, '\t')
	if !ok || line != "/show " || pos != len(line) {
		t.Fatalf("%q %d %v", line, pos, ok)
	}
	if _, _, ok := completeTUI("/bo", 3, '\t'); ok {
		t.Fatal("ambiguous completion")
	}
	if got := safeTerminalText("hello\x1b[2J\r\x00\u009b\u202eworld\n"); strings.ContainsAny(got, "\x1b\r\x00\u009b\u202e") {
		t.Fatalf("unsafe output %q", got)
	}
}
