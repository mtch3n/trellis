package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintReportsAnUnknownFrontmatterKey(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Typo'd"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "title: Typo'd", "title: Typo'd\nprovenence: extracted", 1)
	if edited == string(raw) {
		t.Fatal("setup: expected the title line to be found and rewritten")
	}
	if err := os.WriteFile(entry.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	found := findingsFor(t, c, p.ID, entry)
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
	c, p, _ := vaultCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: warn\nrequired: [owner]\n---\n# {{title}}\n"
	if err := os.WriteFile(filepath.Join(dir, "owned.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Has an owner", Template: "owned", Set: map[string]string{"owner": "alice"},
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	for _, f := range findingsFor(t, c, p.ID, entry) {
		if f.Kind == "unknown_field" {
			t.Errorf("owner was flagged as unknown, but the owned template names it: %+v", f)
		}
	}
}
