package core

import (
	"errors"
	"strings"
	"testing"
)

func TestGlossaryTemplateKeepsItsTermsSection(t *testing.T) {
	c, p, _ := vaultCore(t)
	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Glossary", Template: "glossary",
		Body: "| Term | Means | Not |\n|---|---|---|\n",
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "Terms") {
		t.Fatalf("err = %v, want template_violation naming the Terms section", err)
	}
}

func TestGlossaryTemplateRendersItsTable(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Glossary", Template: "glossary"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Template != "glossary" ||
		!strings.Contains(entry.BodyMD, "## Terms") ||
		!strings.Contains(entry.BodyMD, "| Term | Means | Not |") {
		t.Fatalf("template %q, body:\n%s", entry.Template, entry.BodyMD)
	}
}
