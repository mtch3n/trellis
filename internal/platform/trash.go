package platform

import (
	"context"
	"uuid"

	"github.com/jmoiron/sqlx"
)

func (p *Platform) DeleteEntry(ctx context.Context, pr Principal, project, slug string) error {
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpWrite, true, false)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE entry SET deleted_at = now(), deleted_by = $2 WHERE id = $1`, e.ID, pr.Actor()); err != nil {
			return err
		}
		return recordEntryEvent(tx, pr, proj, e.ID, "entry.deleted", map[string]any{"slug": e.Slug})
	})
}

func (p *Platform) RestoreEntry(ctx context.Context, pr Principal, project, slug string) (Entry, error) {
	var out Entry
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpWrite, true, true)
		if err != nil {
			return err
		}
		twin, err := liveTwin(tx, e)
		if err != nil {
			return err
		}
		if twin != (uuid.UUID{}) {
			return errEntryExists(e.Slug, twin)
		}
		_, err = tx.Exec(`UPDATE entry SET deleted_at = NULL, deleted_by = NULL WHERE id = $1`, e.ID)
		if isUniqueViolation(err) {
			return errEntryExists(e.Slug, uuid.UUID{})
		}
		if err != nil {
			return err
		}
		if err := fillStubs(tx, e); err != nil {
			return err
		}
		if err := recordEntryEvent(tx, pr, proj, e.ID, "entry.restored", map[string]any{"slug": e.Slug}); err != nil {
			return err
		}
		out, err = loadEntry(tx, pr, proj, e.ID)
		return err
	})
	return out, err
}
