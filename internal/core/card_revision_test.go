package core

import (
	"fmt"
	"testing"
)

func cardRevisionCount(t *testing.T, c *Core, cardID string) int {
	t.Helper()
	var n int
	if err := c.db.Get(&n, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, cardID); err != nil {
		t.Fatal(err)
	}
	return n
}

func cardRevisionVersions(t *testing.T, c *Core, cardID string) []int64 {
	t.Helper()
	var versions []int64
	if err := c.db.Select(&versions, `SELECT version FROM card_revision WHERE card_id = ? ORDER BY version`, cardID); err != nil {
		t.Fatal(err)
	}
	return versions
}

func TestCreateCardCapturesVersionOne(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship", Body: "draft"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 1 {
		t.Fatalf("%d card_revision rows after create, want 1", n)
	}
	var title, body string
	if err := c.db.QueryRow(`SELECT title, body_md FROM card_revision WHERE card_id = ? AND version = 1`, card.ID).
		Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	if title != "Ship" || body != "draft" {
		t.Errorf("revision 1 = %q/%q, want Ship/draft", title, body)
	}
}

func TestEditingTitleOrBodyCapturesBothVersions(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship", Body: "v1"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	newBody := "v2"
	edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &card.Version})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	versions := cardRevisionVersions(t, c, card.ID)
	if len(versions) != 2 || versions[0] != card.Version || versions[1] != edited.Version {
		t.Fatalf("versions = %v, want [%d %d]", versions, card.Version, edited.Version)
	}
}

// Moving a card bumps its version without touching title or body, so it must
// not write a revision. The card's next body edit does, numbered with the
// card's version at that time -- which is why versions have gaps.
func TestMovingACardWritesNoRevisionButTheNextEditDoes(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	moved, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{UUID: card.ID}, "review")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	if moved.Version != card.Version+1 {
		t.Fatalf("version = %d, want %d after the move", moved.Version, card.Version+1)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 1 {
		t.Fatalf("%d card_revision rows after a move, want 1 (version 1 only)", n)
	}

	newBody := "after the move"
	edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &moved.Version})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	versions := cardRevisionVersions(t, c, card.ID)
	if len(versions) != 2 || versions[1] != edited.Version {
		t.Fatalf("versions = %v, want the move's version absent and %d present", versions, edited.Version)
	}
}

func TestFirstEditOfAPreexistingCardCapturesItsPriorState(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Old", Body: "v1"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	// Simulate a card that predates this feature: no rows for it yet.
	if _, err := c.db.Exec(`DELETE FROM card_revision WHERE card_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}

	newBody := "v2"
	edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &card.Version})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	versions := cardRevisionVersions(t, c, card.ID)
	if len(versions) != 2 || versions[0] != card.Version || versions[1] != edited.Version {
		t.Fatalf("versions = %v, want [%d %d]", versions, card.Version, edited.Version)
	}
}

func TestEditCardTrimsToHistoryKeep(t *testing.T) {
	c, p, b := kbCore(t)
	c.historyKeep = 3
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Busy", Body: "v1"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	version := card.Version
	for i := 2; i <= 5; i++ {
		body := fmt.Sprintf("v%d", i)
		edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &body, IfVersion: &version})
		if err != nil {
			t.Fatalf("EditCard v%d: %v", i, err)
		}
		version = edited.Version
	}
	if n := cardRevisionCount(t, c, card.ID); n != 3 {
		t.Fatalf("%d card_revision rows, want 3 (keep=3)", n)
	}
}

func TestHistoryKeepZeroCapturesNoCardRevisions(t *testing.T) {
	c, p, b := kbCore(t)
	c.historyKeep = 0
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Off"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 0 {
		t.Fatalf("%d card_revision rows despite historyKeep = 0, want 0", n)
	}
	newBody := "v2"
	if _, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &newBody, IfVersion: &card.Version}); err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 0 {
		t.Fatalf("%d card_revision rows despite historyKeep = 0, want 0", n)
	}
}

func TestDeletingACardRemovesItsRevisions(t *testing.T) {
	c, p, b := kbCore(t)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 1 {
		t.Fatalf("%d card_revision rows before delete, want 1", n)
	}
	if err := c.DeleteCard(t.Context(), p.ID, CardRef{UUID: card.ID}); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}
	if n := cardRevisionCount(t, c, card.ID); n != 0 {
		t.Fatalf("%d card_revision rows after delete, want 0 (ON DELETE CASCADE)", n)
	}
}
