package core

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/store"
)

// TestCreateNote tests creating a note on a card.
func TestCreateNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "test-actor")
	ctx := context.Background()

	proj, err := core.CreateProject(ctx, "NOTE", false)
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

	// Create a note.
	note, err := core.CreateNote(ctx, card.ID, "test note body")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	if note.CardID != card.ID {
		t.Errorf("CardID = %s, want %s", note.CardID, card.ID)
	}
	if note.BodyMD != "test note body" {
		t.Errorf("BodyMD = %s, want 'test note body'", note.BodyMD)
	}
	if note.Actor != "test-actor" {
		t.Errorf("Actor = %s, want 'test-actor'", note.Actor)
	}

	// Verify card.version was not bumped.
	updatedCard, err := core.GetCard(ctx, proj.ID, CardRef{UUID: card.ID})
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if updatedCard.Version != card.Version {
		t.Errorf("card.Version = %d, want %d (notes should not bump version)", updatedCard.Version, card.Version)
	}
}
