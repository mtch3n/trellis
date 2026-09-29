package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func onDisk(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestTrash_TRASH_C4_a_trashed_entry_moves_to_trash_and_comes_back_whole(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EditEntry(ctx, p.ID, entry.Slug, "v2\n", &entry.Version); err != nil {
		t.Fatal(err)
	}
	edited, err := c.ReadEntry(ctx, p.ID, entry.Slug)
	if err != nil {
		t.Fatal(err)
	}

	if err := c.DeleteEntry(ctx, p.ID, entry.Slug); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if onDisk(entry.Path) || onDisk(revisionDir(entry.Path)) {
		t.Fatalf("the entry or its revisions are still in the vault")
	}
	trashRoot := filepath.Join(c.root, "projects", p.Key, ".trash")
	var files, revs int
	filepath.WalkDir(trashRoot, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if filepath.Base(filepath.Dir(path)) == "."+filepath.Base(entry.Path) {
				revs++
			} else if filepath.Base(path) == filepath.Base(entry.Path) {
				files++
			}
		}
		return nil
	})
	if files != 1 || revs != 2 {
		t.Fatalf("under %s: %d entry files, %d revisions; want 1 and 2", trashRoot, files, revs)
	}

	if _, err := c.RestoreEntry(ctx, p.ID, entry.Slug); err != nil {
		t.Fatalf("RestoreEntry: %v", err)
	}
	back, err := c.ReadEntry(ctx, p.ID, entry.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if back.BodyMD != edited.BodyMD || back.Version != edited.Version {
		t.Errorf("restored body %q version %d; want %q version %d", back.BodyMD, back.Version, edited.BodyMD, edited.Version)
	}
	if history, _ := c.ListEntryRevisions(ctx, p.ID, entry.Slug); len(history) != 2 {
		t.Errorf("history = %d revisions, want 2", len(history))
	}
}

func TestTrash_TRASH_C5_a_failed_trash_puts_the_file_back(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Kept", Body: "v1\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`CREATE TRIGGER refuse BEFORE DELETE ON entry BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}

	if err := c.DeleteEntry(ctx, p.ID, entry.Slug); err == nil {
		t.Fatal("DeleteEntry succeeded past the trigger")
	}

	if !onDisk(entry.Path) || !onDisk(revisionDir(entry.Path)) {
		t.Errorf("the file or its revisions did not come back")
	}
	var rows, trashed int
	c.db.Get(&rows, `SELECT count(*) FROM entry WHERE id = ?`, entry.ID)
	c.db.Get(&trashed, `SELECT count(*) FROM trash`)
	if rows != 1 || trashed != 0 {
		t.Errorf("entry rows = %d, trash rows = %d; want 1 and 0", rows, trashed)
	}
}

func TestTrash_TRASH_C6_lint_health_and_walks_ignore_the_trash(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Kept", Body: "v1\n"}); err != nil {
		t.Fatal(err)
	}
	gone, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Gone", Body: "v1\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEntry(ctx, p.ID, gone.Slug); err != nil {
		t.Fatal(err)
	}
	if err := c.SyncEntrySearch(ctx); err != nil {
		t.Fatal(err)
	}

	diags, err := c.Lint(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if strings.Contains(d.Entry, gone.Slug) || strings.Contains(d.Ref, gone.Slug) {
			t.Errorf("lint reports the trashed entry: %+v", d)
		}
	}
	health, err := c.Health(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range health {
		if (h.What == "entries" && h.Count != 1) || (h.What == "leftover revision directories" && h.Count != 0) {
			t.Errorf("health: %+v", h)
		}
	}
	if n, err := c.LeftoverRevisionsCount(ctx); err != nil || n != 0 {
		t.Errorf("leftover revision directories = %d, %v; want 0", n, err)
	}
	listed, err := c.ListEntries(ctx, p.ID, EntryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range listed {
		if e.Slug == gone.Slug {
			t.Errorf("vault ls lists the trashed entry")
		}
	}
}

func TestTrash_TRASH_C11_restoring_into_a_reused_slug_is_a_conflict(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	old, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Runbook", Body: "old\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEntry(ctx, p.ID, old.Slug); err != nil {
		t.Fatal(err)
	}
	fresh, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Runbook", Body: "new\n"})
	if err != nil || fresh.Slug != old.Slug {
		t.Fatalf("new entry = %q, %v; want slug %q", fresh.Slug, err, old.Slug)
	}
	before, _ := c.ReadEntry(ctx, p.ID, fresh.Slug)

	_, err = c.RestoreEntry(ctx, p.ID, old.Slug)

	if exitOf(err) != 4 || !strings.Contains(err.Error(), old.Slug) {
		t.Fatalf("restore = %v, want exit 4 naming %s", err, old.Slug)
	}
	after, _ := c.ReadEntry(ctx, p.ID, fresh.Slug)
	if after.ID != fresh.ID || after.BodyMD != before.BodyMD {
		t.Errorf("the live entry changed: %+v", after)
	}
	var trashed int
	c.db.Get(&trashed, `SELECT count(*) FROM trash WHERE item_id = ?`, old.ID)
	if trashed != 1 {
		t.Errorf("the trashed entry left the trash")
	}
}

func stubsFrom(t *testing.T, c *Core, projectID, slug string) int {
	t.Helper()
	diags, err := c.Lint(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range diags {
		if d.Kind == "stub" && strings.HasSuffix(d.Entry, "/"+slug) {
			n++
		}
	}
	return n
}

func TestTrash_TRASH_C23_links_into_a_trashed_entry_are_stubs_until_restore(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	b, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Target", Body: "target\n"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Source", Body: "see [[" + b.Slug + "]]\n"})
	if err != nil {
		t.Fatal(err)
	}
	if n := stubsFrom(t, c, p.ID, a.Slug); n != 0 {
		t.Fatalf("stubs before trash = %d", n)
	}

	if err := c.DeleteEntry(ctx, p.ID, b.Slug); err != nil {
		t.Fatal(err)
	}
	if n := stubsFrom(t, c, p.ID, a.Slug); n != 1 {
		t.Errorf("stubs while trashed = %d, want 1", n)
	}
	if _, err := c.RestoreEntry(ctx, p.ID, b.Slug); err != nil {
		t.Fatal(err)
	}
	if n := stubsFrom(t, c, p.ID, a.Slug); n != 0 {
		t.Errorf("stubs after restore = %d, want 0", n)
	}

	if err := c.DeleteEntry(ctx, p.ID, b.Slug); err != nil {
		t.Fatal(err)
	}
	var at int64
	c.db.Get(&at, `SELECT trashed_at FROM trash WHERE item_id = ?`, b.ID)
	if _, err := c.PurgeTrash(ctx, at+1); err != nil {
		t.Fatal(err)
	}
	if n := stubsFrom(t, c, p.ID, a.Slug); n != 1 {
		t.Errorf("stubs after purge = %d, want 1", n)
	}
}

func TestTrash_TRASH_C29_a_new_entry_takes_a_trashed_slugs_links(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	b, _ := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Target", Body: "old\n"})
	a, _ := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Source", Body: "see [[" + b.Slug + "]]\n"})
	if err := c.DeleteEntry(ctx, p.ID, b.Slug); err != nil {
		t.Fatal(err)
	}

	fresh, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Target", Body: "new\n"})
	if err != nil || fresh.Slug != b.Slug {
		t.Fatalf("new entry = %q, %v", fresh.Slug, err)
	}

	var toIDs []string
	c.db.Select(&toIDs, `SELECT COALESCE(to_id, '') FROM link WHERE from_type = 'entry' AND from_id = ? AND to_type = 'entry'`, a.ID)
	if len(toIDs) != 1 || toIDs[0] != fresh.ID {
		t.Errorf("links from the source = %v, want exactly one to %s", toIDs, fresh.ID)
	}
	if n := stubsFrom(t, c, p.ID, a.Slug); n != 0 {
		t.Errorf("stubs = %d, want 0", n)
	}
}
