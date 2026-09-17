package core

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/store"
)

func testCore(t *testing.T) *Core {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, FixedClock{MS: 1_757_000_000_000}, "test:1", dir)
}

func TestRecordEventIsMonotonic(t *testing.T) {
	c := testCore(t)
	ctx := t.Context()

	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.recordEvent(tx, "card", "a", "created", "", "", ""); err != nil {
			return err
		}
		return c.recordEvent(tx, "card", "a", "moved", "column", "backlog", "review")
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	var seqs []int64
	if err := c.db.Select(&seqs, "SELECT seq FROM event ORDER BY seq"); err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 2 || seqs[0] >= seqs[1] {
		t.Errorf("event seqs = %v, want two strictly increasing values", seqs)
	}

	var actor string
	if err := c.db.Get(&actor, "SELECT actor FROM event LIMIT 1"); err != nil {
		t.Fatal(err)
	}
	if actor != "test:1" {
		t.Errorf("actor = %q, want test:1", actor)
	}
}

func TestTxRollsBackOnError(t *testing.T) {
	c := testCore(t)
	sentinel := errors.New("boom")

	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		if err := c.recordEvent(tx, "card", "a", "created", "", "", ""); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Tx error = %v, want %v", err, sentinel)
	}

	var n int
	if err := c.db.Get(&n, "SELECT count(*) FROM event"); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("event count = %d after rollback, want 0", n)
	}
}

func TestTxRollsBackOnPanic(t *testing.T) {
	c := testCore(t)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected Tx to re-panic")
			}
		}()
		_ = c.Tx(t.Context(), func(tx *sqlx.Tx) error {
			if err := c.recordEvent(tx, "card", "a", "created", "", "", ""); err != nil {
				t.Fatal(err)
			}
			panic("boom")
		})
	}()

	var n int
	if err := c.db.Get(&n, "SELECT count(*) FROM event"); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("event count = %d after panic, want 0", n)
	}

	// The connection must still be usable: a stuck transaction would wedge
	// this process's single connection for every later Tx call.
	err := c.Tx(t.Context(), func(tx *sqlx.Tx) error {
		return c.recordEvent(tx, "card", "b", "created", "", "", "")
	})
	if err != nil {
		t.Fatalf("Tx after recovered panic: %v", err)
	}
	if err := c.db.Get(&n, "SELECT count(*) FROM event"); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("event count = %d after post-panic Tx, want 1", n)
	}
}

func TestErrorCarriesExitCode(t *testing.T) {
	err := ErrConflict("conflict", "card changed since you read it", "trellis card show 12 --json")

	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatal("ErrConflict did not produce a *core.Error")
	}
	if te.Exit != 4 {
		t.Errorf("Exit = %d, want 4", te.Exit)
	}
}
