package platform

import (
	"context"

	"github.com/jmoiron/sqlx"
)

const entryRecallable = `(NOT e.private AND (e.scope <> 'global' OR e.verify_by > now()))`

func (p *Platform) Verify(ctx context.Context, pr Principal, project, slug string) (ShareResult, error) {
	var out ShareResult
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if err := refuseSystem(pr); err != nil {
			return err
		}
		r, err := findShared(tx, pr, proj, slug, []string{"global", ScopeProject, ScopePersonal}, true)
		if err != nil {
			return err
		}
		if r.Scope != "global" {
			return ErrUsage("not_global", "only a global entry is verified; "+slug+" is a "+r.Scope+" entry",
				"vault_share promote "+slug+" first")
		}
		if _, err := tx.Exec(`UPDATE entry SET verify_by = now() + `+verifyInterval+`, verified_at = now()
			WHERE id = $1`, r.ID); err != nil {
			return err
		}
		if err := shareEvent(tx, pr, r, "entry.verified", map[string]any{"slug": slug}); err != nil {
			return err
		}
		out, err = reloadShared(tx, r.ID)
		return err
	})
	return out, err
}
