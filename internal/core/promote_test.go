package core

import "testing"

func TestPromoteMovesTheEntryAndKeepsReferences(t *testing.T) {
	c, p, _ := vaultCore(t)
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Postgres conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Setup", Body: "Follow [[postgres-conventions]].\n"}); err != nil {
		t.Fatal(err)
	}

	moved, err := c.PromoteEntry(t.Context(), p.ID, target.Slug, "needed from three projects")
	if err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if !moved.Global || moved.Ref != "/GLOBAL/vault/postgres-conventions" {
		t.Errorf("promoted entry = %+v, want a global ref", moved)
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
		t.Errorf("Backlinks after promotion = %+v, want the reference to survive", back)
	}
	if findings, _ := c.Lint(t.Context(), p.ID); len(findings) != 0 {
		t.Errorf("lint = %+v, want no stub: the reference still resolves", findings)
	}

	if _, err := c.DemoteEntry(t.Context(), target.Slug, "wrong call"); err != nil {
		t.Fatalf("DemoteEntry: %v", err)
	}
}
