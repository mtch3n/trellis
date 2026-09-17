package core

import "testing"

func TestPinFallsBackToSummaryAndGoesStale(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Concurrency model", Summary: "Leases, not locks", Body: "# Concurrency model\n\nBody.\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	pin, err := c.PinEntry(t.Context(), p.ID, "concurrency-model", "", "")
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
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "Rewritten entirely.\n", &entry.Version); err != nil {
		t.Fatal(err)
	}
	pins, err = c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || !pins[0].Stale {
		t.Fatalf("Pins = %+v, want the recap marked stale after the entry changed", pins)
	}

	if err := c.UnpinEntry(t.Context(), p.ID, entry.Slug, ""); err != nil {
		t.Fatal(err)
	}
	if pins, _ := c.Pins(t.Context(), p.ID, "", 0); len(pins) != 0 {
		t.Errorf("Pins after unpin = %+v, want none", pins)
	}
}
func TestPinWithoutBoardUpdatesExistingPin(t *testing.T) {
	c, p, _ := vaultCore(t)
	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Concurrency model", Summary: "Leases, not locks", Body: "# Concurrency model\n\nBody.\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Pin the same entry twice without a board
	_, err = c.PinEntry(t.Context(), p.ID, "concurrency-model", "", "")
	if err != nil {
		t.Fatalf("First PinKnowledge: %v", err)
	}

	pin2, err := c.PinEntry(t.Context(), p.ID, "concurrency-model", "Updated recap", "")
	if err != nil {
		t.Fatalf("Second PinKnowledge: %v", err)
	}

	// Should have only one pin
	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 {
		t.Errorf("Pins = %+v, want exactly one pin after pinning twice without board", pins)
	}
	if pins[0].Recap != "Updated recap" {
		t.Errorf("Pin recap = %q, want 'Updated recap'", pins[0].Recap)
	}
	if pins[0].CreatedAt != pin2.CreatedAt {
		t.Errorf("Pin created_at = %d, want %d (from second pin)", pins[0].CreatedAt, pin2.CreatedAt)
	}
}

func TestPinCreatedWhilePrivateThenUnprivateShowsStale(t *testing.T) {
	c, p, _ := vaultCore(t)

	// Create an entry
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Secret credentials", Body: "private\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Set it as private by editing the file
	setPrivateInFile(t, entry.Path, true)

	// Pin while private (creates pin with NULL recap)
	pin, err := c.PinEntry(t.Context(), p.ID, entry.Slug, "", "")
	if err != nil {
		t.Fatalf("PinKnowledge: %v", err)
	}
	if pin.Recap != "" {
		t.Errorf("Private pin recap = %q, want empty", pin.Recap)
	}

	// Verify it's not stale while private
	pins, err := c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var foundPin Pin
	for _, p := range pins {
		if p.Slug == entry.Slug {
			foundPin = p
			break
		}
	}
	if foundPin.Slug == "" {
		t.Fatal("pin not found")
	}
	if foundPin.Stale {
		t.Error("private pin is marked stale, want not stale")
	}

	// Now mark as non-private by editing the file
	setPrivateInFile(t, entry.Path, false)

	// Check pins again - should now be stale (has no recap but is non-private)
	pins, err = c.Pins(t.Context(), p.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pins {
		if p.Slug == entry.Slug {
			foundPin = p
			break
		}
	}
	if !foundPin.Stale {
		t.Error("non-private pin with no recap is not marked stale, want stale")
	}

	// Health count should also report it
	health, err := c.Health(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var staleCount int
	for _, line := range health {
		if line.What == "stale pinned recaps" {
			staleCount = line.Count
			break
		}
	}
	if staleCount == 0 {
		t.Error("health count 0 stale recaps, want 1: pin with no recap on non-private entry")
	}
}

func TestEscalateMovesTheEntryAndKeepsReferences(t *testing.T) {
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
	if !moved.Global || moved.Ref != "/GLOBAL/vault/postgres-conventions" {
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

	if _, err := c.DemoteEntry(t.Context(), target.Slug, "wrong call"); err != nil {
		t.Fatalf("DemoteKnowledge: %v", err)
	}
}
