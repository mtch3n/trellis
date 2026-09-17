package core

import (
	"errors"
	"strings"
	"testing"
)

func TestDecisionTemplateRejectsWithNoSources(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Pick a queue", Template: "decision"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "sources") {
		t.Fatalf("err = %v, want template_violation naming sources", err)
	}
	if !strings.Contains(e.Fix, "template show decision") {
		t.Errorf("Fix = %q, want it to point at the template", e.Fix)
	}
}

func TestMissingSourcesFixIsARunnableSourceExample(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Template: "decision"})
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("err = %v, want *Error", err)
	}
	if !strings.Contains(e.Fix, "--source") || !strings.Contains(e.Fix, "template show decision") {
		t.Errorf("Fix = %q, want a --source example and a pointer to template show", e.Fix)
	}
}

// A filled-in decision — every section replaced with real content, sources
// supplied — must pass. decision.md's own placeholders, "### Option A —
// <name>", are level-3 headings and must never be mistaken for a missing
// required section.
func TestFilledInDecisionIsAccepted(t *testing.T) {
	c, p, _ := kbCore(t)
	body := `# Use SQLite over Postgres

## Context

We need embedded storage with no server to run.

## Options considered

### Option A — SQLite

- **What**: an embedded, file-based database.
- **Pros**: no server, single file, battle-tested.
- **Cons**: one writer at a time.

### Option B — Postgres

- **What**: a client-server database.
- **Pros**: concurrent writers.
- **Cons**: another process to run and back up.

## Decision

SQLite: Trellis is single-writer by design already.

## Consequences

Backups are a file copy. A future multi-writer feature would need a rethink.
`
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Use SQLite over Postgres", Template: "decision", Body: body,
		Sources: []string{"https://sqlite.org/whentouse.html"},
	})
	if err != nil {
		t.Fatalf("a filled-in decision must be accepted: %v", err)
	}
	if len(doc.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", doc.Warnings)
	}
}

func TestFindingTemplateRejectsWithNoSources(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Flaky test", Template: "finding"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "sources") {
		t.Fatalf("err = %v, want template_violation naming sources", err)
	}
}

func TestFindingTemplateSkeletonHasFactEvidenceScope(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Flaky test", Template: "finding", Sources: []string{"https://ci.example.com/run/482"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	for _, heading := range []string{"## Fact", "## Evidence", "## Scope"} {
		if !strings.Contains(doc.BodyMD, heading) {
			t.Errorf("body missing %q:\n%s", heading, doc.BodyMD)
		}
	}
}
