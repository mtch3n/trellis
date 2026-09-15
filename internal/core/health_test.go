package core

import "testing"

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
