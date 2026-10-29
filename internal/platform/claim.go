package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

const DefaultClaimTTL = 30 * time.Minute

type ClaimOpts struct {
	TTL    time.Duration
	Steal  bool
	Reason string
}

func takeClaim(tx *sqlx.Tx, pr Principal, proj projectAccess, boardID, cardID uuid.UUID, ttl time.Duration) (Card, error) {
	if ttl <= 0 {
		ttl = DefaultClaimTTL
	}
	if _, err := tx.Exec(`UPDATE card SET claimed_by = $2, claimed_member_id = $3,
		claim_until = now() + make_interval(secs => $4), version = version + 1, updated_at = now() WHERE id = $1`,
		cardID, pr.Actor(), pr.MemberID, ttl.Seconds()); err != nil {
		return Card{}, err
	}
	if err := recordEvent(tx, pr, proj, boardID, &cardID, "card.claimed", nil); err != nil {
		return Card{}, err
	}
	return getCard(tx, cardID)
}

func (p *Platform) ClaimCard(ctx context.Context, pr Principal, project, ref string, o ClaimOpts) (Card, error) {
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		r, err := loadCard(tx, pr, proj, ref, OpWrite, true)
		if err != nil {
			return err
		}
		if err := checkClaim(r, pr); err != nil {
			if !o.Steal {
				return err
			}
			reason := strings.TrimSpace(o.Reason)
			if reason == "" {
				return ErrUsage("reason_required", "a steal needs a reason", "card_claim claim with steal and a reason")
			}
			if _, err := insertComment(tx, pr, r.ID, "claimed from "+*r.ClaimedBy+": "+reason); err != nil {
				return err
			}
			if err := recordEvent(tx, pr, proj, r.BoardID, &r.ID, "card.stolen", map[string]any{"from": *r.ClaimedBy}); err != nil {
				return err
			}
		}
		card, err = takeClaim(tx, pr, proj, r.BoardID, r.ID, o.TTL)
		return err
	})
	return card, err
}

func (p *Platform) ClaimNext(ctx context.Context, pr Principal, project, board string, ttl time.Duration) (*Card, error) {
	var card *Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		b, err := findBoard(tx, pr, proj, board, OpWrite, false)
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.Get(&id, `SELECT c.id FROM card c JOIN board_column col ON col.id = c.column_id
			WHERE c.board_id = $1 AND NOT col.done
			  AND (c.claim_until IS NULL OR c.claim_until <= now())
			  AND NOT EXISTS (
			    SELECT 1 FROM blocker bl
			    JOIN card bc ON bc.id = bl.blocker_id
			    JOIN board_column bcol ON bcol.id = bc.column_id
			    WHERE bl.card_id = c.id AND NOT bcol.done)
			ORDER BY col.position DESC, c.position
			LIMIT 1
			FOR UPDATE OF c SKIP LOCKED`, b.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		c, err := takeClaim(tx, pr, proj, b.ID, id, ttl)
		card = &c
		return err
	})
	return card, err
}

func loadMyClaim(tx *sqlx.Tx, pr Principal, proj projectAccess, ref string) (cardRow, error) {
	r, err := loadCard(tx, pr, proj, ref, OpWrite, true)
	if err != nil {
		return r, err
	}
	if err := checkClaim(r, pr); err != nil {
		return r, err
	}
	if r.ClaimedBy == nil {
		return r, ErrConflict("not_yours", r.Ref+" is not claimed (moving a card to done releases its claim)", "card_show "+r.Ref)
	}
	if *r.ClaimedBy != pr.Actor() {
		return r, ErrConflict("not_yours", "the claim on "+r.Ref+" has expired", "card_claim claim "+r.Ref)
	}
	return r, nil
}

func (p *Platform) RenewClaim(ctx context.Context, pr Principal, project, ref string, ttl time.Duration) (Card, error) {
	if ttl <= 0 {
		ttl = DefaultClaimTTL
	}
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		r, err := loadMyClaim(tx, pr, proj, ref)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE card SET claim_until = now() + make_interval(secs => $2) WHERE id = $1`,
			r.ID, ttl.Seconds()); err != nil {
			return err
		}
		if err := recordEvent(tx, pr, proj, r.BoardID, &r.ID, "card.renewed", nil); err != nil {
			return err
		}
		card, err = getCard(tx, r.ID)
		return err
	})
	return card, err
}

func (p *Platform) ReleaseClaim(ctx context.Context, pr Principal, project, ref string) (Card, error) {
	var card Card
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		r, err := loadMyClaim(tx, pr, proj, ref)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE card SET claimed_by = NULL, claimed_member_id = NULL, claim_until = NULL,
			version = version + 1, updated_at = now() WHERE id = $1`, r.ID); err != nil {
			return err
		}
		if err := recordEvent(tx, pr, proj, r.BoardID, &r.ID, "card.released", nil); err != nil {
			return err
		}
		card, err = getCard(tx, r.ID)
		return err
	})
	return card, err
}
