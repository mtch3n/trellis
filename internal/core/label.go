package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

type Label struct {
	ID          string `db:"id" json:"id"`
	ProjectID   string `db:"project_id" json:"-"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
}

type Tag struct {
	ID        string `db:"id" json:"id"`
	ProjectID string `db:"project_id" json:"-"`
	Name      string `db:"name" json:"name"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
}

// CreateLabel creates a new label in a project. Labels have descriptions and are
// explicitly created; an unknown label on a card is a hard reject.
func (c *Core) CreateLabel(ctx context.Context, projectID, name, description string) (Label, error) {
	var label Label
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Check if label already exists in this project
		var existing Label
		err := tx.Get(&existing, `SELECT * FROM label WHERE project_id = ? AND name = ?`, projectID, name)
		if err == nil {
			return ErrConflict("label_exists",
				fmt.Sprintf("label %q already exists in this project", name),
				"trellis label ls")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		label = Label{
			ID:          NewID(),
			ProjectID:   projectID,
			Name:        name,
			Description: description,
			CreatedAt:   c.clock.NowMS(),
		}
		if _, err := tx.Exec(
			`INSERT INTO label (id, project_id, name, description, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			label.ID, label.ProjectID, label.Name, label.Description, label.CreatedAt); err != nil {
			return err
		}
		return c.recordEvent(tx, "label", label.ID, "created", "", "", label.Name)
	})
	return label, err
}

// ListLabels returns all labels for a project, sorted alphabetically by name.
func (c *Core) ListLabels(ctx context.Context, projectID string) ([]Label, error) {
	var labels []Label
	err := c.db.SelectContext(ctx, &labels,
		`SELECT * FROM label WHERE project_id = ? ORDER BY name`, projectID)
	return labels, err
}

// GetLabel fetches a label by name within a project.
func (c *Core) GetLabel(ctx context.Context, projectID, name string) (Label, error) {
	var label Label
	err := c.db.GetContext(ctx, &label,
		`SELECT * FROM label WHERE project_id = ? AND name = ?`, projectID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return Label{}, ErrNotFound("label_not_found",
			fmt.Sprintf("no label %q in project", name),
			"trellis label ls")
	}
	return label, err
}

// DeleteLabel removes a label. If any cards or entries still use it, returns a
// hard reject with exit 4 and instructions to use merge instead.
func (c *Core) DeleteLabel(ctx context.Context, projectID, name string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		label, err := c.getLabelTx(tx, projectID, name)
		if err != nil {
			return err
		}

		// Check if any cards use this label
		var count int
		if err := tx.Get(&count,
			`SELECT COUNT(*) FROM card_label WHERE label_id = ?`, label.ID); err != nil {
			return err
		}
		if count > 0 {
			return ErrConflict("label_in_use",
				fmt.Sprintf("%d cards still use label %q", count, name),
				fmt.Sprintf("trellis card ls --label %s", name))
		}

		if err := c.recordEvent(tx, "label", label.ID, "deleted", "", label.Name, ""); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM label WHERE id = ?`, label.ID); err != nil {
			return err
		}
		return nil
	})
}

// MergeLabel rewrites all card_label references from 'from' label to 'into' label,
// then deletes the 'from' label, all in one transaction.
func (c *Core) MergeLabel(ctx context.Context, projectID, from, into string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		fromLabel, err := c.getLabelTx(tx, projectID, from)
		if err != nil {
			return err
		}
		toLabel, err := c.getLabelTx(tx, projectID, into)
		if err != nil {
			return err
		}

		// Get all cards with the 'from' label
		var cardIDs []string
		if err := tx.Select(&cardIDs,
			`SELECT card_id FROM card_label WHERE label_id = ?`, fromLabel.ID); err != nil {
			return err
		}

		// For each card, add the 'into' label if not already present, then remove 'from'
		for _, cardID := range cardIDs {
			// Check if card already has the 'into' label
			var exists int
			if err := tx.Get(&exists,
				`SELECT COUNT(*) FROM card_label WHERE card_id = ? AND label_id = ?`,
				cardID, toLabel.ID); err != nil {
				return err
			}

			// Add the 'into' label if not present
			if exists == 0 {
				if _, err := tx.Exec(
					`INSERT INTO card_label (card_id, label_id) VALUES (?, ?)`,
					cardID, toLabel.ID); err != nil {
					return err
				}
			}

			// Remove the 'from' label
			if _, err := tx.Exec(
				`DELETE FROM card_label WHERE card_id = ? AND label_id = ?`,
				cardID, fromLabel.ID); err != nil {
				return err
			}
		}

		// recordEvent looks up its project_id from the label's own row, so it
		// must run before that row is gone -- otherwise the event lands with a
		// NULL project_id and never reaches a project-scoped read.
		if err := c.recordEvent(tx, "label", fromLabel.ID, "merged", "target", fromLabel.Name, toLabel.Name); err != nil {
			return err
		}
		// Delete the 'from' label
		_, err = tx.Exec(`DELETE FROM label WHERE id = ?`, fromLabel.ID)
		return err
	})
}

// getLabelTx fetches a label by name within a project inside a transaction.
func (c *Core) getLabelTx(tx *sqlx.Tx, projectID, name string) (Label, error) {
	var label Label
	err := tx.Get(&label,
		`SELECT * FROM label WHERE project_id = ? AND name = ?`, projectID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return Label{}, ErrNotFound("label_not_found",
			fmt.Sprintf("no label %q in project", name),
			"trellis label ls")
	}
	return label, err
}

// GetCardLabels returns all labels for a card.
func (c *Core) GetCardLabels(tx *sqlx.Tx, cardID string) ([]Label, error) {
	var labels []Label
	err := tx.Select(&labels,
		`SELECT l.* FROM label l
		 JOIN card_label cl ON cl.label_id = l.id
		 WHERE cl.card_id = ?
		 ORDER BY l.name`, cardID)
	return labels, err
}

// AddCardLabel adds a label to a card. If the label does not exist, returns
// a hard reject with exit 3 and suggestions.
func (c *Core) AddCardLabel(tx *sqlx.Tx, cardID, projectID, labelName string) error {
	label, err := c.getLabelTx(tx, projectID, labelName)
	if err != nil {
		// Label not found; return error with available labels
		labels, listErr := c.listLabelsTx(tx, projectID)
		if listErr != nil {
			return err // return original error if we can't list
		}
		names := make([]string, len(labels))
		for i, l := range labels {
			names[i] = l.Name
		}
		slices.Sort(names)
		var project Project
		if err := tx.Get(&project, `SELECT * FROM project WHERE id = ?`, projectID); err == nil {
			available := strings.Join(names, ", ")
			if len(names) > 8 {
				available = strings.Join(names[:8], ", ")
			}
			return ErrNotFound("label_not_found",
				fmt.Sprintf("no label %q in project %s", labelName, project.Key),
				fmt.Sprintf("trellis label new %s --description \"...\"  available: %s", labelName, available))
		}
		return err
	}

	// Check if card already has this label
	var count int
	if err := tx.Get(&count,
		`SELECT COUNT(*) FROM card_label WHERE card_id = ? AND label_id = ?`,
		cardID, label.ID); err != nil {
		return err
	}
	if count > 0 {
		return nil // already has this label, nothing to do
	}

	if _, err := tx.Exec(
		`INSERT INTO card_label (card_id, label_id) VALUES (?, ?)`,
		cardID, label.ID); err != nil {
		return err
	}
	return c.recordEvent(tx, "card", cardID, "labeled", "label", "", labelName)
}

// RemoveCardLabel removes a label from a card.
func (c *Core) RemoveCardLabel(tx *sqlx.Tx, cardID, labelName string) error {
	// Get the label by name; we don't need to validate it exists in the project
	// if we're just removing it
	var labelID string
	err := tx.Get(&labelID,
		`SELECT id FROM label WHERE name = ? AND id IN (
			SELECT label_id FROM card_label WHERE card_id = ?)`,
		labelName, cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // card doesn't have this label, nothing to do
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`DELETE FROM card_label WHERE card_id = ? AND label_id = ?`,
		cardID, labelID); err != nil {
		return err
	}
	return c.recordEvent(tx, "card", cardID, "unlabeled", "label", labelName, "")
}

// listLabelsTx returns all labels for a project within a transaction, sorted alphabetically by name.
func (c *Core) listLabelsTx(tx *sqlx.Tx, projectID string) ([]Label, error) {
	var labels []Label
	err := tx.Select(&labels,
		`SELECT * FROM label WHERE project_id = ? ORDER BY name`, projectID)
	return labels, err
}

// CreateOrGetTag creates a tag if it doesn't exist, or returns the existing tag.
// Tags are free-form and auto-created on first use.
func (c *Core) CreateOrGetTag(tx *sqlx.Tx, projectID, name string) (Tag, error) {
	var tag Tag
	err := tx.Get(&tag,
		`SELECT * FROM tag WHERE project_id = ? AND name = ?`, projectID, name)
	if err == nil {
		return tag, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Tag{}, err
	}

	tag = Tag{
		ID:        NewID(),
		ProjectID: projectID,
		Name:      name,
		CreatedAt: c.clock.NowMS(),
	}
	if _, err := tx.Exec(
		`INSERT INTO tag (id, project_id, name, created_at)
		 VALUES (?, ?, ?, ?)`,
		tag.ID, tag.ProjectID, tag.Name, tag.CreatedAt); err != nil {
		return Tag{}, err
	}
	return tag, nil
}

// GetCardTags returns all tags for a card.
func (c *Core) GetCardTags(tx *sqlx.Tx, cardID string) ([]Tag, error) {
	var tags []Tag
	err := tx.Select(&tags,
		`SELECT t.* FROM tag t
		 JOIN card_tag ct ON ct.tag_id = t.id
		 WHERE ct.card_id = ?
		 ORDER BY t.name`, cardID)
	return tags, err
}

// AddCardTag adds a tag to a card. Tags are auto-created if they don't exist.
func (c *Core) AddCardTag(tx *sqlx.Tx, cardID, projectID, tagName string) error {
	tag, err := c.CreateOrGetTag(tx, projectID, tagName)
	if err != nil {
		return err
	}

	// Check if card already has this tag
	var count int
	if err := tx.Get(&count,
		`SELECT COUNT(*) FROM card_tag WHERE card_id = ? AND tag_id = ?`,
		cardID, tag.ID); err != nil {
		return err
	}
	if count > 0 {
		return nil // already has this tag, nothing to do
	}

	if _, err := tx.Exec(
		`INSERT INTO card_tag (card_id, tag_id) VALUES (?, ?)`,
		cardID, tag.ID); err != nil {
		return err
	}
	return c.recordEvent(tx, "card", cardID, "tagged", "tag", "", tagName)
}

// RemoveCardTag removes a tag from a card.
func (c *Core) RemoveCardTag(tx *sqlx.Tx, cardID, tagName string) error {
	var tagID string
	err := tx.Get(&tagID,
		`SELECT id FROM tag WHERE name = ? AND id IN (
			SELECT tag_id FROM card_tag WHERE card_id = ?)`,
		tagName, cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // card doesn't have this tag, nothing to do
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`DELETE FROM card_tag WHERE card_id = ? AND tag_id = ?`,
		cardID, tagID); err != nil {
		return err
	}
	return c.recordEvent(tx, "card", cardID, "untagged", "tag", tagName, "")
}

// SeedDefaultLabels creates the default preset of 8 labels for a project.
// It will fail if labels already exist.
func (c *Core) SeedDefaultLabels(tx *sqlx.Tx, projectID string) error {
	defaultLabels := []struct {
		name        string
		description string
	}{
		{"bug", "A defect in existing behavior"},
		{"feature", "New capability that does not exist yet"},
		{"chore", "Maintenance, dependencies, cleanup — no behavior change"},
		{"docs", "Documentation or comments only"},
		{"research", "Investigation or spike; the output is an answer, not shipped code"},
		{"blocked", "Cannot proceed until an external dependency resolves"},
		{"needs-review", "Work is done and waiting on a human decision"},
		{"question", "Needs a decision from Ming before work can continue"},
	}

	for _, def := range defaultLabels {
		label := Label{
			ID:          NewID(),
			ProjectID:   projectID,
			Name:        def.name,
			Description: def.description,
			CreatedAt:   c.clock.NowMS(),
		}
		if _, err := tx.Exec(
			`INSERT INTO label (id, project_id, name, description, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			label.ID, label.ProjectID, label.Name, label.Description, label.CreatedAt); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "label", label.ID, "created", "", "", label.Name); err != nil {
			return err
		}
	}
	return nil
}

// seedLabelsIfNone seeds the default labels when the project has none, in the
// caller's transaction, and reports whether it did.
func (c *Core) seedLabelsIfNone(tx *sqlx.Tx, projectID string) (bool, error) {
	var count int
	if err := tx.Get(&count, `SELECT COUNT(*) FROM label WHERE project_id = ?`, projectID); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	return true, c.SeedDefaultLabels(tx, projectID)
}
