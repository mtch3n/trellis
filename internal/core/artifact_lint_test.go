package core

import (
	"strings"
	"testing"
)

func findingsFor(t *testing.T, c *Core, projectID string, doc Knowledge) []LintFinding {
	t.Helper()
	all, err := c.Lint(t.Context(), projectID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	var out []LintFinding
	for _, f := range all {
		if f.Doc == doc.Ref {
			out = append(out, f)
		}
	}
	return out
}

func entryNaming(t *testing.T, c *Core, projectID, title string, names ...string) Knowledge {
	t.Helper()
	doc, err := c.CreateKnowledge(t.Context(), projectID, NewKnowledge{Title: title})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, names...)
	if _, err := c.LoadKnowledge(t.Context(), projectID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	return doc
}

func TestLintReportsAMissingArtifact(t *testing.T) {
	c, p, _ := kbCore(t)
	doc := entryNaming(t, c, p.ID, "Absent", "absent.pdf")

	var found bool
	for _, f := range findingsFor(t, c, p.ID, doc) {
		if f.Kind == "missing_artifact" && f.Ref == "absent.pdf" {
			found = true
			if !strings.Contains(f.Fix, "artifact add") {
				t.Errorf("fix = %q, want it to point at `artifact add`", f.Fix)
			}
		}
		if f.Kind == "stub" && f.Ref == "absent.pdf" {
			t.Errorf("an artifact name leaked into the wikilink stub finding")
		}
	}
	if !found {
		t.Error("no missing_artifact finding for absent.pdf")
	}
}

func TestLintIsQuietAboutAResolvedArtifact(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "fine.png", "\x89PNG\r\n\x1a\nx")
	doc := entryNaming(t, c, p.ID, "Fine", a.Name)
	for _, f := range findingsFor(t, c, p.ID, doc) {
		if f.Kind == "missing_artifact" {
			t.Errorf("unexpected finding %+v", f)
		}
	}
}

// Orphan means disconnected from other entries and cards. A file attached to
// an entry does not connect it to anything, and the orphan fix says so.
func TestAnEntryLinkedOnlyToAnArtifactIsStillAnOrphan(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "alone.png", "\x89PNG\r\n\x1a\nx")
	doc := entryNaming(t, c, p.ID, "Alone", a.Name)
	for _, f := range findingsFor(t, c, p.ID, doc) {
		if f.Kind == "orphan" {
			return
		}
	}
	t.Error("an entry whose only link is an artifact was not reported as an orphan")
}
