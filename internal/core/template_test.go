package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredSectionsMatchLevelTwoExactly(t *testing.T) {
	body := "# {{title}}\n\n## Context\n\n## Options considered\n\n" +
		"### Option A — <name>\n\n### Option B — <name>\n\n## Decision\n\n## Consequences\n"
	got := requiredSections(body)
	want := []string{"Context", "Options considered", "Decision", "Consequences"}
	if len(got) != len(want) {
		t.Fatalf("requiredSections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("requiredSections[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOptionalSectionIsNotRequiredAndMarkerIsStripped(t *testing.T) {
	body := "# {{title}}\n\n## Steps\n\n## Rollback <!-- optional -->\n"
	if got := requiredSections(body); len(got) != 1 || got[0] != "Steps" {
		t.Fatalf("requiredSections = %v, want just [Steps]", got)
	}
	rendered := stripOptionalMarkers(body)
	if rendered != "# {{title}}\n\n## Steps\n\n## Rollback\n" {
		t.Errorf("stripOptionalMarkers = %q", rendered)
	}
}

func TestPresentSectionsMatchesADocumentBody(t *testing.T) {
	body := "# Title\n\n## Preconditions\n\nSome text.\n\n## Steps\n\n1. Do it.\n"
	set := presentSections(body)
	if !set["Preconditions"] || !set["Steps"] {
		t.Errorf("presentSections = %v, want Preconditions and Steps", set)
	}
	if set["Verification"] {
		t.Error("Verification was never written and must not be present")
	}
}

func TestRenderTemplateBodySubstitutesAndDefaultsToEmpty(t *testing.T) {
	body := "# {{title}}\n\nOwner: {{owner}}. Extra: {{missing}}.\n"
	got := renderTemplateBody(body, "Rollback the API", map[string]string{"owner": "alice"})
	want := "# Rollback the API\n\nOwner: alice. Extra: .\n"
	if got != want {
		t.Errorf("renderTemplateBody = %q, want %q", got, want)
	}
}

func TestValidateTemplateRulesRejectsBadEnforceAndEmptyChoices(t *testing.T) {
	if err := validateTemplateRules(TemplateRules{Enforce: "warn"}); err != nil {
		t.Errorf("warn should be valid: %v", err)
	}
	if err := validateTemplateRules(TemplateRules{}); err != nil {
		t.Errorf("empty enforce (defaults to warn) should be valid: %v", err)
	}
	if err := validateTemplateRules(TemplateRules{Enforce: "sometimes"}); err == nil {
		t.Error("want an error for an unknown enforce value")
	}
	if err := validateTemplateRules(TemplateRules{Choices: map[string][]string{"severity": {}}}); err == nil {
		t.Error("want an error for an empty choices list")
	}
}

func TestLoadTemplateMissingIsUnknownTemplate(t *testing.T) {
	_, err := loadTemplate(t.TempDir(), "nope")
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "unknown_template" {
		t.Fatalf("err = %v, want unknown_template", err)
	}
}

func TestLoadTemplateNamesItsFileWhenBroken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte("---\nenforce: sometimes\n---\n# {{title}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadTemplate(dir, "bad")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want it to name %s", err, path)
	}
}

func TestLoadTemplateReadsRulesAndBody(t *testing.T) {
	dir := t.TempDir()
	raw := "---\nenforce: reject\nrequired: [owner]\nchoices:\n  severity: [low, high]\n---\n# {{title}}\n\n## Steps\n"
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpl, err := loadTemplate(dir, "runbook")
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if tmpl.Enforce != "reject" || len(tmpl.Required) != 1 || tmpl.Required[0] != "owner" {
		t.Errorf("tmpl = %+v", tmpl)
	}
	if len(tmpl.Choices["severity"]) != 2 {
		t.Errorf("choices = %+v", tmpl.Choices)
	}
	if !strings.Contains(tmpl.Body, "## Steps") {
		t.Errorf("Body = %q", tmpl.Body)
	}
}

func TestLoadTemplateWithNoFrontmatterDefaultsToWarnAndNoRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain.md"), []byte("# {{title}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpl, err := loadTemplate(dir, "plain")
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if tmpl.Enforce != "warn" || len(tmpl.Required) != 0 || len(tmpl.Choices) != 0 {
		t.Errorf("tmpl = %+v, want warn with no rules", tmpl)
	}
}

func TestSeedingWritesBuiltinsOnceAndNeverAgain(t *testing.T) {
	c := testCore(t)
	c.WithKBRoot(t.TempDir())

	dir, err := c.templatesDir()
	if err != nil {
		t.Fatalf("templatesDir: %v", err)
	}
	for _, name := range Templates() {
		if _, err := os.Stat(filepath.Join(dir, name+".md")); err != nil {
			t.Errorf("%s was not seeded: %v", name, err)
		}
	}

	// Deleting a built-in and asking for the directory again must not
	// bring it back: seeding runs only when the directory itself is
	// absent.
	if err := os.Remove(filepath.Join(dir, "decision.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.templatesDir(); err != nil {
		t.Fatalf("templatesDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "decision.md")); !os.IsNotExist(err) {
		t.Error("decision.md was reseeded after being deleted")
	}
}

func TestSeededTemplatesAllParse(t *testing.T) {
	c := testCore(t)
	c.WithKBRoot(t.TempDir())
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	wantEnforce := map[string]string{
		"decision": "reject", "finding": "reject", "glossary": "reject",
		"reference": "warn", "research": "warn", "runbook": "warn",
	}
	for _, name := range Templates() {
		tmpl, err := loadTemplate(dir, name)
		if err != nil {
			t.Fatalf("loadTemplate(%s): %v", name, err)
		}
		if tmpl.Enforce != wantEnforce[name] {
			t.Errorf("%s: enforce = %q, want %q", name, tmpl.Enforce, wantEnforce[name])
		}
	}
}
