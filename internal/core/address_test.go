package core

import "testing"

func TestDocAddress(t *testing.T) {
	if got := DocAddress("XPSCTL", false, "design"); got != "/XPSCTL/knowledge/design" {
		t.Errorf("project entry = %q", got)
	}
	if got := DocAddress("XPSCTL", true, "design"); got != "/GLOBAL/knowledge/design" {
		t.Errorf("vault entry = %q", got)
	}
}

// The SQL fragment and the Go helper must never disagree: search results are
// compared with and passed back to commands that use the Go form.
func TestDocAddressSQLMatchesGo(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Local entry"}); err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Vault entry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Slug   string `db:"slug"`
		Global bool   `db:"global"`
		Ref    string `db:"ref"`
	}
	if err := c.db.Select(&rows, `SELECT k.slug, k.global, `+docAddressSQL+` AS ref
		FROM knowledge k JOIN project p ON p.id = k.project_id ORDER BY k.slug`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		if want := DocAddress(p.Key, r.Global, r.Slug); r.Ref != want {
			t.Errorf("SQL address %q, Go address %q", r.Ref, want)
		}
	}
}

func TestSearchRecallAndVectorHitsCarryAddresses(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Lease renewal", Body: "A claim starts the lease.\n"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "/XPSCTL/knowledge/lease-renewal"
	if doc.Ref != want {
		t.Errorf("created ref = %q, want %q", doc.Ref, want)
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
		if h.Kind == "knowledge" {
			refs = append(refs, h.Ref)
		}
	}
	for _, h := range recalled {
		if h.Kind == "knowledge" {
			refs = append(refs, h.Ref)
		}
	}
	hit, err := c.KnowledgeHit(ctx, doc.ID, p.ID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	refs = append(refs, hit.Ref)
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
	c, p, _ := kbCore(t)
	ctx := t.Context()
	target, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Source", Body: "See [[target]].\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, source.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/GLOBAL/knowledge/source" {
		t.Errorf("backlinks = %+v, want the vault address", back)
	}
}

func TestGraphNamesEntriesByAddress(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()
	if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Target"}); err != nil {
		t.Fatal(err)
	}
	source, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Source", Body: "See [[target]].\n"})
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
	}
	if !seen["/XPSCTL/knowledge/source"] || !seen["/XPSCTL/knowledge/target"] {
		t.Errorf("nodes = %+v", g.Nodes)
	}
}
