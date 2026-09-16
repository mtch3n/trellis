package core

import (
	"os"
	"strings"
	"testing"
)

// setPrivateInFile edits the file the way a human with an editor would, which
// is the ordinary way this flag gets set.
func setPrivateInFile(t *testing.T, path string, on bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(raw)
	s = strings.ReplaceAll(s, "private: true\n", "")
	if on {
		s = strings.Replace(s, "title:", "private: true\ntitle:", 1)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestPrivateRoundTripsThroughTheFile(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if !doc.Private {
		t.Fatal("Private = false on the returned doc, want true")
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "private: true") {
		t.Errorf("frontmatter missing the flag:\n%s", raw)
	}

	reread, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Error("Private = false after reload, want true")
	}
}

// Both directions. The un-setting direction is the one a SQL-side filter would
// break permanently, so it is asserted explicitly.
func TestPrivateFollowsTheFileInBothDirections(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Deploy log"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Private {
		t.Fatal("Private = true by default, want false")
	}

	setPrivateInFile(t, doc.Path, true)
	on, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge after setting: %v", err)
	}
	if !on.Private {
		t.Fatal("Private = false after the file set it, want true")
	}

	setPrivateInFile(t, doc.Path, false)
	off, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge after clearing: %v", err)
	}
	if off.Private {
		t.Error("Private = true after the file cleared it, want false")
	}
}

// The mirror can disagree with the file — a database restored from an older
// backup, or a file that already carried the key when the column was added. The
// file wins, and the content hash alone does not notice, so the refresh must
// compare the flag too.
func TestMirrorDriftIsCorrectedFromTheFile(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Staging credentials", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.db.Exec(`UPDATE knowledge SET private = 0 WHERE id = ?`, doc.ID); err != nil {
		t.Fatalf("drift the mirror: %v", err)
	}

	reread, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if !reread.Private {
		t.Fatal("the stale mirror won over the file")
	}

	var stored int
	if err := c.db.Get(&stored, `SELECT private FROM knowledge WHERE id = ?`, doc.ID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != 1 {
		t.Errorf("private column = %d after the read, want 1: the correction was not persisted", stored)
	}
}

func TestPrivateWithANonBooleanValueFailsTheParse(t *testing.T) {
	_, _, err := SplitFrontmatter("---\ntitle: X\nprivate: maybe\n---\n\nbody\n")
	if err == nil {
		t.Fatal("SplitFrontmatter accepted a non-boolean private value")
	}
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("error = %v, want it to name the frontmatter", err)
	}
}
