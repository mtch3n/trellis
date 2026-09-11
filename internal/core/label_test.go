package core

import (
	"errors"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestCreateLabel(t *testing.T) {
	tests := []struct {
		name    string
		labels  []struct{ name, desc string }
		wantErr bool
		errCode string
	}{
		{
			name:    "create single label",
			labels:  []struct{ name, desc string }{{"bug", "A defect"}},
			wantErr: false,
		},
		{
			name: "create multiple labels",
			labels: []struct{ name, desc string }{
				{"bug", "A defect"},
				{"feature", "New capability"},
			},
			wantErr: false,
		},
		{
			name: "reject duplicate label in same project",
			labels: []struct{ name, desc string }{
				{"bug", "A defect"},
				{"bug", "Another defect"},
			},
			wantErr: true,
			errCode: "label_exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testCore(t)
			p := seededProject(t, c)
			ctx := t.Context()

			for i, label := range tt.labels {
				_, err := c.CreateLabel(ctx, p.ID, label.name, label.desc)
				if i < len(tt.labels)-1 && err != nil {
					t.Errorf("failed to create label %d: %v", i, err)
					return
				}

				if i == len(tt.labels)-1 {
					if (err != nil) != tt.wantErr {
						t.Errorf("wantErr %v, got %v", tt.wantErr, err)
						return
					}
					if tt.wantErr && err != nil {
						if e, ok := errors.AsType[*Error](err); !ok || e.Code != tt.errCode {
							t.Errorf("wantCode %q, got %v", tt.errCode, err)
						}
					}
				}
			}
		})
	}
}

func TestDeleteLabelInUse(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Create a label
	label, err := c.CreateLabel(ctx, p.ID, "bug", "A defect")
	if err != nil {
		t.Fatalf("failed to create label: %v", err)
	}

	// Create a card
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Test card",
	})
	if err != nil {
		t.Fatalf("failed to create card: %v", err)
	}

	// Add the label to the card
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		return c.AddCardLabel(tx, card.ID, p.ID, "bug")
	})
	if err != nil {
		t.Fatalf("failed to add label to card: %v", err)
	}

	// Try to delete the label; should fail with "in use" error
	err = c.DeleteLabel(ctx, p.ID, "bug")
	if err == nil {
		t.Errorf("expected error when deleting label in use, got nil")
		return
	}

	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "label_in_use" {
		t.Errorf("expected label_in_use error, got %v", err)
	}

	// Verify label still exists
	labels, err := c.ListLabels(ctx, p.ID)
	if err != nil {
		t.Fatalf("failed to list labels: %v", err)
	}
	if len(labels) != 1 || labels[0].ID != label.ID {
		t.Errorf("expected label to still exist")
	}
}

func TestMergeLabel(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Create two labels
	_, err := c.CreateLabel(ctx, p.ID, "bug", "A defect")
	if err != nil {
		t.Fatalf("failed to create 'bug' label: %v", err)
	}
	_, err = c.CreateLabel(ctx, p.ID, "chore", "Maintenance")
	if err != nil {
		t.Fatalf("failed to create 'chore' label: %v", err)
	}

	// Create two cards
	card1, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Bug card",
	})
	if err != nil {
		t.Fatalf("failed to create card 1: %v", err)
	}

	card2, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Another bug",
	})
	if err != nil {
		t.Fatalf("failed to create card 2: %v", err)
	}

	// Add "bug" label to both cards
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.AddCardLabel(tx, card1.ID, p.ID, "bug"); err != nil {
			return err
		}
		return c.AddCardLabel(tx, card2.ID, p.ID, "bug")
	})
	if err != nil {
		t.Fatalf("failed to add labels: %v", err)
	}

	// Merge "bug" into "chore"
	err = c.MergeLabel(ctx, p.ID, "bug", "chore")
	if err != nil {
		t.Fatalf("failed to merge labels: %v", err)
	}

	// Verify "bug" label is gone
	labels, err := c.ListLabels(ctx, p.ID)
	if err != nil {
		t.Fatalf("failed to list labels: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "chore" {
		t.Errorf("expected only 'chore' label to remain, got %v", labels)
	}

	// Verify both cards now have "chore" label
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		labels1, err := c.GetCardLabels(tx, card1.ID)
		if err != nil {
			return err
		}
		labels2, err := c.GetCardLabels(tx, card2.ID)
		if err != nil {
			return err
		}

		if len(labels1) != 1 || labels1[0].Name != "chore" {
			t.Errorf("card1: expected 'chore' label, got %v", labels1)
		}
		if len(labels2) != 1 || labels2[0].Name != "chore" {
			t.Errorf("card2: expected 'chore' label, got %v", labels2)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to verify merged labels: %v", err)
	}
}

func TestAddCardLabelUnknown(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Create a card
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Test card",
	})
	if err != nil {
		t.Fatalf("failed to create card: %v", err)
	}

	// Try to add unknown label
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		return c.AddCardLabel(tx, card.ID, p.ID, "unknown")
	})

	if err == nil {
		t.Errorf("expected error when adding unknown label, got nil")
		return
	}

	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "label_not_found" {
		t.Errorf("expected label_not_found error, got %v", err)
	}

	// Verify error message mentions the project key
	if e, ok := errors.AsType[*Error](err); ok && !contains(e.Msg, p.Key) {
		t.Errorf("error message should mention project key %q, got: %s", p.Key, e.Msg)
	}
}

func TestTagAutoCreate(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Create a card
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Test card",
	})
	if err != nil {
		t.Fatalf("failed to create card: %v", err)
	}

	// Add tag; should auto-create
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		return c.AddCardTag(tx, card.ID, p.ID, "sprint-12")
	})
	if err != nil {
		t.Fatalf("failed to add tag: %v", err)
	}

	// Verify tag was created
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		tags, err := c.GetCardTags(tx, card.ID)
		if err != nil {
			return err
		}
		if len(tags) != 1 || tags[0].Name != "sprint-12" {
			t.Errorf("expected tag 'sprint-12', got %v", tags)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to verify tag: %v", err)
	}
}

func TestTagDeduplication(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Create a card
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Test card",
	})
	if err != nil {
		t.Fatalf("failed to create card: %v", err)
	}

	// Add tag twice; second should be a no-op
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.AddCardTag(tx, card.ID, p.ID, "sprint-12"); err != nil {
			return err
		}
		return c.AddCardTag(tx, card.ID, p.ID, "sprint-12")
	})
	if err != nil {
		t.Fatalf("failed to add tag twice: %v", err)
	}

	// Verify tag appears only once
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		tags, err := c.GetCardTags(tx, card.ID)
		if err != nil {
			return err
		}
		if len(tags) != 1 {
			t.Errorf("expected 1 tag, got %d", len(tags))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to verify tag: %v", err)
	}
}

func TestSeedDefaultLabels(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	ctx := t.Context()

	// Seed labels
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return c.SeedDefaultLabels(tx, p.ID)
	})
	if err != nil {
		t.Fatalf("failed to seed labels: %v", err)
	}

	// Verify all 8 labels exist
	labels, err := c.ListLabels(ctx, p.ID)
	if err != nil {
		t.Fatalf("failed to list labels: %v", err)
	}

	expectedLabels := []string{"bug", "feature", "chore", "docs", "research", "blocked", "needs-review", "question"}
	if len(labels) != 8 {
		t.Errorf("expected 8 labels, got %d", len(labels))
	}

	labelNames := make(map[string]bool)
	for _, l := range labels {
		labelNames[l.Name] = true
	}

	for _, exp := range expectedLabels {
		if !labelNames[exp] {
			t.Errorf("expected label %q to exist", exp)
		}
	}

	// Verify descriptions
	for _, label := range labels {
		if label.Description == "" {
			t.Errorf("label %q has empty description", label.Name)
		}
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
