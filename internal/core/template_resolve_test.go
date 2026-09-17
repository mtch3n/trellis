package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCustomTemplate(t *testing.T, c *Core, name, raw string) {
	t.Helper()
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatalf("templatesDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRejectsWhenSourcesIsMissing(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "cited",
		"---\nenforce: reject\nrequired: [sources]\nresolve: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Claim", Template: "cited"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "sources") {
		t.Fatalf("err = %v, want template_violation naming sources", err)
	}
}

func TestResolveRejectsAnUnresolvedCardAddress(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited", Sources: []string{"/XPSCTL/cards/XPSCTL-999"},
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "XPSCTL-999") {
		t.Fatalf("err = %v, want template_violation naming the unresolved card", err)
	}
}

func TestResolveRejectsAnUnresolvedWikilink(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited", Sources: []string{"[[missing]]"},
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "[[missing]]") {
		t.Fatalf("err = %v, want template_violation naming the dangling wikilink", err)
	}
}

func TestResolveAcceptsAURLAndProse(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited",
		Sources: []string{"https://example.com/paper", "discussed in standup on Tuesday"},
	})
	if err != nil {
		t.Fatalf("a URL and prose must pass unchecked: %v", err)
	}
}

func TestResolveAcceptsAResolvedCardEntryAndArtifact(t *testing.T) {
	c, p, b := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Evidence"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Referenced entry"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	art := addArtifact(t, c, p.ID, "evidence.png", "\x89PNG\r\n\x1a\nx")

	_, err = c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited",
		Sources: []string{
			"/XPSCTL/cards/" + card.Ref,
			"[[" + entry.Slug + "]]",
			"/XPSCTL/artifacts/" + art.Name,
		},
	})
	if err != nil {
		t.Fatalf("resolved references must pass: %v", err)
	}
}

func TestResolveBodyRejectsADanglingLink(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "linked", "---\nenforce: reject\nresolve: [body]\n---\n# {{title}}\n")

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "linked", Body: "# Claim\n\nSee [[missing]].\n",
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "[[missing]]") {
		t.Fatalf("err = %v, want template_violation naming the dangling body link", err)
	}
}

func TestNoteTemplateStillAcceptsADanglingBodyLink(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Notes", Body: "# Notes\n\nSee [[not-written-yet]].\n",
	})
	if err != nil {
		t.Fatalf("a template with no resolve rule must not block a dangling link: %v", err)
	}
	if entry.Slug != "notes" {
		t.Errorf("slug = %q", entry.Slug)
	}
}

// Ruling: a "/"-prefixed source only counts as an internal reference when it
// has the address shape /<key>/(cards|vault|artifacts)/<rest>. Anything
// else that merely starts with "/" is external and passes unchecked.
func TestResolveAcceptsAFilesystemPathSource(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited", Sources: []string{"/usr/share/doc/x.txt:10"},
	})
	if err != nil {
		t.Fatalf("a filesystem path must pass unchecked: %v", err)
	}
}

// A value that does have the address shape but fails to parse as a valid
// address (a malformed ref, here) is unresolved and rejected — the shape
// check only decides whether to bother resolving at all, not whether the
// address is well-formed.
func TestResolveRejectsAnAddressShapedButMalformedSource(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited", Sources: []string{"/XPSCTL/cards/not-a-ref"},
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "not-a-ref") {
		t.Fatalf("err = %v, want template_violation naming the malformed address", err)
	}
}

// review-knowledge #17: the resolve rule matched a card address on seq alone,
// ignoring the ref's own prefix, so an address naming a different project's
// (or a nonexistent) ref "resolved" whenever this project happened to have
// a card at the same seq. card.ref is unique; match on it the way loadCard
// already does for a qualified reference.
func TestResolveRejectsACardAddressWhoseRefPrefixDoesNotMatch(t *testing.T) {
	c, p, b := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")
	var fifth Card
	for i := 1; i <= 5; i++ {
		card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "filler"})
		if err != nil {
			t.Fatalf("CreateCard: %v", err)
		}
		fifth = card
	}
	if fifth.Ref != p.Key+"-5" {
		t.Fatalf("ref = %q, want %s-5", fifth.Ref, p.Key)
	}

	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited", Sources: []string{"/" + p.Key + "/cards/OTHER-5"},
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" {
		t.Fatalf("err = %v, want template_violation: OTHER-5 must not resolve via %s-5's seq alone", err, p.Key)
	}
}

// An entry address counted a promoted row because it had no
// "global = 0" filter, while resolveEntryRef's own address branch excludes
// exactly that row -- an entry's old project address becomes a stub, not a
// hit, once it lives in the global vault instead.
func TestResolveRejectsAPromotedEntrysOldProjectAddress(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nresolve: [sources]\n---\n# {{title}}\n")
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Shared"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, target.Slug, "reason"); err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}

	_, err = c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "cited", Sources: []string{"/" + p.Key + "/vault/" + target.Slug},
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" {
		t.Fatalf("err = %v, want template_violation: the old project address is a stub once the entry is global", err)
	}
}

// Ruling: in a body, an absolute address counts only at the start of the
// text, or after whitespace or "(". A URL's path segment and a source file's
// relative path must never be mistaken for one, since in both cases the
// character right before the "/" is neither whitespace, "(", nor the start
// of the text.
func TestResolveBodyIgnoresURLAndRelativePathLookalikes(t *testing.T) {
	c, p, _ := vaultCore(t)
	writeCustomTemplate(t, c, "linked", "---\nenforce: reject\nresolve: [body]\n---\n# {{title}}\n")

	body := "# Claim\n\n" +
		"See https://example.com/foo/cards/bar for the upstream issue.\n" +
		"Implemented in src/api/cards/handler.go.\n"
	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Claim", Template: "linked", Body: body,
	})
	if err != nil {
		t.Fatalf("a URL path and a relative path must not be read as addresses: %v", err)
	}
}
