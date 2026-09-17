package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// The timeline reads the same event log an extension does: every event
// carries a title, an entry carries its template, and reads stay out.
func TestProjectEventsComeFromTheEventLog(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-events-test", dir)
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "LOG", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBoard(ctx, p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCard(ctx, p.ID, b.ID, core.NewCard{Title: "Ship it"}); err != nil {
		t.Fatal(err)
	}
	entry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{
		Title: "Cache stampede", Template: "finding", Sources: []string{"https://example.com/incident"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadEntry(ctx, p.ID, entry.Slug); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	req := httptest.NewRequest(http.MethodGet, "/api/p/LOG/events", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got struct {
		Events []core.LogEvent `json:"events"`
		Next   *int64          `json:"next"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	var sawCard, sawFinding bool
	for _, ev := range got.Events {
		if ev.Action == "read" {
			t.Errorf("a read event reached the timeline: %+v", ev)
		}
		if ev.Entity == "card" && ev.Action == "created" {
			sawCard = true
			if ev.Title != "Ship it" || ev.Ref != "LOG-1" {
				t.Errorf("card event = %+v, want title and ref", ev)
			}
		}
		if ev.Entity == "entry" && ev.Action == "created" {
			sawFinding = true
			if ev.Template != "finding" || ev.Title != "Cache stampede" || ev.Ref != entry.Ref {
				t.Errorf("entry event = %+v, want template, title and ref", ev)
			}
		}
	}
	if !sawCard || !sawFinding {
		t.Fatalf("events = %+v, want the card and the finding", got.Events)
	}
	if got.Next == nil || *got.Next != got.Events[len(got.Events)-1].Seq {
		t.Errorf("next = %v, want the last seq", got.Next)
	}
}
