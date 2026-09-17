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

func TestPresentSectionsMatchesAnEntryBody(t *testing.T) {
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

// review-knowledge #2 / review-cli #1: a template name is joined straight
// into a filesystem path with no validation. "../" in the name must never
// let a read escape <root>/templates.
func TestLoadTemplateRejectsPathTraversalName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.md")
	if err := os.WriteFile(secret, []byte("---\ntitle: Secret\n---\ntop secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTemplate(dir, "../secret"); !isCode(err, "bad_template_name") {
		t.Fatalf("loadTemplate(../secret): err = %v, want bad_template_name", err)
	}
}

// The same validator guards every write, not just the read loadTemplate does:
// NewTemplate, EditTemplate and DeleteTemplate must all refuse a name that
// would resolve outside <root>/templates, before ever touching the disk.
func TestTemplateWritesRejectPathTraversalNames(t *testing.T) {
	tc := testCore(t)
	root := t.TempDir()
	c := New(tc.db, tc.clock, tc.actor, root)

	if _, err := c.NewTemplate(t.Context(), "../evil"); !isCode(err, "bad_template_name") {
		t.Fatalf("NewTemplate(../evil): err = %v, want bad_template_name", err)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.md")); !os.IsNotExist(err) {
		t.Errorf("NewTemplate wrote outside <root>/templates: %v", err)
	}

	if _, err := c.EditTemplate(t.Context(), "../evil", "---\nenforce: warn\n---\npwned\n"); !isCode(err, "bad_template_name") {
		t.Fatalf("EditTemplate(../evil): err = %v, want bad_template_name", err)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.md")); !os.IsNotExist(err) {
		t.Errorf("EditTemplate wrote outside <root>/templates")
	}

	// A file DeleteTemplate must not be able to reach through a traversal
	// name, even one it already knows exists.
	target := filepath.Join(root, "target.md")
	if err := os.WriteFile(target, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteTemplate(t.Context(), "../target"); !isCode(err, "bad_template_name") {
		t.Fatalf("DeleteTemplate(../target): err = %v, want bad_template_name", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("DeleteTemplate removed a file outside <root>/templates: %v", err)
	}
}

// The disclosure scenario the finding calls out by name: a --template value
// that is really a path to a private entry's own file must never load that
// file as a "template" and copy its body into a new, non-private entry.
func TestCreateEntryRefusesPathTraversalTemplateName(t *testing.T) {
	c, p, _ := vaultCore(t)
	secret, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Secret", Body: "the private body must never leak\n", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateEntry secret: %v", err)
	}
	traversal := "../projects/" + p.Key + "/vault/" + secret.Slug
	_, err = c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Copy", Template: traversal})
	if !isCode(err, "bad_template_name") {
		t.Fatalf("CreateEntry --template %s: err = %v, want bad_template_name", traversal, err)
	}
}

// review-knowledge #9: `template check`'s fields map was built from
// fm.Extra alone, so a required or choices rule on a built-in field
// (summary, sources, title, tags, ...) always reported a violation even
// when the entry plainly had that field -- disagreeing with edit and lint,
// which both use frontmatterFields.
func TestCheckTemplateSeesBuiltInFields(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.NewTemplate(t.Context(), "strict"); err != nil {
		t.Fatalf("NewTemplate: %v", err)
	}
	if _, err := c.EditTemplate(t.Context(), "strict",
		"---\nenforce: warn\nrequired: [summary, sources]\n---\n# {{title}}\n"); err != nil {
		t.Fatalf("EditTemplate: %v", err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Has both", Summary: "a summary", Sources: []string{"https://example.com"},
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	violations, err := c.CheckTemplate(t.Context(), p.ID, "strict", entry.Slug)
	if err != nil {
		t.Fatalf("CheckTemplate: %v", err)
	}
	for _, v := range violations {
		if strings.Contains(v, "missing required field") {
			t.Errorf("violations = %v, want summary and sources recognised as present", violations)
		}
	}
}

// review-knowledge #10: CreateEntry's template check only ever looked at
// summary, sources and --set values, so title, tags, labels, board and
// provenance were never checked at creation even though the edit path
// checks all of them via frontmatterFields.
func TestCreateEntryTemplateChecksTags(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.NewTemplate(t.Context(), "needs-tag"); err != nil {
		t.Fatalf("NewTemplate: %v", err)
	}
	if _, err := c.EditTemplate(t.Context(), "needs-tag",
		"---\nenforce: reject\nrequired: [tags]\n---\n# {{title}}\n"); err != nil {
		t.Fatalf("EditTemplate: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Tagged", Template: "needs-tag", Tags: []string{"ops"},
	}); err != nil {
		t.Fatalf("CreateEntry with --tag should satisfy required: [tags]: %v", err)
	}
}

// A choices rule on provenance is likewise never checked at creation today,
// so a reject template accepts a value it would refuse on the next edit.
func TestCreateEntryTemplateChecksProvenanceChoices(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.NewTemplate(t.Context(), "extracted-only"); err != nil {
		t.Fatalf("NewTemplate: %v", err)
	}
	if _, err := c.EditTemplate(t.Context(), "extracted-only",
		"---\nenforce: reject\nchoices:\n  provenance: [extracted]\n---\n# {{title}}\n"); err != nil {
		t.Fatalf("EditTemplate: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Authored", Template: "extracted-only", Provenance: "authored",
	}); err == nil {
		t.Fatal("CreateEntry should be refused: provenance \"authored\" is not in choices [extracted]")
	}
}

func TestSeedingWritesBuiltinsOnceAndNeverAgain(t *testing.T) {
	c := testCore(t)

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
