package core

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/store"
)

// TestCreateComment tests creating a comment on a card.
func TestCreateComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "test-actor", filepath.Dir(path))
	ctx := context.Background()

	proj, err := core.CreateProject(ctx, "COMMENT", false)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	board, err := core.CreateBoard(ctx, proj.ID, "default", true)
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	card, err := core.CreateCard(ctx, proj.ID, board.ID, NewCard{
		Title: "test card",
		Body:  "",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	// Create a comment.
	comment, err := core.CreateComment(ctx, card.ID, "test comment body")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	if comment.CardID != card.ID {
		t.Errorf("CardID = %s, want %s", comment.CardID, card.ID)
	}
	if comment.BodyMD != "test comment body" {
		t.Errorf("BodyMD = %s, want 'test comment body'", comment.BodyMD)
	}
	if comment.Actor != "test-actor" {
		t.Errorf("Actor = %s, want 'test-actor'", comment.Actor)
	}

	// Verify card.version was not bumped.
	updatedCard, err := core.GetCard(ctx, proj.ID, CardRef{UUID: card.ID})
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if updatedCard.Version != card.Version {
		t.Errorf("card.Version = %d, want %d (comments should not bump version)", updatedCard.Version, card.Version)
	}

	// Verify the event was recorded
	events, _, err := core.EventLog(ctx, EventQuery{ProjectID: proj.ID, Entities: []string{"comment"}})
	if err != nil {
		t.Fatalf("EventLog: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("EventLog returned %d events, want 1", len(events))
	} else if events[0].Entity != "comment" {
		t.Errorf("event Entity = %s, want 'comment'", events[0].Entity)
	}
}
