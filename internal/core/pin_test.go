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

	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins[0].Stale {
		t.Fatalf("Pins = %+v, want one fresh pin", pins)
	}

	// The entry moves on; the recap does not. That must be visible.
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "Rewritten entirely.\n", &doc.Version); err != nil {
		t.Fatal(err)
	}
	pins, err = c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || !pins[0].Stale {
		t.Fatalf("Pins = %+v, want the recap marked stale after the entry changed", pins)
	}

	if err := c.UnpinKnowledge(t.Context(), p.ID, doc.Slug, ""); err != nil {
		t.Fatal(err)
	}
	if pins, _ := c.Pins(t.Context(), p.ID, "", 0); len(pins) != 0 {
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
	if !moved.Global || moved.Ref != "/GLOBAL/knowledge/postgres-conventions" {
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
