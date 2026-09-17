package core

import "testing"

func TestDuplicatesClustersOverlappingTitles(t *testing.T) {
	c, p, _ := vaultCore(t)
	for _, title := range []string{
		"Postgres user conventions", "Postgres user naming conventions", "Arc GPU power",
	} {
		if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
			Title: title, Body: "Body for " + title + ".\n"}); err != nil {
			t.Fatal(err)
		}
	}
	clusters, err := c.Duplicates(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(clusters) != 1 || len(clusters[0].Slugs) != 2 {
		t.Fatalf("clusters = %+v, want the two postgres entries together", clusters)
	}
}
