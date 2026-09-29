package core

import (
	"os"
	"path/filepath"
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
