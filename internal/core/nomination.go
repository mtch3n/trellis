package core

import (
	"context"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Nomination is an agent's argument that an entry is useful beyond its project.
type Nomination struct {
	Slug        string `db:"slug" json:"slug"`
	Title       string `db:"title" json:"title"`
	Actor       string `db:"actor" json:"actor"`
	Reason      string `db:"reason" json:"reason"`
	Cited       int    `db:"cited" json:"cited"`
	Pinned      int    `db:"pinned" json:"pinned"`
	Reads       int    `db:"reads" json:"reads_30d"`
	Actors      int    `db:"actors" json:"actors"`
	Nominations int    `db:"noms" json:"noms"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
}

// NominateEntry records a nominee for promotion. No threshold blocks
// one: a genuinely new insight can deserve global status immediately, and a gate
// that argues with the nominator is a gate nobody uses (§10.7).
func (c *Core) NominateEntry(ctx context.Context, projectID, slug, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrUsage("missing_reason", "a nomination carries evidence, not just an opinion",
			`trellis knowledge nominate `+slug+` --reason "every repo re-derives this"`)
	}
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var entry Entry
		if err := c.loadEntry(tx, projectID, slug, &entry); err != nil {
			return err
		}
		if entry.Global {
			return ErrUsage("already_global", entry.Slug+" is already global", "trellis knowledge show "+entry.Slug)
		}
		if _, err := tx.Exec(
			`INSERT INTO nomination (id, entry_id, actor, reason, created_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT (entry_id, actor) DO UPDATE SET reason = excluded.reason`,
			NewCardID(), entry.ID, c.actor, reason, c.clock.NowMS()); err != nil {
			return err
		}
		return c.recordEvent(tx, "entry", entry.ID, "nominated", "", "", reason)
	})
}

// Nominations is the nomination queue, ordered by evidence trellis already
// keeps: what was actually read and cited, not what merely exists (§10.7).
func (c *Core) Nominations(ctx context.Context, projectID string) ([]Nomination, error) {
	out := []Nomination{}
	since := c.clock.NowMS() - readWindowMS()
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&out,
			`SELECT k.slug, k.title, n.actor, n.reason, n.created_at,
			        (SELECT COUNT(*) FROM link l WHERE l.to_type = 'entry' AND l.to_id = k.id) AS cited,
			        (SELECT COUNT(*) FROM pin p WHERE p.entry_id = k.id) AS pinned,
			        (SELECT COUNT(*) FROM event e WHERE e.entity_type = 'entry'
			           AND e.entity_id = k.id AND e.action = 'read' AND e.ts > ?) AS reads,
			        (SELECT COUNT(DISTINCT e.actor) FROM event e WHERE e.entity_type = 'entry'
			           AND e.entity_id = k.id AND e.action = 'read' AND e.ts > ?) AS actors,
			        (SELECT COUNT(*) FROM nomination n2 WHERE n2.entry_id = k.id) AS noms
			 FROM nomination n JOIN entry k ON k.id = n.entry_id
			 WHERE k.project_id = ? AND k.global = 0
			 ORDER BY noms DESC, reads DESC, cited DESC, n.created_at`,
			since, since, projectID)
	})
	return out, err
}
