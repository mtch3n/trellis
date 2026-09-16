package core

import (
	"context"
	"errors"
	"strconv"

	"github.com/jmoiron/sqlx"
)

// ImportCard is one entry in the JSON array `card import` reads. ID is a
// caller-chosen local handle used only to express ordering inside this import:
// an implementation plan is a list of steps where step 3 waits on step 2, and
// the real refs do not exist until the cards are written.
type ImportCard struct {
	ID        string   `json:"id,omitempty"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Column    string   `json:"column,omitempty"`
	Priority  string   `json:"priority,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}

// ImportCards writes a whole plan in one transaction: either the board gains
// every card or it gains none, so a rejected label halfway down does not leave
// an agent guessing which half landed.
func (c *Core) ImportCards(ctx context.Context, projectID, boardID string, in []ImportCard) ([]Card, error) {
	if len(in) == 0 {
		return nil, ErrUsage("empty_import", "no cards in the input",
			`echo '[{"title":"first step"}]' | trellis card import --json -`)
	}

	out := make([]Card, 0, len(in))
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		local := map[string]*Card{} // local handle -> written card

		for i, item := range in {
			if item.Title == "" {
				return ErrUsage("missing_title",
					"card "+strconv.Itoa(i+1)+" in the import has no title",
					`each entry needs {"title": "..."}`)
			}
			var prio *Priority
			if item.Priority != "" {
				p, err := ParsePriority(item.Priority)
				if err != nil {
					return err
				}
				prio = &p
			}
			card, err := c.createCard(ctx, tx, projectID, boardID, NewCard{
				Title: item.Title, Body: item.Body, Column: item.Column,
				Priority: prio, Labels: item.Labels, Tags: item.Tags,
			})
			if err != nil {
				return err
			}
			out = append(out, card)
			if item.ID != "" {
				local[item.ID] = &out[len(out)-1]
			}
		}

		// Links are resolved in a second pass: an entry may depend on one
		// written after it.
		for i, item := range in {
			for _, dep := range item.BlockedBy {
				blocker, ok := local[dep]
				if !ok {
					var existing Card
					ref := ParseCardRef(dep)
					if err := c.loadCard(tx, projectID, ref, &existing); err != nil {
						if e, isCore := errors.AsType[*Error](err); isCore && e.Code == "wrong_project" {
							return crossProjectBlock(err, ref)
						}
						return ErrUsage("unknown_blocker",
							"card "+strconv.Itoa(i+1)+" is blocked by "+dep+", which is neither an id in this import nor an existing card",
							`give the blocking entry an "id" and reference it, or pass an existing ref like XPSCTL-12`)
					}
					blocker = &existing
				}
				if blocker.ID == out[i].ID {
					continue
				}
				if _, err := tx.Exec(
					`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
					 VALUES ('card', ?, 'card', ?, ?, NULL, 'blocked_by')`,
					out[i].ID, blocker.ID, blocker.Ref); err != nil {
					return err
				}
				if err := c.recordEvent(tx, "card", out[i].ID, "blocked", "blocked_by", "", blocker.Ref); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
