package core

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/store"
)

func TestClaimedWithoutCommentIgnoresFirstColumnAndCommentedCards(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	inBacklog, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "not started"})
	if err != nil {
		t.Fatal(err)
	}
	silent, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "claimed, nothing written"})
	if err != nil {
		t.Fatal(err)
	}
	commented, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "claimed, wrote it down"})
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []Card{inBacklog, silent, commented} {
		if _, err := c.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, card := range []Card{silent, commented} {
		if _, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{Seq: card.Seq}, "in-progress"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.CreateComment(t.Context(), commented.ID, "what I learned"); err != nil {
		t.Fatal(err)
	}

	claimed, err := c.ClaimedWithoutComment(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != silent.ID {
		var refs []string
		for _, cl := range claimed {
			refs = append(refs, cl.Ref)
		}
		t.Fatalf("ClaimedWithoutComment = %v, want only %s", refs, silent.Ref)
	}
}

// TestRegisterAgent tests agent registration and updates.
func TestRegisterAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "agent-1", filepath.Dir(path))
	ctx := context.Background()

	// Register an agent.
	agent, err := core.RegisterAgent(ctx, "my-handle", "agent", "/home/user/work", "localhost", 12345)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if agent.Handle != "my-handle" {
		t.Errorf("Handle = %s, want 'my-handle'", agent.Handle)
	}
	if agent.Kind != "agent" {
		t.Errorf("Kind = %s, want 'agent'", agent.Kind)
	}
	if agent.PID != 12345 {
		t.Errorf("PID = %d, want 12345", agent.PID)
	}

	// Register again with updated fields.
	agent2, err := core.RegisterAgent(ctx, "my-handle-updated", "hook", "/home/user/work2", "localhost", 12346)
	if err != nil {
		t.Fatalf("RegisterAgent update: %v", err)
	}

	if agent2.ID != agent.ID {
		t.Errorf("ID changed on re-registration: %s -> %s", agent.ID, agent2.ID)
	}
	if agent2.Handle != "my-handle-updated" {
		t.Errorf("Handle not updated: %s, want 'my-handle-updated'", agent2.Handle)
	}
}
