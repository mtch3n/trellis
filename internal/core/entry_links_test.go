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
			t.Fatalf("CreateEntry %s: %v", title, err)
		}
		return entry
	}

	create("Target Entry", "target\n", false)
	vault := create("Vault Entry", "[[target-entry]]\n", false)
	if _, err := c.EscalateKnowledge(ctx, p.ID, vault.Slug, "shared"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	create("Source Entry", "[[target-entry#usage]] [[missing-one]] [[/GLOBAL/vault/vault-entry]]\n", false)
	create("Secret Entry", "[[target-entry]]\n", true)
	// Private in the file only: the mirror still says public.
	stale := create("Stale Entry", "[[target-entry]]\n", false)
	setPrivateInFile(t, stale.Path, true)

	links, err := c.EntryLinks(ctx, p.ID)
	if err != nil {
		t.Fatalf("EntryLinks: %v", err)
	}
	addr := func(s string) *string { return &s }
	key := p.Key
	want := []EntryLink{
		{From: "/GLOBAL/vault/vault-entry", To: addr("/" + key + "/vault/target-entry"), Raw: "target-entry"},
		{From: "/" + key + "/vault/source-entry", To: addr("/GLOBAL/vault/vault-entry"), Raw: "/GLOBAL/vault/vault-entry"},
		{From: "/" + key + "/vault/source-entry", To: nil, Raw: "missing-one"},
		{From: "/" + key + "/vault/source-entry", To: addr("/" + key + "/vault/target-entry"), Raw: "target-entry#usage", Anchor: "usage"},
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
