package core

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClaimContentionNamesTheHolderAndStealRecordsWhy(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "contended"})
	if err != nil {
		t.Fatal(err)
	}

	holder := New(c.db, c.clock, "sess:holder")
	if _, err := holder.RegisterAgent(t.Context(), "worker-1", "agent", "/tmp", "host", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	// A second agent must be told who holds it, not left waiting on a
	// connection the transaction is already holding.
	other := New(c.db, c.clock, "sess:other")
	done := make(chan error, 1)
	go func() {
		_, err := other.ClaimCard(t.Context(), card.ID, 60_000, false, "")
		done <- err
	}()
	select {
	case err := <-done:
		var te *Error
		if !errors.As(err, &te) || te.Exit != 4 {
			t.Fatalf("contended claim = %v, want a conflict naming the holder", err)
		}
		if !strings.Contains(te.Msg, "worker-1") {
			t.Errorf("message %q does not name the holder's handle", te.Msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("contended claim hung: a nested transaction is waiting for the only connection")
	}

	stolen, err := other.ClaimCard(t.Context(), card.ID, 60_000, true, "held 4h with no notes")
	if err != nil {
		t.Fatalf("steal: %v", err)
	}
	if stolen.Owner == nil || *stolen.Owner != "sess:other" {
		t.Fatalf("owner = %v, want the stealer", stolen.Owner)
	}
	notes, err := c.GetNotesByCard(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].BodyMD, "held 4h with no notes") {
		t.Errorf("notes = %+v, want the reason recorded where the displaced agent will see it", notes)
	}
}
func TestUnregisteredHolderStillHoldsTheCard(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "held by a stranger"})
	if err != nil {
		t.Fatal(err)
	}
	// No RegisterAgent call: a hook may not have run, but the lease is real.
	holder := New(c.db, c.clock, "sess:unregistered")
	if _, err := holder.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	other := New(c.db, c.clock, "sess:other")
	_, err = other.ClaimCard(t.Context(), card.ID, 60_000, false, "")
	var te *Error
	if !errors.As(err, &te) || te.Exit != 4 {
		t.Fatalf("claim = %v, want a conflict: the lease grants ownership, not the agent row", err)
	}
}
