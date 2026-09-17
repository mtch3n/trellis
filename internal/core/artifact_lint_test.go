package core

import (
	"strings"
	"testing"
)

func diagnosticsFor(t *testing.T, c *Core, projectID string, entry Entry) []Diagnostic {
	t.Helper()
	all, err := c.Lint(t.Context(), projectID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	var out []Diagnostic
	for _, f := range all {
		if f.Entry == entry.Ref {
			out = append(out, f)
		}
	}
	return out
}

func entryNaming(t *testing.T, c *Core, projectID, title string, names ...string) Entry {
	t.Helper()
	entry, err := c.CreateEntry(t.Context(), projectID, NewEntry{Title: title})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, names...)
	if _, err := c.LoadEntry(t.Context(), projectID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	return entry
}

func TestLintReportsAMissingArtifact(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry := entryNaming(t, c, p.ID, "Absent", "absent.pdf")

	var found bool
	for _, f := range diagnosticsFor(t, c, p.ID, entry) {
		if f.Kind == "missing_artifact" && f.Ref == "absent.pdf" {
			found = true
			if !strings.Contains(f.Fix, "artifact add") {
				t.Errorf("fix = %q, want it to point at `artifact add`", f.Fix)
			}
		}
		if f.Kind == "stub" && f.Ref == "absent.pdf" {
			t.Errorf("an artifact name leaked into the wikilink stub diagnostic")
		}
	}
	if !found {
		t.Error("no missing_artifact diagnostic for absent.pdf")
	}
}

func TestLintIsQuietAboutAResolvedArtifact(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "fine.png", "\x89PNG\r\n\x1a\nx")
	entry := entryNaming(t, c, p.ID, "Fine", a.Name)
	for _, f := range diagnosticsFor(t, c, p.ID, entry) {
		if f.Kind == "missing_artifact" {
			t.Errorf("unexpected diagnostic %+v", f)
		}
	}
}

// Orphan means disconnected from other entries and cards. A file attached to
// an entry does not connect it to anything, and the orphan fix says so.
func TestAnEntryLinkedOnlyToAnArtifactIsStillAnOrphan(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "alone.png", "\x89PNG\r\n\x1a\nx")
	entry := entryNaming(t, c, p.ID, "Alone", a.Name)
	for _, f := range diagnosticsFor(t, c, p.ID, entry) {
		if f.Kind == "orphan" {
			return
		}
	}
	t.Error("an entry whose only link is an artifact was not reported as an orphan")
}
