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
