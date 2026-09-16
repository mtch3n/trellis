package core

import (
	"testing"
)

func TestRelateCardsAllRelations(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	// Test each relation type
	rels := []string{"resolved_by", "duplicate_of", "relates_to"}
	for _, rel := range rels {
		if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, rel, CardRef{Seq: d.Seq}); err != nil {
			t.Fatalf("RelateCards %s: %v", rel, err)
		}
		relations, err := c.CardRelations(t.Context(), a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(relations) == 0 {
			t.Fatalf("CardRelations after adding %s: got empty", rel)
		}
		found := false
		for _, rel := range relations {
			if rel.Ref == d.Ref {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("CardRelations: %s not found in relations", d.Ref)
		}
		// Clean up for next iteration
		c.UnrelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, rel, CardRef{Seq: d.Seq})
	}
}

func TestRelateCardsInverseView(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	// Create resolved_by link: a is resolved by d
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "resolved_by", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}

	// From a's perspective, d resolves a
	relationsA, _ := c.CardRelations(t.Context(), a.ID)
	if len(relationsA) != 1 || relationsA[0].Rel != "resolved_by" {
		t.Fatalf("From a: expected resolved_by, got %+v", relationsA)
	}

	// From d's perspective, a is resolved by d (inverse: d resolves a)
	relationsD, _ := c.CardRelations(t.Context(), d.ID)
	if len(relationsD) != 1 || relationsD[0].Rel != "resolves" {
		t.Fatalf("From d: expected resolves, got %+v", relationsD)
	}
}

func TestRelateCardsSymmetricRelatesToStoredOnce(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	// Create relates_to from a to d
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "relates_to", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}

	// Creating it in reverse should be a no-op
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: d.Seq}, "relates_to", CardRef{Seq: a.Seq}); err != nil {
		t.Fatal(err)
	}

	// Both should see the same relationship (exactly one link stored)
	relationsA, _ := c.CardRelations(t.Context(), a.ID)
	relationsD, _ := c.CardRelations(t.Context(), d.ID)
	if len(relationsA) != 1 || len(relationsD) != 1 {
		t.Fatalf("Expected 1 relation from each, got A=%d, D=%d", len(relationsA), len(relationsD))
	}
	if relationsA[0].Rel != "relates_to" || relationsD[0].Rel != "relates_to" {
		t.Fatalf("Expected relates_to from both sides")
	}
}

func TestRelateCardsSelfRefused(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})

	err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "resolved_by", CardRef{Seq: a.Seq})
	if err == nil {
		t.Fatal("RelateCards(self) succeeded, want an error")
	}
}

func TestRelateCardsUnknownRelRefused(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "unknown_rel", CardRef{Seq: d.Seq})
	if err == nil {
		t.Fatal("RelateCards(unknown) succeeded, want an error")
	}
}

func TestBlockedByStillRefusesCycle(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"})

	// Using RelateCards for blocked_by should go through BlockCard and check cycles
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "blocked_by", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: d.Seq}, "blocked_by", CardRef{Seq: a.Seq}); err == nil {
		t.Fatal("blocked_by cycle accepted")
	}
}

func TestUnrelateCards(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "resolved_by", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.UnrelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "resolved_by", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}
	relations, _ := c.CardRelations(t.Context(), a.ID)
	if len(relations) != 0 {
		t.Fatalf("After unrelate, got %d relations, want 0", len(relations))
	}
}

func TestRelateCardRecordsEvent(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "resolved_by", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}

	var action, field, newValue string
	if err := c.db.Get(&action,
		`SELECT action FROM event WHERE entity_type='card' AND entity_id=? AND action='related' ORDER BY seq DESC LIMIT 1`, a.ID); err != nil {
		t.Fatalf("no relate event recorded: %v", err)
	}
	if err := c.db.Get(&field,
		`SELECT field FROM event WHERE entity_type='card' AND entity_id=? AND action='related' ORDER BY seq DESC LIMIT 1`, a.ID); err != nil {
		t.Fatalf("field not recorded: %v", err)
	}
	if field != "resolved_by" {
		t.Errorf("field = %q, want resolved_by", field)
	}
	if err := c.db.Get(&newValue,
		`SELECT new_value FROM event WHERE entity_type='card' AND entity_id=? AND action='related' ORDER BY seq DESC LIMIT 1`, a.ID); err != nil {
		t.Fatalf("new_value not recorded: %v", err)
	}
	if newValue != d.Ref {
		t.Errorf("new_value = %q, want %s", newValue, d.Ref)
	}
}

func TestCardRelationsOrdering(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d1, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d1"})
	d2, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d2"})
	d3, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d3"})

	// Create various relations: resolved_by d1, duplicate_of d2, relates_to d3
	c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "resolved_by", CardRef{Seq: d1.Seq})
	c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "duplicate_of", CardRef{Seq: d2.Seq})
	c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "relates_to", CardRef{Seq: d3.Seq})

	relations, _ := c.CardRelations(t.Context(), a.ID)

	// Should be sorted by rel then ref
	if len(relations) != 3 {
		t.Fatalf("Expected 3 relations, got %d", len(relations))
	}

	expectedOrder := []string{"duplicate_of", "relates_to", "resolved_by"}
	for i, expected := range expectedOrder {
		if relations[i].Rel != expected {
			t.Errorf("relations[%d].Rel = %q, want %q", i, relations[i].Rel, expected)
		}
	}
}

func TestCardRelationsDoneColumn(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	// d is initially in the first column (backlog)
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "resolved_by", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}

	relations, _ := c.CardRelations(t.Context(), a.ID)
	if len(relations) != 1 {
		t.Fatal("expected 1 relation")
	}
	if relations[0].Done {
		t.Errorf("d should not be in done column initially")
	}

	// Move d to the done column
	cols, _ := c.ListColumns(t.Context(), b.ID)
	var doneCol *Column
	for _, col := range cols {
		if col.IsDone {
			doneCol = &col
			break
		}
	}
	if doneCol == nil {
		t.Fatal("no done column found")
	}

	c.MoveCard(t.Context(), p.ID, b.ID, CardRef{Seq: d.Seq}, doneCol.Name)

	relations, _ = c.CardRelations(t.Context(), a.ID)
	if len(relations) != 1 {
		t.Fatal("expected 1 relation after move")
	}
	if !relations[0].Done {
		t.Errorf("d should now be in done column")
	}
}
