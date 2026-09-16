package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintReportsAnUnknownFrontmatterKey(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Typo'd"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "title: Typo'd", "title: Typo'd\nprovenence: extracted", 1)
	if edited == string(raw) {
		t.Fatal("setup: expected the title line to be found and rewritten")
	}
	if err := os.WriteFile(doc.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	found := findingsFor(t, c, p.ID, doc)
	var got *LintFinding
	for i := range found {
		if found[i].Kind == "unknown_field" {
			got = &found[i]
		}
	}
	if got == nil || got.Ref != "provenence" {
		t.Fatalf("findings = %+v, want an unknown_field naming provenence", found)
	}
}

func TestLintDoesNotReportAFieldATemplateNames(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: warn\nrequired: [owner]\n---\n# {{title}}\n"
	if err := os.WriteFile(filepath.Join(dir, "owned.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Has an owner", Template: "owned", Set: map[string]string{"owner": "alice"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	for _, f := range findingsFor(t, c, p.ID, doc) {
		if f.Kind == "unknown_field" {
			t.Errorf("owner was flagged as unknown, but the owned template names it: %+v", f)
		}
	}
}
