package store

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenAppliesPragmas(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var journal string
	if err := db.Get(&journal, "PRAGMA journal_mode"); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}

	var fk int
	if err := db.Get(&fk, "PRAGMA foreign_keys"); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestOpenCreatesSchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"project", "board", "column_", "card", "event"} {
		var n int
		q := "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?"
		if err := db.Get(&n, q, table); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if n != 1 {
			t.Errorf("table %q missing", table)
		}
	}
}

// Two opens racing to migrate must both succeed; the loser is a no-op.
func TestConcurrentOpenIsSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range 4 {
		wg.Go(func() {
			db, err := Open(path)
			if err == nil {
				db.Close()
			}
			errs[i] = err
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("open %d: %v", i, err)
		}
	}
}

// TestConcurrentOpenAcrossProcesses re-invokes the test binary as several
// separate OS processes, all racing to Open the same brand-new database.
// TestConcurrentOpenIsSafe only races goroutines inside one process, sharing
// one Go runtime; the goose bootstrap race this package guards against with
// the migration file lock is fundamentally cross-process (each process has
// its own connection and its own view of "does the version table exist
// yet"), so only a real multi-process test can catch a regression here.
func TestConcurrentOpenAcrossProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns child processes")
	}

	// Child mode: a single Open, then report success or failure via exit
	// code so the parent can tell without parsing output.
	if path := os.Getenv("TRELLIS_TEST_CHILD"); path != "" {
		db, err := Open(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		db.Close()
		os.Exit(0)
	}

	path := filepath.Join(t.TempDir(), "t.db")
	const children = 6

	var wg sync.WaitGroup
	outs := make([][]byte, children)
	errs := make([]error, children)
	for i := range children {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentOpenAcrossProcesses$")
			cmd.Env = append(os.Environ(), "TRELLIS_TEST_CHILD="+path)
			out, err := cmd.CombinedOutput()
			outs[i] = out
			errs[i] = err
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("child %d exited non-zero: %v\noutput:\n%s", i, err, outs[i])
		}
	}
}

// TestImmediateTxlockPreventsBusySnapshot proves _txlock=immediate is load
// bearing, not cosmetic. Two separate Open handles (two independent
// connections/pools to the same file -- one handle with SetMaxOpenConns(1)
// cannot run two transactions at once) begin transactions against the same
// row; the second writer's BEGIN happens while the first is still open, and
// its write is deliberately held until after the first commits. Under
// _txlock=deferred the second transaction's read snapshot is taken at BEGIN
// and never refreshed, so its later write is rejected outright with
// SQLITE_BUSY_SNAPSHOT the instant the first transaction's commit makes that
// snapshot stale -- the busy handler is never consulted, so busy_timeout
// cannot help. Under _txlock=immediate the second BEGIN blocks for the write
// lock up front and only proceeds once it can see the first transaction's
// commit, so both succeed.
func TestImmediateTxlockPreventsBusySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")

	dbA, err := Open(path)
	if err != nil {
		t.Fatalf("Open A: %v", err)
	}
	defer dbA.Close()

	dbB, err := Open(path)
	if err != nil {
		t.Fatalf("Open B: %v", err)
	}
	defer dbB.Close()

	if _, err := dbA.Exec("CREATE TABLE counter (id INTEGER PRIMARY KEY, v INTEGER NOT NULL)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := dbA.Exec("INSERT INTO counter (id, v) VALUES (1, 0)"); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	ctx := context.Background()

	txA, err := dbA.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin A: %v", err)
	}
	var vA int
	if err := txA.Get(&vA, "SELECT v FROM counter WHERE id = 1"); err != nil {
		t.Fatalf("select A: %v", err)
	}

	// committedA is closed once A has committed. B's writer waits on it so
	// its UPDATE is deliberately attempted after A's commit -- that is what
	// makes a deferred B's earlier read snapshot stale, which is the
	// specific condition SQLITE_BUSY_SNAPSHOT reports.
	committedA := make(chan struct{})
	resultB := make(chan error, 1)
	go func() {
		txB, err := dbB.BeginTxx(ctx, nil)
		if err != nil {
			resultB <- fmt.Errorf("begin B: %w", err)
			return
		}
		var vB int
		if err := txB.Get(&vB, "SELECT v FROM counter WHERE id = 1"); err != nil {
			txB.Rollback()
			resultB <- fmt.Errorf("select B: %w", err)
			return
		}
		<-committedA
		if _, err := txB.Exec("UPDATE counter SET v = ? WHERE id = 1", vB+1); err != nil {
			txB.Rollback()
			resultB <- fmt.Errorf("update B: %w", err)
			return
		}
		resultB <- txB.Commit()
	}()

	// Give B a chance to begin and, under a deferred txlock, fix its read
	// snapshot before A commits. Under an immediate txlock B's BEGIN blocks
	// on A's write lock instead, so this sleep is not load-bearing there.
	time.Sleep(50 * time.Millisecond)

	if _, err := txA.Exec("UPDATE counter SET v = ? WHERE id = 1", vA+1); err != nil {
		t.Fatalf("update A: %v", err)
	}
	if err := txA.Commit(); err != nil {
		t.Fatalf("commit A: %v", err)
	}
	close(committedA)

	select {
	case err := <-resultB:
		if err != nil {
			t.Errorf("second writer did not complete: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second writer (B) did not complete in time")
	}
}
