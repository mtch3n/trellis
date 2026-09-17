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

// The timeline reads the same feed an extension does: every event carries a
// title, knowledge carries its type, and reads stay out.
func TestProjectEventsComeFromTheFeed(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-events-test").WithKBRoot(t.TempDir())
	ctx := context.Background()
	p, err := c.CreateProject(ctx, "FEED", false)
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
	doc, err := c.CreateKnowledge(ctx, p.ID, core.NewKnowledge{
		Title: "Cache stampede", Template: "finding", Sources: []string{"https://example.com/incident"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadKnowledge(ctx, p.ID, doc.Slug); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/api/p/FEED/events", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got struct {
		Events []core.FeedEvent `json:"events"`
		Next   *int64           `json:"next"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	var sawCard, sawFinding bool
	for _, ev := range got.Events {
		if ev.Action == "read" {
			t.Errorf("a read event reached the timeline: %+v", ev)
		}
		if ev.Kind == "card" && ev.Action == "created" {
			sawCard = true
			if ev.Title != "Ship it" || ev.Ref != "FEED-1" {
				t.Errorf("card event = %+v, want title and ref", ev)
			}
		}
		if ev.Kind == "knowledge" && ev.Action == "created" {
			sawFinding = true
			if ev.Template != "finding" || ev.Title != "Cache stampede" || ev.Ref != doc.Ref {
				t.Errorf("knowledge event = %+v, want template, title and ref", ev)
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
