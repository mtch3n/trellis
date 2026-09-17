package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// rejectContaining is the kind of policy a user would attach; trellis ships none.
type rejectContaining struct{ needle string }

func (p rejectContaining) Name() string { return "reject-" + p.needle }

func (p rejectContaining) Check(_ context.Context, w ProposedWrite) error {
	for _, v := range w.Fields {
		if strings.Contains(v, p.needle) {
			return errors.New("found " + p.needle)
		}
	}
	return nil
}

func TestNoPoliciesByDefault(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "sk-live-anything"}); err != nil {
		t.Fatalf("CreateCard with no policies: %v, want success (§14.1)", err)
	}
}

func TestPolicyRejectsCardNoteAndEdit(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ok, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "clean"})
	if err != nil {
		t.Fatal(err)
	}

	c.WithPolicies(rejectContaining{needle: "SECRET"})

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"card.create", func() error {
			_, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "has SECRET"})
			return err
		}},
		{"card.edit", func() error {
			_, err := c.EditCard(t.Context(), p.ID, CardRef{Seq: ok.Seq},
				CardEdit{Body: ptr("has SECRET"), IfVersion: &ok.Version})
			return err
		}},
		{"note.create", func() error {
			_, err := c.CreateComment(t.Context(), ok.ID, "output with SECRET in it")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			var te *Error
			if !errors.As(err, &te) || te.Exit != 5 {
				t.Fatalf("err = %v, want a policy error with exit 5", err)
			}
		})
	}

	// The rejected writes left nothing behind.
	cards, err := c.ListCards(t.Context(), b.ID, CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Errorf("%d cards, want only the clean one", len(cards))
	}
	notes, err := c.GetCommentsByCard(t.Context(), ok.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("%d notes, want 0: a rejected write rolls back", len(notes))
	}
}

func ptr[T any](v T) *T { return &v }
