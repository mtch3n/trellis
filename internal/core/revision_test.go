package core

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCreateKnowledgeCapturesVersionOne(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Standup", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	rev, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil {
		t.Fatalf("version 1 was not captured: %v", err)
	}
	entry, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(rev) != string(entry) {
		t.Errorf("revision 1 = %q, want the entry's own bytes %q", rev, entry)
	}
}

func TestEditKeepsBothVersions(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	v1, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil {
		t.Fatalf("version 1 missing before the edit: %v", err)
	}

	edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version)
	if err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if edited.Version != 2 {
		t.Fatalf("version = %d, want 2", edited.Version)
	}
	v1After, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil || string(v1After) != string(v1) {
		t.Errorf("version 1 changed or vanished after the edit: %v", err)
	}
	v2, err := os.ReadFile(revisionFilePath(doc.Path, 2))
	if err != nil {
		t.Fatalf("version 2 was not captured: %v", err)
	}
	entry, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(v2) != string(entry) {
		t.Errorf("revision 2 = %q, want the entry's current bytes %q", v2, entry)
	}
}

// An entry that existed before this feature has no revision directory. Its
// first Trellis write must still capture the version it is about to replace.
func TestFirstEditCapturesAPredatingVersion(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Old", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := os.RemoveAll(revisionDir(doc.Path)); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}

	edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version)
	if err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	pre, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil || string(pre) != string(original) {
		t.Errorf("version 1 = %q, %v; want the pre-edit bytes %q", pre, err, original)
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, edited.Version)); err != nil {
		t.Errorf("version %d was not captured: %v", edited.Version, err)
	}
}

// This is the case that motivates copying a version in rather than moving the
// previous one out: a direct edit lands after Trellis has already written a
// version, and that version must not be lost.
func TestADirectEditAfterATrellisWriteKeepsBothVersions(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Race", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	trellisEdit, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2 via trellis\n", &doc.Version)
	if err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	direct := RenderDoc(fm, "v3 direct edit\n")
	if err := os.WriteFile(doc.Path, []byte(direct), 0o600); err != nil {
		t.Fatal(err)
	}

	reloaded, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if reloaded.Version != trellisEdit.Version+1 {
		t.Fatalf("version = %d, want %d after the direct edit", reloaded.Version, trellisEdit.Version+1)
	}

	trellisRev, err := os.ReadFile(revisionFilePath(doc.Path, trellisEdit.Version))
	if err != nil {
		t.Fatalf("the trellis-written version was lost: %v", err)
	}
	if !strings.Contains(string(trellisRev), "v2 via trellis") {
		t.Errorf("revision %d = %q, want the trellis-written body", trellisEdit.Version, trellisRev)
	}
	directRev, err := os.ReadFile(revisionFilePath(doc.Path, reloaded.Version))
	if err != nil {
		t.Fatalf("the direct edit was not captured: %v", err)
	}
	if !strings.Contains(string(directRev), "v3 direct edit") {
		t.Errorf("revision %d = %q, want the direct edit's body", reloaded.Version, directRev)
	}
}

func TestRepeatedReadsWriteNoNewRevision(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Stable", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
			t.Fatalf("LoadKnowledge: %v", err)
		}
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
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
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Quiet", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE knowledge SET private = 1 WHERE id = ?`, doc.ID); err != nil {
		t.Fatal(err)
	}

	reloaded, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if reloaded.Version != doc.Version+1 {
		t.Fatalf("version = %d, want %d: the drift must still bump the version", reloaded.Version, doc.Version+1)
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d revision files after a drift-only bump, want 1 (version 1 only)", len(entries))
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, reloaded.Version)); err == nil {
		t.Errorf("a revision was captured for the drift-only version %d", reloaded.Version)
	}
}

// c.historyKeep is unexported: this package's own tests set it directly
// rather than through a setter. Task 4 adds the public SetHistoryKeep, wired
// from config; the field and its enforcement already work without it.
func TestEditTrimsToHistoryKeep(t *testing.T) {
	c, p, _ := kbCore(t)
	c.historyKeep = 3
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Busy", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	version := doc.Version
	for i := 2; i <= 4; i++ {
		edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditKnowledge v%d: %v", i, err)
		}
		version = edited.Version
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d revision files, want 3 (keep=3)", len(entries))
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, 1)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("version 1 should have been trimmed")
	}
	for v := int64(2); v <= 4; v++ {
		if _, err := os.Stat(revisionFilePath(doc.Path, v)); err != nil {
			t.Errorf("version %d should be retained: %v", v, err)
		}
	}
}

func TestHistoryKeepZeroCapturesNothing(t *testing.T) {
	c, p, _ := kbCore(t)
	c.historyKeep = 0
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Off", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := os.Stat(revisionDir(doc.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a revision directory was created despite historyKeep = 0")
	}
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if _, err := os.Stat(revisionDir(doc.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a revision directory was created by an edit despite historyKeep = 0")
	}
}

// A revision write that cannot land must fail the edit and leave the entry
// file exactly as it was: revision capture rides the same undo path as every
// other failure in EditKnowledgeFields.
func TestAFailedRevisionWriteFailsTheEdit(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Blocked", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	original, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Version 1's revision file cannot be (re-)written: a plain file occupies
	// where its directory needs to be.
	if err := os.RemoveAll(revisionDir(doc.Path)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revisionDir(doc.Path), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = c.EditKnowledge(t.Context(), p.ID, doc.Slug, "v2\n", &doc.Version)
	if err == nil {
		t.Fatal("EditKnowledge succeeded despite a blocked revision directory")
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(original) {
		t.Errorf("file = %q, want the original bytes after the failed edit", raw)
	}
}
