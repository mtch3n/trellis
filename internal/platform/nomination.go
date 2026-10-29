package platform

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

const maxWaitingNominations = 10

type Nomination struct {
	Slug        string    `db:"slug" json:"slug"`
	Title       string    `db:"title" json:"title"`
	Actor       string    `db:"actor" json:"actor"`
	Reason      string    `db:"reason" json:"reason"`
	Nominations int       `db:"nominations" json:"nominations"`
	Cited       int       `db:"cited" json:"cited"`
	Pinned      int       `db:"pinned" json:"pinned"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
}

func (p *Platform) Nominate(ctx context.Context, pr Principal, project, slug, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrUsage("reason_required", "a nomination needs a reason", "vault_share nominate "+slug+" with a reason")
	}
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if err := refuseSystem(pr); err != nil {
			return err
		}
		if _, err := tx.Exec(`SELECT 1 FROM project WHERE id = $1 FOR UPDATE`, proj.ID); err != nil {
			return err
		}
		r, err := findEntry(tx, pr, proj, slug, OpRead, false, false)
		if err != nil {
			return err
		}
		switch r.Scope {
		case ScopePersonal:
			return ErrPolicy("not_published", slug+" is personal; only a project entry is nominated",
				"vault_share publish "+slug+" first")
		case "global":
			return ErrUsage("already_global", slug+" is already global", "vault_show "+slug)
		}
		var waiting int
		if err := tx.Get(&waiting, `SELECT count(DISTINCT n.entry_id) FROM nomination n JOIN entry e ON e.id = n.entry_id
			WHERE n.project_id = $1 AND e.scope = 'project' AND e.deleted_at IS NULL AND n.entry_id <> $2`,
			proj.ID, r.ID); err != nil {
			return err
		}
		if waiting >= maxWaitingNominations {
			return ErrPolicy("nomination_queue_full",
				fmt.Sprintf("%d entries already wait for review in %s", waiting, proj.Key),
				"vault_check nominations")
		}
		if _, err := tx.Exec(`INSERT INTO nomination (project_id, entry_id, member_id, actor, reason)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (entry_id, actor) DO UPDATE SET reason = excluded.reason`,
			proj.ID, r.ID, pr.MemberID, pr.Actor(), reason); err != nil {
			return err
		}
		return shareEvent(tx, pr, r, "entry.nominated", map[string]any{"slug": slug})
	})
}

func (p *Platform) Nominations(ctx context.Context, pr Principal, project string) ([]Nomination, error) {
	out := []Nomination{}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		return tx.Select(&out, `SELECT e.slug, e.title, n.actor, n.reason, n.created_at,
			(SELECT count(*) FROM nomination n2 WHERE n2.entry_id = e.id) AS nominations,
			(SELECT count(*) FROM entry_link l WHERE l.to_entry_id = e.id) AS cited,
			(SELECT count(*) FROM pin p WHERE p.entry_id = e.id) AS pinned
			FROM nomination n JOIN entry e ON e.id = n.entry_id
			WHERE n.project_id = $1 AND e.scope = 'project' AND e.deleted_at IS NULL
			ORDER BY nominations DESC, cited DESC, pinned DESC, e.slug, n.created_at`, proj.ID)
	})
	return out, err
}
