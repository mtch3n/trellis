package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateEntryCapturesVersionOne(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Standup", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	rev, err := os.ReadFile(revisionFilePath(entry.Path, 1))
	if err != nil {
		t.Fatalf("version 1 was not captured: %v", err)
	}
	onDisk, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(rev) != string(onDisk) {
		t.Errorf("revision 1 = %q, want the entry's own bytes %q", rev, onDisk)
	}
}

func TestEditKeepsBothVersions(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	v1, err := os.ReadFile(revisionFilePath(entry.Path, 1))
	if err != nil {
		t.Fatalf("version 1 missing before the edit: %v", err)
	}

	edited, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "v2\n", &entry.Version)
	if err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	if edited.Version != 2 {
		t.Fatalf("version = %d, want 2", edited.Version)
	}
	v1After, err := os.ReadFile(revisionFilePath(entry.Path, 1))
	if err != nil || string(v1After) != string(v1) {
		t.Errorf("version 1 changed or vanished after the edit: %v", err)
	}
	v2, err := os.ReadFile(revisionFilePath(entry.Path, 2))
	if err != nil {
		t.Fatalf("version 2 was not captured: %v", err)
	}
	onDisk, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(v2) != string(onDisk) {
		t.Errorf("revision 2 = %q, want the entry's current bytes %q", v2, onDisk)
	}
}

// An entry that existed before this feature has no revision directory. Its
// first Trellis write must still capture the version it is about to replace.
func TestFirstEditCapturesAPredatingVersion(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Old", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if err := os.RemoveAll(revisionDir(entry.Path)); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}

	edited, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "v2\n", &entry.Version)
	if err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	pre, err := os.ReadFile(revisionFilePath(entry.Path, 1))
	if err != nil || string(pre) != string(original) {
		t.Errorf("version 1 = %q, %v; want the pre-edit bytes %q", pre, err, original)
	}
	if _, err := os.Stat(revisionFilePath(entry.Path, edited.Version)); err != nil {
		t.Errorf("version %d was not captured: %v", edited.Version, err)
	}
}

// This is the case that motivates copying a version in rather than moving the
// previous one out: a direct edit lands after Trellis has already written a
// version, and that version must not be lost.
func TestADirectEditAfterATrellisWriteKeepsBothVersions(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Race", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	trellisEdit, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "v2 via trellis\n", &entry.Version)
	if err != nil {
		t.Fatalf("EditEntry: %v", err)
	}

	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	direct := RenderEntry(fm, "v3 direct edit\n")
	if err := os.WriteFile(entry.Path, []byte(direct), 0o600); err != nil {
		t.Fatal(err)
	}

	reloaded, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if reloaded.Version != trellisEdit.Version+1 {
		t.Fatalf("version = %d, want %d after the direct edit", reloaded.Version, trellisEdit.Version+1)
	}

	trellisRev, err := os.ReadFile(revisionFilePath(entry.Path, trellisEdit.Version))
	if err != nil {
		t.Fatalf("the trellis-written version was lost: %v", err)
	}
	if !strings.Contains(string(trellisRev), "v2 via trellis") {
		t.Errorf("revision %d = %q, want the trellis-written body", trellisEdit.Version, trellisRev)
	}
	directRev, err := os.ReadFile(revisionFilePath(entry.Path, reloaded.Version))
	if err != nil {
		t.Fatalf("the direct edit was not captured: %v", err)
	}
	if !strings.Contains(string(directRev), "v3 direct edit") {
		t.Errorf("revision %d = %q, want the direct edit's body", reloaded.Version, directRev)
	}
}

func TestRepeatedReadsWriteNoNewRevision(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Stable", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
			t.Fatalf("LoadEntry: %v", err)
		}
	}
	entries, err := os.ReadDir(revisionDir(entry.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d revision files after repeated reads, want 1", len(entries))
	}
}

// The private mirror can drift from the file without the file's bytes
// changing (§ refreshFromFile: a database restored from an older backup, or a
// file that already carried the key when the column was added). That bump
// must not write a revision.
func TestPrivateMirrorDriftWritesNoRevision(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Quiet", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE entry SET private = 1 WHERE id = ?`, entry.ID); err != nil {
		t.Fatal(err)
	}

	reloaded, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if reloaded.Version != entry.Version+1 {
		t.Fatalf("version = %d, want %d: the drift must still bump the version", reloaded.Version, entry.Version+1)
	}
	entries, err := os.ReadDir(revisionDir(entry.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d revision files after a drift-only bump, want 1 (version 1 only)", len(entries))
	}
	if _, err := os.Stat(revisionFilePath(entry.Path, reloaded.Version)); err == nil {
		t.Errorf("a revision was captured for the drift-only version %d", reloaded.Version)
	}
}

// c.historyKeep is unexported: this package's own tests set it directly
// rather than through a setter. Task 4 adds the public SetHistoryKeep, wired
// from config; the field and its enforcement already work without it.
func TestEditTrimsToHistoryKeep(t *testing.T) {
	c, p, _ := vaultCore(t)
	c.historyKeep = 3
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Busy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	version := entry.Version
	for i := 2; i <= 4; i++ {
		edited, err := c.EditEntry(t.Context(), p.ID, entry.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditEntry v%d: %v", i, err)
		}
		version = edited.Version
	}
	entries, err := os.ReadDir(revisionDir(entry.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d revision files, want 3 (keep=3)", len(entries))
	}
	if _, err := os.Stat(revisionFilePath(entry.Path, 1)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("version 1 should have been trimmed")
	}
	for v := int64(2); v <= 4; v++ {
		if _, err := os.Stat(revisionFilePath(entry.Path, v)); err != nil {
			t.Errorf("version %d should be retained: %v", v, err)
		}
	}
}

func TestHistoryKeepZeroCapturesNothing(t *testing.T) {
	c, p, _ := vaultCore(t)
	c.historyKeep = 0
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Off", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := os.Stat(revisionDir(entry.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a revision directory was created despite historyKeep = 0")
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "v2\n", &entry.Version); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	if _, err := os.Stat(revisionDir(entry.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a revision directory was created by an edit despite historyKeep = 0")
	}
}

// A revision write that cannot land must fail the edit and leave the entry
// file exactly as it was: revision capture rides the same undo path as every
// other failure in EditEntryFields.
func TestAFailedRevisionWriteFailsTheEdit(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Blocked", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	original, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Version 1's revision file cannot be (re-)written: a plain file occupies
	// where its directory needs to be.
	if err := os.RemoveAll(revisionDir(entry.Path)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revisionDir(entry.Path), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = c.EditEntry(t.Context(), p.ID, entry.Slug, "v2\n", &entry.Version)
	if err == nil {
		t.Fatal("EditEntry succeeded despite a blocked revision directory")
	}

	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(original) {
		t.Errorf("file = %q, want the original bytes after the failed edit", raw)
	}
}

// review-knowledge #5: EditEntryFields captures the new version's
// content before the transaction is known to have landed. If the commit
// itself then fails -- an ambiguous outcome, not a statement error -- that
// speculative capture must be discarded along with the file write it goes
// with. Left behind, it blocks the real version from ever being captured:
// revisionToKeep sees "that version is already retained" and skips it.
func TestEditEntryFieldsCommitFailureDiscardsTheSpeculativeRevision(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Standup", Body: "v1\n"})
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}

	// A canary that turns EditEntryFields's own row update into a
	// foreign key violation SQLite defers to COMMIT: the closure completes
	// normally (done = true), the speculative version-2 capture happens,
	// and only the commit itself then fails.
	if _, err := c.db.Exec(`CREATE TABLE canary (id INTEGER PRIMARY KEY, target TEXT REFERENCES entry(id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`CREATE TRIGGER canary_trg AFTER UPDATE OF content_hash ON entry
		BEGIN INSERT INTO canary (target) VALUES ('does-not-exist'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}

	body := "v2\n"
	if _, err := c.EditEntryFields(ctx, p.ID, entry.Slug, EntryEdit{Body: &body, IfVersion: &entry.Version}); err == nil {
		t.Fatal("EditEntryFields succeeded; the deferred foreign key violation should have failed its commit")
	}

	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(original) {
		t.Fatalf("entry not restored after the failed commit:\ngot  %q\nwant %q", raw, original)
	}
	if _, err := os.Stat(revisionFilePath(entry.Path, entry.Version+1)); !os.IsNotExist(err) {
		t.Fatalf("a phantom version %d survived the failed commit: %v", entry.Version+1, err)
	}

	if _, err := c.db.Exec(`DROP TRIGGER canary_trg`); err != nil {
		t.Fatal(err)
	}

	// Redo the edit for real: it must capture the actual version 2, not
	// skip it as "already retained" because of the discarded phantom.
	edited, err := c.EditEntryFields(ctx, p.ID, entry.Slug, EntryEdit{Body: &body, IfVersion: &entry.Version})
	if err != nil {
		t.Fatalf("EditEntryFields (retry): %v", err)
	}
	v2, err := os.ReadFile(revisionFilePath(entry.Path, edited.Version))
	if err != nil {
		t.Fatalf("version %d was never captured: %v", edited.Version, err)
	}
	current, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(v2) != string(current) {
		t.Errorf("retained version %d = %q, want the real content %q", edited.Version, v2, current)
	}
}

func TestPromoteMovesTheRevisionDirectory(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Shared", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "v2\n", &entry.Version); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	oldDir := revisionDir(entry.Path)

	promoted, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "shared across projects")
	if err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if _, err := os.Stat(oldDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old revision directory still exists")
	}
	newDir := revisionDir(promoted.Path)
	for v := int64(1); v <= 2; v++ {
		if _, err := os.Stat(revisionFilePath(promoted.Path, v)); err != nil {
			t.Errorf("version %d missing after promote: %v", v, err)
		}
	}
	_ = newDir

	back, err := c.DemoteEntry(t.Context(), promoted.Slug, "back to project")
	if err != nil {
		t.Fatalf("DemoteEntry: %v", err)
	}
	if _, err := os.Stat(newDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the global revision directory still exists after demote")
	}
	for v := int64(1); v <= 2; v++ {
		if _, err := os.Stat(revisionFilePath(back.Path, v)); err != nil {
			t.Errorf("version %d missing after demote: %v", v, err)
		}
	}
}

func TestAFailedPromoteMovesTheRevisionDirectoryBack(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Deploy"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	oldDir := revisionDir(entry.Path)

	if _, err := c.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	defer func() {
		if _, err := c.db.Exec(`DROP TRIGGER boom`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}()

	if _, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason"); err == nil {
		t.Fatal("PromoteEntry succeeded despite the trigger")
	}
	if _, err := os.Stat(revisionFilePath(entry.Path, 1)); err != nil {
		t.Errorf("revision directory not restored at %s: %v", oldDir, err)
	}
	globalDir := filepath.Join(c.root, "global", "vault")
	if _, err := os.Stat(filepath.Join(globalDir, "."+filepath.Base(entry.Path))); !os.IsNotExist(err) {
		t.Errorf("revision directory should not remain in the global directory")
	}
}

func TestDeletingAnEntryRemovesItsRevisionDirectory(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Gone", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	dir := revisionDir(entry.Path)

	if err := c.DeleteEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("revision directory still exists after delete")
	}
}

func TestAFailedDeleteRestoresTheRevisionDirectory(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	dir := revisionDir(entry.Path)

	if _, err := c.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	defer func() {
		if _, err := c.db.Exec(`DROP TRIGGER boom`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}()

	if err := c.DeleteEntry(t.Context(), p.ID, entry.Slug); err == nil {
		t.Fatal("DeleteEntry succeeded despite the trigger")
	}
	if _, err := os.Stat(revisionFilePath(entry.Path, 1)); err != nil {
		t.Errorf("revision directory not restored at %s: %v", dir, err)
	}
}

func TestListEntryRevisionsNewestFirst(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Log", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	version := entry.Version
	for i := 2; i <= 3; i++ {
		edited, err := c.EditEntry(t.Context(), p.ID, entry.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditEntry v%d: %v", i, err)
		}
		version = edited.Version
	}
	revs, err := c.ListEntryRevisions(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("ListEntryRevisions: %v", err)
	}
	if len(revs) != 3 {
		t.Fatalf("%d revisions, want 3", len(revs))
	}
	for i, want := range []int64{3, 2, 1} {
		if revs[i].Version != want {
			t.Errorf("revs[%d].Version = %d, want %d (newest first)", i, revs[i].Version, want)
		}
	}
}

func TestDiffEntryDefaultsToPreviousAgainstLatest(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Notes", Body: "line one\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "line one\nline two\n", &entry.Version); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	diff, err := c.DiffEntry(t.Context(), p.ID, entry.Slug, 0, 0)
	if err != nil {
		t.Fatalf("DiffEntry: %v", err)
	}
	if diff.From != 1 || diff.To != 2 {
		t.Errorf("from/to = %d/%d, want 1/2", diff.From, diff.To)
	}
	if !strings.Contains(diff.Diff, "+line two") {
		t.Errorf("diff = %q, want it to add line two", diff.Diff)
	}
}

func TestDiffEntryRejectsAnUnretainedVersion(t *testing.T) {
	c, p, _ := vaultCore(t)
	c.historyKeep = 1
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Tight", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "v2\n", &entry.Version); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	_, err = c.DiffEntry(t.Context(), p.ID, entry.Slug, 1, 2)
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "revision_not_retained" {
		t.Fatalf("err = %v, want revision_not_retained", err)
	}
	if !strings.Contains(err.Error(), "2-2") {
		t.Errorf("error %q does not name the retained range", err.Error())
	}
}
