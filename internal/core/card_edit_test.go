package core

import (
	"errors"
	"strings"
	"testing"
)

func ptrOf[T any](v T) *T { return &v }

// Wholesale replacement requires the version the caller read.
func TestEditCardBodyRequiresIfVersion(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"})

	_, err := c.EditCard(t.Context(), p.ID, CardRef{Seq: card.Seq},
		CardEdit{Body: ptrOf("new body")})

	te, ok := errors.AsType[*Error](err)
	if !ok || te.Exit != 2 {
		t.Fatalf("error = %v, want exit 2 demanding --if-version", err)
	}
	if !strings.Contains(te.Fix, "--if-version") {
		t.Errorf("fix = %q, should show the --if-version invocation", te.Fix)
	}
}

func TestEditCardStaleVersionConflicts(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"})

	// Someone else writes first.
	if _, err := c.EditCard(t.Context(), p.ID, CardRef{Seq: card.Seq},
		CardEdit{Body: ptrOf("theirs"), IfVersion: ptrOf(card.Version)}); err != nil {
		t.Fatal(err)
	}

	_, err := c.EditCard(t.Context(), p.ID, CardRef{Seq: card.Seq},
		CardEdit{Body: ptrOf("mine"), IfVersion: ptrOf(card.Version)})

	te, ok := errors.AsType[*Error](err)
	if !ok || te.Exit != 4 {
		t.Fatalf("error = %v, want a conflict with exit 4", err)
	}
	if te.Code != "conflict" {
		t.Errorf("code = %q, want conflict", te.Code)
	}
}

// A delta needs no version, and must not be blocked by one.
func TestEditCardPriorityNeedsNoVersion(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"})

	got, err := c.EditCard(t.Context(), p.ID, CardRef{Seq: card.Seq},
		CardEdit{Priority: ptrOf(PriorityUrgent)})
	if err != nil {
		t.Fatalf("EditCard: %v", err)
	}
	if got.Priority != PriorityUrgent {
		t.Errorf("Priority = %v, want urgent", got.Priority)
	}
}

// Only the fields passed are touched, so an agent's title edit cannot erase a
// body someone changed moments earlier.
func TestEditCardIsPartial(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "keep me", Body: "keep this body"})

	got, err := c.EditCard(t.Context(), p.ID, CardRef{Seq: card.Seq},
		CardEdit{Priority: ptrOf(PriorityHigh)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "keep me" || got.BodyMD != "keep this body" {
		t.Errorf("partial edit changed other fields: %+v", got)
	}
}
