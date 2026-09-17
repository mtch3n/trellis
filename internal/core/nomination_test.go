package core

import "testing"

func TestNominateEntryCountsCitations(t *testing.T) {
	c, p, _ := vaultCore(t)
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Postgres conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Setup", Body: "Follow [[postgres-conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	if err := c.NominateEntry(t.Context(), p.ID, target.Slug, "every repo re-derives this"); err != nil {
		t.Fatal(err)
	}
	nominees, err := c.Nominations(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nominees) != 1 || nominees[0].Cited != 1 || nominees[0].Nominations != 1 {
		t.Fatalf("Nominations = %+v, want one with the citation counted", nominees)
	}
}
