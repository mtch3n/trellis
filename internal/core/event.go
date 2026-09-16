package core

import (
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// recordEvent appends to the monotonic change feed inside the caller's
// transaction. Every mutation records one, from P0: the event log is also the
// cursor a future sync extension reads, and history cannot be backfilled.
// It fills project_id from the entity's row, so a project-scoped feed still
// reaches an entity after it is deleted. Deletions record their event before
// removing the row for that reason.
func (c *Core) recordEvent(tx *sqlx.Tx, entityType, entityID, action, field, oldV, newV string) error {
	var projectID *string
	if q, ok := eventProjectQuery[entityType]; ok {
		if err := tx.Get(&projectID, q, entityID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	const q = `INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value, new_value, project_id)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := tx.Exec(q, c.clock.NowMS(), c.actor, entityType, entityID, action,
		nullIfEmpty(field), nullIfEmpty(oldV), nullIfEmpty(newV), projectID)
	return err
}

// eventProjectQuery finds the project an event's entity belongs to. An entity
// type missing here records no project.
var eventProjectQuery = map[string]string{
	"card":      `SELECT project_id FROM card WHERE id = ?`,
	"knowledge": `SELECT project_id FROM knowledge WHERE id = ?`,
	"board":     `SELECT project_id FROM board WHERE id = ?`,
	"label":     `SELECT project_id FROM label WHERE id = ?`,
	"note":      `SELECT c.project_id FROM note n JOIN card c ON c.id = n.card_id WHERE n.id = ?`,
	"project":   `SELECT id FROM project WHERE id = ?`,
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
