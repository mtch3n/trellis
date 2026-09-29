package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/store"
)

const day = int64(24 * 60 * 60 * 1000)

// trashEntryAt creates and trashes an entry at ms, returning its trash id
// and the directory its files went to.
func trashEntryAt(t *testing.T, c *Core, p Project, title string, ms int64) (string, string) {
	t.Helper()
	c.clock = FixedClock{MS: ms}
	e, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: title, Body: "x\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEntry(t.Context(), p.ID, e.Slug); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := c.db.Get(&id, `SELECT id FROM trash WHERE item_id = ?`, e.ID); err != nil {
		t.Fatal(err)
	}
	return id, c.trashDir(p.Key, false, id)
}

func TestTrash_TRASH_C13_the_sweep_purges_only_what_is_past_retention(t *testing.T) {
	c, p, _ := vaultCore(t)
	now := int64(1_800_000_000_000)
	oldID, oldDir := trashEntryAt(t, c, p, "Old", now-31*day)
	newID, newDir := trashEntryAt(t, c, p, "New", now-29*day)
	c.clock = FixedClock{MS: now}

	swept, err := c.SweepTrash(t.Context(), 30*24*time.Hour)
	if err != nil || !swept {
		t.Fatalf("SweepTrash = %v, %v; want a sweep", swept, err)
	}

	if n := count(t, c, `SELECT count(*) FROM trash WHERE id = ?`, oldID); n != 0 || onDisk(oldDir) {
		t.Errorf("the 31-day-old item: row %d, files on disk %v; want both gone", n, onDisk(oldDir))
	}
	if n := count(t, c, `SELECT count(*) FROM trash WHERE id = ?`, newID); n != 1 || !onDisk(newDir) {
		t.Errorf("the 29-day-old item: row %d, files on disk %v; want both kept", n, onDisk(newDir))
	}
}

func TestTrash_TRASH_C14_the_sweep_runs_at_most_hourly(t *testing.T) {
	c, p, _ := vaultCore(t)
	now := int64(1_800_000_000_000)
	c.clock = FixedClock{MS: now - 30*60*1000}
	if swept, err := c.SweepTrash(t.Context(), 30*24*time.Hour); err != nil || !swept {
		t.Fatalf("first sweep = %v, %v", swept, err)
	}
	id, _ := trashEntryAt(t, c, p, "Old", now-40*day)

	c.clock = FixedClock{MS: now}
	if swept, err := c.SweepTrash(t.Context(), 30*24*time.Hour); err != nil || swept {
		t.Errorf("a sweep 30 minutes after the last = %v, %v; want none", swept, err)
	}
	if n := count(t, c, `SELECT count(*) FROM trash WHERE id = ?`, id); n != 1 {
		t.Errorf("purged without a sweep")
	}

	c.clock = FixedClock{MS: now + 60*60*1000}
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		RunTrashSweeper(t.Context(), c, func() time.Duration { return 30 * 24 * time.Hour }, tick, nil)
		close(done)
	}()
	tick <- time.Now()
	tick <- time.Now() // the second send waits until the first sweep returned
	close(tick)
	<-done
	if n := count(t, c, `SELECT count(*) FROM trash WHERE id = ?`, id); n != 0 {
		t.Errorf("the daemon's hourly sweep did not purge")
	}
}

func TestTrash_TRASH_C15_two_processes_sweep_once(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	var cores []*Core
	for range 2 {
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		cores = append(cores, New(db, FixedClock{MS: 1_800_000_000_000}, "test:1", dir))
	}

	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	for i, c := range cores {
		wg.Go(func() { results[i], errs[i] = c.SweepTrash(t.Context(), 30*24*time.Hour) })
	}
	wg.Wait()

	if errs[0] != nil || errs[1] != nil || results[0] == results[1] {
		t.Errorf("sweeps = %v, errors = %v; want exactly one sweep and no error", results, errs)
	}
}

func TestTrash_TRASH_C16_an_interrupted_purge_is_finished_by_the_next(t *testing.T) {
	c, p, _ := vaultCore(t)
	now := int64(1_800_000_000_000)
	id, dir := trashEntryAt(t, c, p, "Old", now-40*day)
	// The purge removed the files and stopped before deleting the row.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	c.clock = FixedClock{MS: now}

	if _, err := c.PurgeTrash(t.Context(), now-30*day); err != nil {
		t.Fatalf("PurgeTrash: %v", err)
	}

	if n := count(t, c, `SELECT count(*) FROM trash WHERE id = ?`, id); n != 0 {
		t.Errorf("the row outlived its files")
	}
	left, _ := os.ReadDir(filepath.Join(c.root, "projects", p.Key, ".trash"))
	if len(left) != 0 {
		t.Errorf("files without a row: %v", left)
	}
}
