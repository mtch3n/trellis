package platform

import (
	"context"

	"github.com/jmoiron/sqlx"
)

func (p *Platform) UnverifiedEntries(ctx context.Context, pr Principal, project string) ([]Entry, error) {
	var out []Entry
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		out, err = visibleEntries(tx, pr, proj, ` AND e.scope = 'global' AND e.verify_by < now() ORDER BY e.verify_by, e.slug`)
		return err
	})
	return out, err
}
