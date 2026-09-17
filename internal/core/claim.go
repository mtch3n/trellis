package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// ClaimNextCard selects one unclaimed or expired card from the board and claims it
// in a single atomic statement. The selection is ordered by column position (DESC to
// finish what is started), then priority, then rank. Blocked cards are skipped.
// Returns nil card if no work is available (exit 0, not error).
func (c *Core) ClaimNextCard(ctx context.Context, boardID string, ttl int64) (*Card, error) {
	var card Card
	now := c.clock.NowMS()
	if ttl <= 0 {
		ttl = c.claimTTL
	}
	claimUntil := now + ttl

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Fetch the board and project to validate and extract the project ID.
		var projectID string
		if err := tx.Get(&projectID,
			`SELECT project_id FROM board WHERE id = ?`, boardID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("board_not_found", "board not found", "")
			}
			return err
		}

		// The critical statement: select and claim in one IMMEDIATE transaction.
		// With _txlock=immediate, the write lock is held from BEGIN, so no two
		// processes can both succeed on the same card.
		//
		// ORDER BY col.position DESC: finish what is started (don't skip work in
		// progress while backlog is empty). Then priority, then rank.
		//
		// The WHERE clause skips:
		// - Done columns (col.is_done = 0)
		// - Archived cards (c.archived_at IS NULL)
		// - Claimed and unexpired cards ((c.claimed_by IS NULL OR c.claim_until < now))
		// - Cards blocked by unfinished cards (NOT EXISTS blocked_by subquery)
		err := tx.Get(&card,
			`UPDATE card
			 SET claimed_by = ?, claim_until = ?, version = version + 1, updated_at = ?
			 WHERE id = (
			   SELECT c.id FROM card c
			   JOIN column_ col ON col.id = c.column_id
			   WHERE c.project_id = ?
			     AND col.is_done = 0
			     AND c.archived_at IS NULL
			     AND (c.claimed_by IS NULL OR c.claim_until < ?)
			     AND NOT EXISTS (
			       SELECT 1 FROM link l
			       JOIN card b ON b.id = l.to_id
			       WHERE l.from_id = c.id
			         AND l.rel = 'blocked_by'
			         AND b.column_id NOT IN (SELECT id FROM column_ WHERE is_done = 1)
			     )
			   ORDER BY col.position DESC, c.priority, c.rank
			   LIMIT 1
			 )
			 RETURNING id, project_id, board_id, seq, column_id, rank, title, body_md,
			           priority, claimed_by, claim_until, version, created_at, updated_at, archived_at`,
			c.actor, claimUntil, now,
			projectID,
			now)

		if errors.Is(err, sql.ErrNoRows) {
			// No work available; this is success with no card.
			return nil
		}
		if err != nil {
			return err
		}

		// Fill computed fields.
		return c.cardView(tx, &card)
	})

	// Convert "no rows" into nil card (not an error).
	if card.ID == "" {
		return nil, err
	}
	return &card, err
}

// GetNextCard returns the next available card without changing any state.
// Its selection rules intentionally match ClaimNextCard: done and archived
// cards, cards with an active claim, and cards with unfinished blockers are
// skipped. The caller can use this to preview work without claiming it.
func (c *Core) GetNextCard(ctx context.Context, boardID string) (*Card, error) {
	var card Card
	now := c.clock.NowMS()

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var projectID string
		if err := tx.Get(&projectID,
			`SELECT project_id FROM board WHERE id = ?`, boardID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("board_not_found", "board not found", "")
			}
			return err
		}

		err := tx.Get(&card,
			`SELECT c.*
			 FROM card c
			 JOIN column_ col ON col.id = c.column_id
			 WHERE c.board_id = ?
			   AND c.project_id = ?
			   AND col.is_done = 0
			   AND c.archived_at IS NULL
			   AND (c.claimed_by IS NULL OR c.claim_until < ?)
			   AND NOT EXISTS (
			     SELECT 1 FROM link l
			     JOIN card b ON b.id = l.to_id
			     WHERE l.from_id = c.id
			       AND l.rel = 'blocked_by'
			       AND b.column_id NOT IN (SELECT id FROM column_ WHERE is_done = 1)
			   )
			 ORDER BY col.position DESC, c.priority, c.rank
			 LIMIT 1`, boardID, projectID, now)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return c.cardView(tx, &card)
	})

	if errors.Is(err, sql.ErrNoRows) || card.ID == "" {
		return nil, err
	}
	return &card, err
}

// ClaimCard attempts to claim a specific card. Returns a contention error if
// someone else claims it and it has not expired.
func (c *Core) ClaimCard(ctx context.Context, cardID string, ttl int64, steal bool, reason string) (*Card, error) {
	var card Card
	now := c.clock.NowMS()
	if ttl <= 0 {
		ttl = c.claimTTL
	}
	claimUntil := now + ttl

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Fetch the current card state.
		if err := tx.Get(&card,
			`SELECT * FROM card WHERE id = ?`, cardID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("card_not_found", "card not found", "")
			}
			return err
		}

		// Check if someone else holds it and it has not expired.
		if card.ClaimedBy != nil && *card.ClaimedBy != c.actor && (card.ClaimUntil == nil || *card.ClaimUntil > now) {
			if !steal {
				// The claimant is looked up on this transaction's connection:
				// store.Open caps the pool at one, so calling the ctx-level
				// GetAgent here would wait for a connection it is itself
				// holding, and hang rather than fail.
				//
				// An unregistered claimant is still a claimant: the claim is what
				// grants the hold, not the agent row. Refusing with no handle
				// to name is §8.5's "active agent, no handle" case, and letting
				// the claim through instead would hand two agents the same card.
				claimant := Agent{ID: *card.ClaimedBy, Handle: *card.ClaimedBy, LastSeen: now}
				_ = tx.Get(&claimant, `SELECT * FROM agent WHERE id = ?`, *card.ClaimedBy)
				return c.contentionError(claimant)
			} else {
				// Stealing is recorded on the card itself, not only in the
				// event log: the displaced agent finds out by reading the card
				// it thought it claimed.
				if _, err := tx.Exec(
					`INSERT INTO comment (id, card_id, actor, body_md, created_at) VALUES (?, ?, ?, ?, ?)`,
					NewCardID(), cardID, c.actor,
					"claimed from "+*card.ClaimedBy+": "+reason, now); err != nil {
					return err
				}
				if err := c.recordEvent(tx, "card", cardID, "stolen", "claimed_by", *card.ClaimedBy, c.actor); err != nil {
					return err
				}
			}
		}

		// Claim it.
		if _, err := tx.Exec(
			`UPDATE card SET claimed_by = ?, claim_until = ?, version = version + 1, updated_at = ?
			 WHERE id = ?`,
			c.actor, claimUntil, now, cardID); err != nil {
			return err
		}

		if err := c.recordEvent(tx, "card", cardID, "claimed", "", "", ""); err != nil {
			return err
		}

		// Refetch to get the updated state.
		if err := tx.Get(&card, `SELECT * FROM card WHERE id = ?`, cardID); err != nil {
			return err
		}
		return c.cardView(tx, &card)
	})

	if err != nil {
		return nil, err
	}
	return &card, err
}

// ReleaseCard releases a card's claim.
func (c *Core) ReleaseCard(ctx context.Context, cardID string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Verify we own it.
		var claimant *string
		if err := tx.Get(&claimant, `SELECT claimed_by FROM card WHERE id = ?`, cardID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("card_not_found", "card not found", "")
			}
			return err
		}

		if claimant == nil || *claimant != c.actor {
			return ErrConflict("not_owned",
				fmt.Sprintf("you do not claim this card (claimed by %v)", claimant),
				fmt.Sprintf("trellis card show %s", cardID))
		}

		now := c.clock.NowMS()
		if _, err := tx.Exec(
			`UPDATE card SET claimed_by = NULL, claim_until = NULL, version = version + 1, updated_at = ?
			 WHERE id = ?`,
			now, cardID); err != nil {
			return err
		}

		if err := c.recordEvent(tx, "card", cardID, "released", "", "", ""); err != nil {
			return err
		}

		return nil
	})
}

// RenewClaim extends the claim on a card the current actor already claims.
func (c *Core) RenewClaim(ctx context.Context, cardID string, ttl int64) error {
	now := c.clock.NowMS()
	if ttl <= 0 {
		ttl = c.claimTTL
	}
	claimUntil := now + ttl

	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Verify we own it.
		var claimant *string
		if err := tx.Get(&claimant, `SELECT claimed_by FROM card WHERE id = ?`, cardID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound("card_not_found", "card not found", "")
			}
			return err
		}

		if claimant == nil || *claimant != c.actor {
			return ErrConflict("not_owned",
				fmt.Sprintf("you do not claim this card (claimed by %v)", claimant),
				fmt.Sprintf("trellis card show %s", cardID))
		}

		if _, err := tx.Exec(
			`UPDATE card SET claim_until = ? WHERE id = ?`,
			claimUntil, cardID); err != nil {
			return err
		}

		if err := c.recordEvent(tx, "card", cardID, "renewed", "", "", ""); err != nil {
			return err
		}

		return nil
	})
}

// ContentionInfo describes why a card cannot be claimed.
type ContentionInfo struct {
	ClaimedBy         *Agent `json:"holder"`
	RecommendedAction string `json:"recommended_action"`
}

// contentionError builds a conflict response when a card is claimed by another actor.
// humanMS renders an age an agent can act on. "last seen 240000ms ago" needs
// arithmetic before it means anything.
func humanMS(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return d.Round(time.Minute).String()
	default:
		return d.Round(time.Hour).String()
	}
}

func (c *Core) contentionError(claimant Agent) error {
	now := c.clock.NowMS()
	ageMS := now - claimant.LastSeen

	// Decide on action based on claimant state.
	action := "take_another_card"
	if ageMS > 5*60*1000 { // 5 minutes of inactivity
		action = "steal_with_reason"
	}

	contentionInfo := ContentionInfo{
		ClaimedBy:         &claimant,
		RecommendedAction: action,
	}

	return &Error{
		Code:   "contention",
		Msg:    fmt.Sprintf("card claimed by %s (last seen %s ago)", claimant.Handle, humanMS(ageMS)),
		Fix:    "trellis agent ls   # see who has claimed what",
		Exit:   4,
		Detail: contentionInfo,
	}
}
