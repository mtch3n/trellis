package platform

import (
	"context"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Reminder struct {
	Cards   []Card  `json:"claimed_without_comment"`
	Entries []Entry `json:"unnominated_entries"`
}

func (p *Platform) Remind(ctx context.Context, pr Principal, project string) (Reminder, error) {
	out := Reminder{Cards: []Card{}, Entries: []Entry{}}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if pr.IsSystem() {
			return nil
		}
		boards, err := visibleBoards(tx, pr, proj)
		if err != nil {
			return err
		}
		visible := map[uuid.UUID]bool{}
		for _, b := range boards {
			visible[b.ID] = true
		}
		var claimed []cardRow
		if err := tx.Select(&claimed, cardSelect+` WHERE c.project_id = $1 AND c.claimed_member_id = $2 AND c.claim_until > now()
			AND NOT EXISTS (
			  SELECT 1 FROM comment cm WHERE cm.card_id = c.id AND cm.member_id = $2
			    AND cm.created_at >= (SELECT max(ev.created_at) FROM event ev
			      WHERE ev.card_id = c.id AND ev.kind IN ('card.claimed', 'card.stolen')))
			ORDER BY c.number`, proj.ID, pr.MemberID); err != nil {
			return err
		}
		for _, r := range claimed {
			if visible[r.BoardID] {
				out.Cards = append(out.Cards, r.Card)
			}
		}
		return tx.Select(&out.Entries, entrySelect+` WHERE e.project_id = $1 AND e.deleted_at IS NULL AND NOT e.private
			AND (e.scope = 'project' OR (e.scope = 'personal' AND e.owner_id = $2))
			AND EXISTS (SELECT 1 FROM entry_revision r WHERE r.entry_id = e.id AND r.member_id = $2)
			AND NOT EXISTS (SELECT 1 FROM nomination n WHERE n.entry_id = e.id)
			ORDER BY e.slug`, proj.ID, pr.MemberID)
	})
	return out, err
}
