package core

import "testing"

func TestPinFallsBackToSummaryAndGoesStale(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Concurrency model", Summary: "Leases, not locks", Body: "# Concurrency model\n\nBody.\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	pin, err := c.PinKnowledge(t.Context(), p.ID, "concurrency-model", "", "")
	if err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}
	if pin.Recap != "Leases, not locks" {
		t.Errorf("Recap = %q, want the frontmatter summary", pin.Recap)
	}

	pins, err := c.Pins(t.Context(), p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins[0].Stale {
		t.Fatalf("Pins = %+v, want one fresh pin", pins)
	}

	// The entry moves on; the recap does not. That must be visible.
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "Rewritten entirely.\n", nil); err != nil {
		t.Fatal(err)
	}
	pins, err = c.Pins(t.Context(), p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || !pins[0].Stale {
		t.Fatalf("Pins = %+v, want the recap marked stale after the entry changed", pins)
	}

	if err := c.UnpinKnowledge(t.Context(), p.ID, doc.Slug, ""); err != nil {
		t.Fatal(err)
	}
	if pins, _ := c.Pins(t.Context(), p.ID, ""); len(pins) != 0 {
		t.Errorf("Pins after unpin = %+v, want none", pins)
	}
}

func TestEscalateMovesTheEntryAndKeepsReferences(t *testing.T) {
	c, p, _ := kbCore(t)
	target, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Postgres conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Setup", Body: "Follow [[postgres-conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	if err := c.NominateKnowledge(t.Context(), p.ID, target.Slug, "every repo re-derives this"); err != nil {
		t.Fatal(err)
	}
	noms, err := c.Nominations(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(noms) != 1 || noms[0].Cited != 1 || noms[0].Noms != 1 {
		t.Fatalf("Nominations = %+v, want one with the citation counted", noms)
	}

	moved, err := c.EscalateKnowledge(t.Context(), p.ID, target.Slug, "needed from three projects")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if !moved.Global || moved.Ref != "GLOBAL/postgres-conventions" {
		t.Errorf("escalated doc = %+v, want a global ref", moved)
	}
	if moved.Path == target.Path {
		t.Error("the file should have moved, not been copied")
	}

	// The existing reference still resolves: link.to_id stores identity (D35).
	back, err := c.Backlinks(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 {
		t.Errorf("Backlinks after escalation = %+v, want the reference to survive", back)
	}
	if findings, _ := c.Lint(t.Context(), p.ID); len(findings) != 0 {
		t.Errorf("lint = %+v, want no stub: the reference still resolves", findings)
	}

	if _, err := c.DemoteKnowledge(t.Context(), target.Slug, "wrong call"); err != nil {
		t.Fatalf("DemoteKnowledge: %v", err)
	}
}

func TestTraverseFollowsBlockedChains(t *testing.T) {
	c, p, b := kbCore(t)
	cards, err := c.ImportCards(t.Context(), p.ID, b.ID, []ImportCard{
		{ID: "c", Title: "third"},
		{ID: "b", Title: "second", BlockedBy: []string{"c"}},
		{ID: "a", Title: "first", BlockedBy: []string{"b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := cards[2]

	// One hop sees only the direct blocker.
	shallow, err := c.Traverse(t.Context(), a.ID, 1, []string{"blocked_by"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(shallow.Nodes) != 2 {
		t.Errorf("depth 1 reached %d nodes, want 2", len(shallow.Nodes))
	}

	deep, err := c.Traverse(t.Context(), a.ID, 3, []string{"blocked_by"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(deep.Nodes) != 3 {
		t.Fatalf("depth 3 reached %d nodes, want the whole chain", len(deep.Nodes))
	}
	if len(deep.Edges) != 2 {
		t.Errorf("edges = %+v, want 2", deep.Edges)
	}

	// Reverse answers "what breaks if I change this".
	rev, err := c.Traverse(t.Context(), cards[0].ID, 3, []string{"blocked_by"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Nodes) != 3 {
		t.Errorf("reverse reached %d nodes, want 3", len(rev.Nodes))
	}
}

func TestReadsCountOnlyDeliberateReads(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Postgres conventions", Body: "The naming rule.\n"})
	if err != nil {
		t.Fatal(err)
	}

	cold, err := c.ColdKnowledge(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cold) != 1 {
		t.Fatalf("cold = %+v, want the unread entry", cold)
	}

	// Lint and links resolve the entry without anyone reading it.
	if _, err := c.Lint(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatal(err)
	}
	if cold, _ := c.ColdKnowledge(t.Context(), p.ID); len(cold) != 1 {
		t.Error("an internal lookup must not count as a read")
	}

	if _, err := c.ReadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatal(err)
	}
	if cold, _ := c.ColdKnowledge(t.Context(), p.ID); len(cold) != 0 {
		t.Error("after a real read the entry is no longer cold")
	}

	if err := c.NominateKnowledge(t.Context(), p.ID, doc.Slug, "needed everywhere"); err != nil {
		t.Fatal(err)
	}
	noms, err := c.Nominations(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(noms) != 1 || noms[0].Reads != 1 || noms[0].Actors != 1 {
		t.Errorf("nomination = %+v, want reads=1 actors=1", noms)
	}

	lines, err := c.Health(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Count != 1 {
		t.Errorf("health = %+v, want one entry counted", lines)
	}
}

func TestDupesClustersOverlappingTitles(t *testing.T) {
	c, p, _ := kbCore(t)
	for _, title := range []string{
		"Postgres user conventions", "Postgres user naming conventions", "Arc GPU power",
	} {
		if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
			Title: title, Body: "Body for " + title + ".\n"}); err != nil {
			t.Fatal(err)
		}
	}
	clusters, err := c.Dupes(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Dupes: %v", err)
	}
	if len(clusters) != 1 || len(clusters[0].Slugs) != 2 {
		t.Fatalf("clusters = %+v, want the two postgres entries together", clusters)
	}
}
