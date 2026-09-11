package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveHidesCardAndReleasesLease(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "stale work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	got, err := c.ArchiveCard(t.Context(), p.ID, CardRef{Seq: card.Seq})
	if err != nil {
		t.Fatalf("ArchiveCard: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Error("ArchivedAt = nil, want a timestamp")
	}
	if got.Owner != nil {
		t.Errorf("Owner = %v, want nil: archiving releases the lease", *got.Owner)
	}

	cards, err := c.ListCards(t.Context(), b.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Errorf("default listing returned %d cards, want 0", len(cards))
	}
	cards, err = c.ListCards(t.Context(), b.ID, CardFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Errorf("--archived listing returned %d cards, want 1", len(cards))
	}

	if _, err := c.UnarchiveCard(t.Context(), p.ID, CardRef{Seq: card.Seq}); err != nil {
		t.Fatalf("UnarchiveCard: %v", err)
	}
	cards, _ = c.ListCards(t.Context(), b.ID, CardFilter{})
	if len(cards) != 1 {
		t.Errorf("after unarchive: %d cards, want 1", len(cards))
	}
}

func TestBlockedCardIsNotClaimedUntilBlockerIsDone(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	blocker, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "first"})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BlockCard(t.Context(), p.ID, CardRef{Seq: blocked.Seq}, CardRef{Seq: blocker.Seq}); err != nil {
		t.Fatalf("BlockCard: %v", err)
	}

	list, err := c.Blockers(t.Context(), blocked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Ref != blocker.Ref || list[0].Done {
		t.Fatalf("Blockers = %+v, want one unfinished %s", list, blocker.Ref)
	}

	// Only the unblocked card is claimable.
	got, err := c.ClaimNextCard(t.Context(), b.ID, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != blocker.ID {
		t.Fatalf("claimed %v, want the unblocked %s", got, blocker.Ref)
	}
	next, err := c.ClaimNextCard(t.Context(), b.ID, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Errorf("claimed %s, want nothing: the only card left is blocked", next.Ref)
	}

	if err := c.UnblockCard(t.Context(), p.ID, CardRef{Seq: blocked.Seq}, CardRef{Seq: blocker.Seq}); err != nil {
		t.Fatalf("UnblockCard: %v", err)
	}
	after, err := c.ClaimNextCard(t.Context(), b.ID, 60_000)
	if err != nil {
		t.Fatal(err)
	}
	if after == nil || after.ID != blocked.ID {
		t.Errorf("after unblock claimed %v, want %s", after, blocked.Ref)
	}
}

func TestBlockCardRejectsSelf(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.BlockCard(t.Context(), p.ID, CardRef{Seq: card.Seq}, CardRef{Seq: card.Seq})
	if err == nil {
		t.Fatal("BlockCard(self) succeeded, want a usage error")
	}
}

func TestBackupWritesAReadableCopy(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "backed up"}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "copy.db")
	if err := c.Backup(t.Context(), dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
		t.Fatalf("Stat(%s) = %v, %v", dest, fi, err)
	}
}

func TestHeldWithoutNoteIgnoresFirstColumnAndNotedCards(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	inBacklog, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "not started"})
	if err != nil {
		t.Fatal(err)
	}
	silent, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "held, nothing written"})
	if err != nil {
		t.Fatal(err)
	}
	noted, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "held, wrote it down"})
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []Card{inBacklog, silent, noted} {
		if _, err := c.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, card := range []Card{silent, noted} {
		if _, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{Seq: card.Seq}, "in-progress"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.CreateNote(t.Context(), noted.ID, "what I learned"); err != nil {
		t.Fatal(err)
	}

	held, err := c.HeldWithoutNote(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0].ID != silent.ID {
		var refs []string
		for _, h := range held {
			refs = append(refs, h.Ref)
		}
		t.Fatalf("HeldWithoutNote = %v, want only %s", refs, silent.Ref)
	}
}

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
