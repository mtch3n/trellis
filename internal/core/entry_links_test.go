package core

import (
	"testing"
)

func TestEntryLinksListsAddressesAndWithholdsPrivateSources(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	create := func(title, body string, private bool) Entry {
		t.Helper()
		entry, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: title, Body: body, Private: private})
		if err != nil {
			t.Fatalf("CreateKnowledge %s: %v", title, err)
		}
		return entry
	}

	create("Target Doc", "target\n", false)
	vault := create("Vault Doc", "[[target-doc]]\n", false)
	if _, err := c.EscalateKnowledge(ctx, p.ID, vault.Slug, "shared"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	create("Source Doc", "[[target-doc#usage]] [[missing-one]] [[/GLOBAL/vault/vault-doc]]\n", false)
	create("Secret Doc", "[[target-doc]]\n", true)
	// Private in the file only: the mirror still says public.
	stale := create("Stale Doc", "[[target-doc]]\n", false)
	setPrivateInFile(t, stale.Path, true)

	links, err := c.EntryLinks(ctx, p.ID)
	if err != nil {
		t.Fatalf("KnowledgeLinks: %v", err)
	}
	addr := func(s string) *string { return &s }
	key := p.Key
	want := []EntryLink{
		{From: "/GLOBAL/vault/vault-doc", To: addr("/" + key + "/vault/target-doc"), Raw: "target-doc"},
		{From: "/" + key + "/vault/source-doc", To: addr("/GLOBAL/vault/vault-doc"), Raw: "/GLOBAL/vault/vault-doc"},
		{From: "/" + key + "/vault/source-doc", To: nil, Raw: "missing-one"},
		{From: "/" + key + "/vault/source-doc", To: addr("/" + key + "/vault/target-doc"), Raw: "target-doc#usage", Anchor: "usage"},
	}
	if len(links) != len(want) {
		t.Fatalf("links = %s, want %d", show(links), len(want))
	}
	for i := range want {
		got, w := links[i], want[i]
		if got.From != w.From || got.Raw != w.Raw || got.Anchor != w.Anchor || deref(got.To) != deref(w.To) || (got.To == nil) != (w.To == nil) {
			t.Errorf("links[%d] = %s, want %s", i, show([]EntryLink{got}), show([]EntryLink{w}))
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func show(links []EntryLink) string {
	out := ""
	for _, l := range links {
		out += "{" + l.From + " -> " + deref(l.To) + " raw=" + l.Raw + " anchor=" + l.Anchor + "} "
	}
	return out
}
