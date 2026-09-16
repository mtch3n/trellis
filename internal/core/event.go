package core

import "github.com/jmoiron/sqlx"

// recordEvent appends to the monotonic change feed inside the caller's
// transaction. Every mutation records one, from P0: the event log is also the
// cursor a future sync extension reads, and history cannot be backfilled.
// It populates project_id by looking up the entity's project from its type,
// allowing project-scoped feeds to reach hard-deleted entities' history.
func (c *Core) recordEvent(tx *sqlx.Tx, entityType, entityID, action, field, oldV, newV string) error {
	// Look up the project_id based on entity_type.
	var projectID *string
	switch entityType {
	case "card":
		// Card has a project_id column directly.
		if err := tx.Get(&projectID, `SELECT project_id FROM card WHERE id = ?`, entityID); err == nil && projectID != nil {
			// Keep projectID
		} else {
			projectID = nil
		}
	case "knowledge":
		// Knowledge has a project_id column directly.
		if err := tx.Get(&projectID, `SELECT project_id FROM knowledge WHERE id = ?`, entityID); err == nil && projectID != nil {
			// Keep projectID
		} else {
			projectID = nil
		}
	case "board":
		// Board has a project_id column directly.
		if err := tx.Get(&projectID, `SELECT project_id FROM board WHERE id = ?`, entityID); err == nil && projectID != nil {
			// Keep projectID
		} else {
			projectID = nil
		}
	case "label":
		// Label has a project_id column directly.
		if err := tx.Get(&projectID, `SELECT project_id FROM label WHERE id = ?`, entityID); err == nil && projectID != nil {
			// Keep projectID
		} else {
			projectID = nil
		}
	case "note":
		// Note's project comes through its card.
		if err := tx.Get(&projectID, `SELECT c.project_id FROM note n LEFT JOIN card c ON c.id = n.card_id WHERE n.id = ?`, entityID); err == nil && projectID != nil {
			// Keep projectID
		} else {
			projectID = nil
		}
	}

	const q = `INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value, new_value, project_id)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := tx.Exec(q, c.clock.NowMS(), c.actor, entityType, entityID, action,
		nullIfEmpty(field), nullIfEmpty(oldV), nullIfEmpty(newV), projectID)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
