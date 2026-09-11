package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// Agent represents a running process or harness-managed entity.
type Agent struct {
	ID        string `db:"id" json:"id"`
	Handle    string `db:"handle" json:"handle"`
	Kind      string `db:"kind" json:"kind"`
	CWD       string `db:"cwd" json:"cwd"`
	Host      string `db:"host" json:"host"`
	PID       int64  `db:"pid" json:"pid"`
	FirstSeen int64  `db:"first_seen" json:"first_seen"`
	LastSeen  int64  `db:"last_seen" json:"last_seen"`
}

// RegisterAgent creates or updates an agent record. Called by SessionStart or
// before the first trellis command in a session.
func (c *Core) RegisterAgent(ctx context.Context, handle, kind, cwd, host string, pid int64) (Agent, error) {
	var agent Agent
	now := c.clock.NowMS()

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// Agent ID comes from environment: TRELLIS_AGENT with optional TRELLIS_ACTOR suffix.
		// This makes it a per-session identifier while handle is addressable by the harness.
		agentID := c.actor

		// Try to update if already exists; if not, insert.
		var existsID string
		err := tx.Get(&existsID, `SELECT id FROM agent WHERE id = ?`, agentID)
		if err != nil {
			// Does not exist; insert a new record.
			if _, err := tx.Exec(
				`INSERT INTO agent (id, handle, kind, cwd, host, pid, first_seen, last_seen)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				agentID, handle, kind, cwd, host, pid, now, now); err != nil {
				return err
			}
		} else {
			// Update existing record's last_seen and other fields.
			if _, err := tx.Exec(
				`UPDATE agent SET handle = ?, kind = ?, cwd = ?, host = ?, pid = ?, last_seen = ?
				 WHERE id = ?`,
				handle, kind, cwd, host, pid, now, agentID); err != nil {
				return err
			}
		}

		// Fetch the final state.
		if err := tx.Get(&agent, `SELECT * FROM agent WHERE id = ?`, agentID); err != nil {
			return err
		}
		return nil
	})

	return agent, err
}

// GetAgent fetches an agent by ID.
func (c *Core) GetAgent(ctx context.Context, agentID string) (Agent, error) {
	var agent Agent
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Get(&agent, `SELECT * FROM agent WHERE id = ?`, agentID)
	})
	return agent, err
}

// ListAgents returns all registered agents, ordered by last_seen descending.
func (c *Core) ListAgents(ctx context.Context) ([]Agent, error) {
	var agents []Agent
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&agents, `SELECT * FROM agent ORDER BY last_seen DESC`)
	})
	return agents, err
}

// UpdateAgentLastSeen updates the last_seen timestamp for the current agent.
// Called at the start of every CLI command to measure liveness.
func (c *Core) UpdateAgentLastSeen(ctx context.Context) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(
			`UPDATE agent SET last_seen = ? WHERE id = ?`,
			c.clock.NowMS(), c.actor)
		return err
	})
}

// LogInvocation records one CLI run. Best-effort by contract: the caller
// ignores the error, because a failed metric must never fail a command.
func (c *Core) LogInvocation(ctx context.Context, argv string, exit int, durationMS int64) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO invocation (ts, actor, argv, exit_code, duration_ms) VALUES (?, ?, ?, ?, ?)`,
		c.clock.NowMS(), c.actor, argv, exit, durationMS)
	return err
}

// HeldWithoutNote lists cards this actor holds that carry no note from it. The
// Stop hook reminds once on these: a lease released at session end with nothing
// written down is how the next agent loses what this one learned.
//
// The column test is is_done = 0 AND position > 0, never a column name, because
// columns are configurable.
func (c *Core) HeldWithoutNote(ctx context.Context) ([]Card, error) {
	cards := []Card{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Select(&cards,
			`SELECT c.* FROM card c
			 JOIN column_ col ON col.id = c.column_id
			 WHERE c.owner = ? AND c.lease_until > ?
			   AND c.archived_at IS NULL
			   AND col.is_done = 0 AND col.position > 0
			   AND NOT EXISTS (SELECT 1 FROM note n WHERE n.card_id = c.id AND n.actor = ?)
			 ORDER BY c.updated_at`,
			c.actor, c.clock.NowMS(), c.actor); err != nil {
			return err
		}
		for i := range cards {
			if err := c.cardView(tx, &cards[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return cards, err
}
