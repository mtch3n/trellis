package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// EventConsumer is a named, durable cursor into the event feed.
type EventConsumer struct {
	Name      string `db:"name" json:"name"`
	Cursor    int64  `db:"cursor" json:"cursor"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
	UpdatedAt int64  `db:"updated_at" json:"updated_at"`
}

// ConsumerStatus is what `trellis events consumers` lists: name, cursor, how
// far behind the newest event it is, and whether pruning has left a hole it
// has not yet acknowledged.
type ConsumerStatus struct {
	Name   string `json:"name"`
	Cursor int64  `json:"cursor"`
	Lag    int64  `json:"lag"`
	Gap    bool   `json:"gap"`
}

// EnsureEventConsumer returns the named consumer, creating it with cursor 0
// on first use.
func (c *Core) EnsureEventConsumer(ctx context.Context, name string) (EventConsumer, error) {
	var ec EventConsumer
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		now := c.clock.NowMS()
		if _, err := tx.Exec(
			`INSERT INTO event_consumer (name, cursor, created_at, updated_at)
			 VALUES (?, 0, ?, ?)
			 ON CONFLICT(name) DO NOTHING`, name, now, now); err != nil {
			return err
		}
		return tx.Get(&ec, `SELECT * FROM event_consumer WHERE name = ?`, name)
	})
	return ec, err
}

// AckEventConsumer records that name has handled everything up to and
// including seq. It never moves the cursor backwards, and it refuses a seq
// past the newest event: acking work that has not happened yet would let a
// later gap check believe events were handled that never were.
func (c *Core) AckEventConsumer(ctx context.Context, name string, seq int64) (EventConsumer, error) {
	var ec EventConsumer
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var newest int64
		if err := tx.Get(&newest, `SELECT COALESCE(MAX(seq), 0) FROM event`); err != nil {
			return err
		}
		if seq > newest {
			return ErrUsage("seq_too_new",
				"seq is past the newest event", "trellis events consumers")
		}
		now := c.clock.NowMS()
		if _, err := tx.Exec(
			`INSERT INTO event_consumer (name, cursor, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET
			   cursor = MAX(event_consumer.cursor, excluded.cursor),
			   updated_at = ?`,
			name, seq, now, now, now); err != nil {
			return err
		}
		return tx.Get(&ec, `SELECT * FROM event_consumer WHERE name = ?`, name)
	})
	return ec, err
}

// ListEventConsumers lists every consumer with its lag against the newest
// event and whether a prune has left a gap it has not acknowledged past.
func (c *Core) ListEventConsumers(ctx context.Context) ([]ConsumerStatus, error) {
	out := []ConsumerStatus{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var consumers []EventConsumer
		if err := tx.Select(&consumers, `SELECT * FROM event_consumer ORDER BY name`); err != nil {
			return err
		}
		var count int
		var newest, oldest int64
		if err := tx.Get(&count, `SELECT COUNT(*) FROM event`); err != nil {
			return err
		}
		if err := tx.Get(&newest, `SELECT COALESCE(MAX(seq), 0) FROM event`); err != nil {
			return err
		}
		if err := tx.Get(&oldest, `SELECT COALESCE(MIN(seq), 0) FROM event`); err != nil {
			return err
		}
		for _, ec := range consumers {
			out = append(out, ConsumerStatus{
				Name:   ec.Name,
				Cursor: ec.Cursor,
				Lag:    newest - ec.Cursor,
				Gap:    gapExists(ec.Cursor, count, oldest),
			})
		}
		return nil
	})
	return out, err
}

// DeleteEventConsumer removes a named consumer. Removing a name that does
// not exist is not an error.
func (c *Core) DeleteEventConsumer(ctx context.Context, name string) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(`DELETE FROM event_consumer WHERE name = ?`, name)
		return err
	})
}

// gapExists is the one rule EventGapAfter and ListEventConsumers both apply:
// a cursor that has never acked (0) never has a gap, and otherwise there is
// one when every event is gone (count == 0) or the oldest surviving one is
// past what the cursor already saw. If prune has removed every event,
// MIN(seq) has nothing to report and COALESCE would default oldest to 0 --
// indistinguishable from "nothing has ever been pruned; the log starts at
// seq 0" -- so count is checked separately rather than folded into oldest.
func gapExists(cursor int64, count int, oldest int64) bool {
	return cursor > 0 && (count == 0 || oldest > cursor+1)
}

// EventGapAfter reports whether resuming a read from after (a consumer's
// stored cursor) would skip events that maintenance prune already removed.
func (c *Core) EventGapAfter(ctx context.Context, after int64) (gap bool, oldest int64, err error) {
	var count int
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&count, `SELECT COUNT(*) FROM event`); err != nil {
			return err
		}
		return tx.Get(&oldest, `SELECT COALESCE(MIN(seq), 0) FROM event`)
	})
	if err != nil {
		return false, 0, err
	}
	return gapExists(after, count, oldest), oldest, nil
}
