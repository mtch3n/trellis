package core

import "testing"

func TestEntryAddress(t *testing.T) {
	if got := EntryAddress("XPSCTL", false, "design"); got != "/XPSCTL/vault/design" {
		t.Errorf("project entry = %q", got)
	}
	if got := EntryAddress("XPSCTL", true, "design"); got != "/GLOBAL/vault/design" {
		t.Errorf("vault entry = %q", got)
	}
}

// The SQL fragment and the Go helper must never disagree: search results are
// compared with and passed back to commands that use the Go form.
func TestEntryAddressSQLMatchesGo(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Local entry"}); err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Vault entry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromoteEntry(ctx, p.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Slug   string `db:"slug"`
		Global bool   `db:"global"`
		Ref    string `db:"ref"`
	}
	if err := c.db.Select(&rows, `SELECT k.slug, k.global, `+entryAddressSQL+` AS ref
		FROM entry k JOIN project p ON p.id = k.project_id ORDER BY k.slug`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		if want := EntryAddress(p.Key, r.Global, r.Slug); r.Ref != want {
			t.Errorf("SQL address %q, Go address %q", r.Ref, want)
		}
	}
}

func TestSearchRecallAndVectorHitsCarryAddresses(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Lease renewal", Body: "A claim starts the lease.\n"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "/XPSCTL/vault/lease-renewal"
	if entry.Ref != want {
		t.Errorf("created ref = %q, want %q", entry.Ref, want)
	}

	hits, err := c.Search(ctx, p.ID, "lease", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	recalled, err := c.Recall(ctx, p.ID, "why did the lease expire", RecallOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, h := range hits {
		if h.Kind == "entry" {
			refs = append(refs, h.Ref)
		}
	}
	for _, h := range recalled {
		if h.Kind == "entry" {
			refs = append(refs, h.Ref)
		}
	}
	hit, err := c.EntryHit(ctx, entry.ID, p.ID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if hit.Kind == "entry" {
		refs = append(refs, hit.Ref)
	}
	if len(refs) != 3 {
		t.Fatalf("refs = %v, want one from search, recall and the vector lookup", refs)
	}
	for _, ref := range refs {
		if ref != want {
			t.Errorf("ref = %q, want %q", ref, want)
		}
	}
}

// A vault entry that links somewhere is named by its vault address in that
// target's backlinks, not by the key of the project it came from.
func TestBacklinksNameAVaultSourceByItsVaultAddress(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	target, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Source", Body: "See [[target]].\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromoteEntry(ctx, p.ID, source.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/GLOBAL/vault/source" {
		t.Errorf("backlinks = %+v, want the vault address", back)
	}
}

func TestGraphNamesEntriesByAddress(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Target"}); err != nil {
		t.Fatal(err)
	}
	source, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Source", Body: "See [[target]].\n"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := c.Traverse(ctx, source.ID, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		seen[n.Ref] = true
		if n.Type != "entry" {
			t.Errorf("node %s has type %q, want entry", n.Ref, n.Type)
		}
	}
	if !seen["/XPSCTL/vault/source"] || !seen["/XPSCTL/vault/target"] {
		t.Errorf("nodes = %+v", g.Nodes)
	}
}
