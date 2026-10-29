package platform

import (
	"context"
	"encoding/json/v2"
	"time"

	"github.com/jmoiron/sqlx"
)

type Revision struct {
	Version   int64     `db:"version" json:"version"`
	Title     string    `db:"title" json:"title"`
	Summary   string    `db:"summary" json:"summary,omitempty"`
	Body      string    `db:"body" json:"body"`
	Actor     string    `db:"actor" json:"actor"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

func saveRevision(tx *sqlx.Tx, pr Principal, e Entry) error {
	meta, err := json.Marshal(map[string]any{
		"template": e.Template, "provenance": e.Provenance, "private": e.Private,
		"tags": e.Tags, "labels": e.Labels, "sources": e.Sources, "fields": e.Fields,
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO entry_revision (entry_id, version, title, summary, body, meta, actor, member_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.Version, e.Title, e.Summary, e.Body, string(meta), pr.Actor(), pr.MemberID); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM entry_revision WHERE entry_id = $1 AND version <= $2`, e.ID, e.Version-maxRevisions)
	return err
}

func (p *Platform) EntryHistory(ctx context.Context, pr Principal, project, slug string) ([]Revision, error) {
	out := []Revision{}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpRead, false, false)
		if err != nil {
			return err
		}
		return tx.Select(&out, `SELECT version, title, summary, body, actor, created_at FROM entry_revision
			WHERE entry_id = $1 ORDER BY version DESC`, e.ID)
	})
	return out, err
}
