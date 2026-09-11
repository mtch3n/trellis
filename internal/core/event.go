package core

import "github.com/jmoiron/sqlx"

// recordEvent appends to the monotonic change feed inside the caller's
// transaction. Every mutation records one, from P0: the event log is also the
// cursor a future sync extension reads, and history cannot be backfilled.
func (c *Core) recordEvent(tx *sqlx.Tx, entityType, entityID, action, field, oldV, newV string) error {
	const q = `INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value, new_value)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := tx.Exec(q, c.clock.NowMS(), c.actor, entityType, entityID, action,
		nullIfEmpty(field), nullIfEmpty(oldV), nullIfEmpty(newV))
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
