package core

import "testing"

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
