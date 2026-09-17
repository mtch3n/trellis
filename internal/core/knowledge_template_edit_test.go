package core

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func decisionBody(extra string) string {
	return "# Chose SQLite\n\n## Context\n\nc\n\n## Options considered\n\no\n\n## Decision\n\nd\n\n## Consequences\n\nq\n" + extra
}

func newDecision(t *testing.T, c *Core, projectID string) Knowledge {
	t.Helper()
	doc, err := c.CreateKnowledge(t.Context(), projectID, NewKnowledge{
		Title: "Chose SQLite", Template: "decision", Body: decisionBody(""),
		Sources: []string{"https://sqlite.org/whentouse.html"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	return doc
}

// An edit is held to the entry's template, as creation is. The reject
// template refuses the edit and leaves the file as it was.
func TestEditUnderARejectTemplateIsRefused(t *testing.T) {
	c, p, _ := kbCore(t)
	doc := newDecision(t, c, p.ID)
	before, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}

	for name, edit := range map[string]KnowledgeEdit{
		"a section removed": {Body: new("# Chose SQLite\n\n## Context\n\nc\n")},
		"sources cleared":   {Sources: new([]string{})},
	} {
		edit.IfVersion = &doc.Version
		_, err := c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, edit)
		if code := errCode(t, err); code != "template_violation" {
			t.Errorf("%s: code = %s (%v)", name, code, err)
		}
	}
	if after, _ := os.ReadFile(doc.Path); string(after) != string(before) {
		t.Errorf("a refused edit changed the file:\n%s", after)
	}
}

// The verify rule reads in the edit's own transaction. With one database
// connection, a second transaction would wait forever, so this has a deadline.
func TestEditRunsTheVerifyRule(t *testing.T) {
	c, p, _ := kbCore(t)
	doc := newDecision(t, c, p.ID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, err := c.EditKnowledgeFields(ctx, p.ID, doc.Slug, KnowledgeEdit{
		Body: new(decisionBody("See [[no-such-entry]].\n")), IfVersion: &doc.Version,
	})
	if code := errCode(t, err); code != "template_violation" || !strings.Contains(err.Error(), "no-such-entry") {
		t.Fatalf("edit citing a missing entry: %v", err)
	}
	if _, err := c.EditKnowledgeFields(ctx, p.ID, doc.Slug, KnowledgeEdit{
		Body: new(decisionBody("See [[chose-sqlite]].\n")), IfVersion: &doc.Version,
	}); err != nil {
		t.Fatalf("edit citing an existing entry: %v", err)
	}
}

// A warn template lets the edit through and says what is missing.
func TestEditUnderAWarnTemplateWarns(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Latency", Template: "research"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{
		Body: new("# Latency\n\n## Question\n\nq\n"), IfVersion: &doc.Version,
	})
	if err != nil {
		t.Fatalf("EditKnowledgeFields: %v", err)
	}
	if !slices.Contains(got.Warnings, "missing section Conclusion") {
		t.Errorf("warnings = %v", got.Warnings)
	}
}

// Switching checks the new template; clearing checks nothing; an unknown
// name is refused.
func TestEditSwitchesAndClearsTheTemplate(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Loose", Body: "just text\n"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Template != "" {
		t.Fatalf("an entry created without --template has template %q", doc.Template)
	}

	_, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Template: new("decision"), IfVersion: &doc.Version})
	if code := errCode(t, err); code != "template_violation" {
		t.Errorf("switch to decision: %v", err)
	}
	_, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{Template: new("nope"), IfVersion: &doc.Version})
	if code := errCode(t, err); code != "unknown_template" {
		t.Errorf("switch to nope: %v", err)
	}

	dec := newDecision(t, c, p.ID)
	cleared, err := c.EditKnowledgeFields(t.Context(), p.ID, dec.Slug, KnowledgeEdit{
		Template: new(""), Body: new("free text\n"), IfVersion: &dec.Version,
	})
	if err != nil {
		t.Fatalf("clear the template: %v", err)
	}
	raw, err := os.ReadFile(cleared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Template != "" || strings.Contains(string(raw), "template:") {
		t.Errorf("cleared entry: template %q, file:\n%s", cleared.Template, raw)
	}
}

// Hand edits are Lint's to report: a broken template, and one that is gone.
// An entry whose template is gone can still be edited.
func TestLintReportsTemplateProblemsFromHandEdits(t *testing.T) {
	c, p, _ := kbCore(t)
	broken := newDecision(t, c, p.ID)
	raw, err := os.ReadFile(broken.Path)
	if err != nil {
		t.Fatal(err)
	}
	hand := strings.Replace(string(raw), "## Consequences", "## Afterwards", 1)
	if err := os.WriteFile(broken.Path, []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Orphan", Template: "research"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(orphan.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan.Path, []byte(strings.Replace(string(raw), "template: research", "template: gone", 1)), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, f := range findings {
		if f.Kind == "template_violation" || f.Kind == "unknown_template" {
			kinds = append(kinds, f.Kind+" "+f.Ref)
		}
	}
	slices.Sort(kinds)
	want := []string{"template_violation missing section Consequences", "unknown_template gone"}
	if !slices.Equal(kinds, want) {
		t.Errorf("findings = %v, want %v", kinds, want)
	}

	cur, err := c.LoadKnowledge(t.Context(), p.ID, orphan.Slug)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.EditKnowledgeFields(t.Context(), p.ID, orphan.Slug, KnowledgeEdit{Body: new("still editable\n"), IfVersion: &cur.Version})
	if err != nil {
		t.Fatalf("edit an entry whose template is gone: %v", err)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "gone") {
		t.Errorf("warnings = %v", got.Warnings)
	}
}

// Set writes and removes the fields a template asks for; built-in fields
// have their own options.
func TestEditSetsTemplateFields(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Owned", Body: "x\n"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{
		Set: map[string]string{"owner": "alice"}, IfVersion: &doc.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(got.Path); !strings.Contains(string(raw), "owner: alice") {
		t.Fatalf("file after set:\n%s", raw)
	}
	got, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{
		Set: map[string]string{"owner": ""}, IfVersion: &got.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(got.Path); strings.Contains(string(raw), "owner:") {
		t.Fatalf("file after removal:\n%s", raw)
	}
	_, err = c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug, KnowledgeEdit{
		Set: map[string]string{"template": "decision"}, IfVersion: &got.Version,
	})
	if code := errCode(t, err); code != "reserved_field" {
		t.Errorf("set template: %v", err)
	}
}

// A refusal lists each problem apart from its one-sentence message.
func TestTemplateViolationListsProblems(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Bare", Template: "decision", Body: "x\n"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || strings.Contains(e.Msg, "\n") || len(e.Problems) < 2 {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(err.Error(), "\n  - missing required field sources") {
		t.Errorf("Error() = %q, want the problems listed for the terminal", err.Error())
	}
}
