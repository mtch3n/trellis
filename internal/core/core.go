// Package core owns every trellis rule: transactions, events, validation and
// concurrency. The CLI and the HTTP handlers are thin shells over it, so no
// policy may live in a command handler.
package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// Core is the entry point to all trellis operations.
type Core struct {
	db             *sqlx.DB
	clock          Clock
	actor          string
	leaseTTL       int64
	defaultColumns []string
	requireLabels  bool
	requireTags    bool
	// historyKeep is how many revisions each entry and card retains. Default
	// 100; zero disables capture. Negative values are rejected before they
	// ever reach here — see config.ValidateValue and config.Load.
	historyKeep int

	// policies is empty by default; see policy.go and §14.
	policies []Policy

	// root is the storage root every knowledge and artifact file lives
	// under. The caller resolves it (TRELLIS_HOME or the platform default)
	// and injects it here; Core never looks it up itself.
	root string

	// knowledgeChanged is a best-effort derived-state hook. Source writes do
	// not fail when an optional embedding provider is unavailable.
	knowledgeChanged func(context.Context, string) error

	// dropDerivedFn releases derived state that is keyed by a project's key --
	// the vector tables -- before the project is removed or merged away.
	dropDerivedFn func(context.Context, string) error
}

// WithActor returns a copy that writes as another principal. It copies rather
// than mutating the receiver, because one Core is shared by every request a
// server handles and they would otherwise trample each other's identity.
func (c *Core) WithActor(actor string) *Core {
	clone := *c
	clone.actor = actor
	return &clone
}

// New constructs a Core against db, writing events as actor, with every
// knowledge and artifact file rooted under root. root is not optional: an
// empty one is a programming error, not user input, so it panics rather than
// falling back to a default — the fallback is exactly what let tests that
// forgot to isolate themselves write into the caller's real home.
func New(db *sqlx.DB, clock Clock, actor, root string) *Core {
	if root == "" {
		panic("core.New: root must not be empty")
	}
	return &Core{db: db, clock: clock, actor: actor, root: root, leaseTTL: 30 * 60 * 1000, historyKeep: 100}
}

// SetLeaseTTL configures the default lease duration in milliseconds.
func (c *Core) SetLeaseTTL(ttl int64) {
	if ttl > 0 {
		c.leaseTTL = ttl
	}
}

// SetDefaultColumns configures columns seeded for newly created boards.
func (c *Core) SetDefaultColumns(names []string) { c.defaultColumns = append([]string(nil), names...) }

func (c *Core) SetCardRequirements(labels, tags bool) { c.requireLabels, c.requireTags = labels, tags }

// SetHistoryKeep configures how many revisions each entry and card retains.
// A negative value is rejected by config.ValidateValue and config.Load
// before it can reach here; this treats one defensively by leaving the
// current value in place.
func (c *Core) SetHistoryKeep(n int) {
	if n >= 0 {
		c.historyKeep = n
	}
}

func (c *Core) SetKnowledgeChanged(fn func(context.Context, string) error) {
	c.knowledgeChanged = fn
}

func (c *Core) notifyKnowledgeChanged(ctx context.Context, projectID string) {
	if c.knowledgeChanged != nil {
		_ = c.knowledgeChanged(ctx, projectID)
	}
}

func (c *Core) SetDropDerived(fn func(context.Context, string) error) { c.dropDerivedFn = fn }

// dropDerived is best effort: the state it drops is rebuilt on demand, so a
// failure is reported by the caller and never blocks the removal.
func (c *Core) dropDerived(ctx context.Context, projectKey string) error {
	if c.dropDerivedFn == nil {
		return nil
	}
	return c.dropDerivedFn(ctx, projectKey)
}

// Tx runs fn inside one transaction. The DSN sets _txlock=immediate, so the
// write lock is taken up front: a deferred transaction would hand the losing
// writer SQLITE_BUSY_SNAPSHOT immediately, without consulting busy_timeout.
//
// fn may panic. store.Open sets SetMaxOpenConns(1), so this connection is the
// only one this process has: if fn panics and something above Tx recovers
// (a future HTTP handler, a test framework) instead of letting the process
// die, an unrolled-back transaction would wedge every later call to Tx in
// this process and hold SQLite's write lock against every other process
// past busy_timeout, forever. The deferred recover below rolls back first
// and then re-panics, so the panic still propagates but never leaves the
// transaction open.
func (c *Core) Tx(ctx context.Context, fn func(*sqlx.Tx) error) error {
	tx, err := c.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
