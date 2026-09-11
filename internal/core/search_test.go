package core

import (
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestSearchCardsFindsCardsByTitleWord(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Create some test cards
	card1, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Implement authentication system"})
	if err != nil {
		t.Fatal(err)
	}

	card2, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Fix login bug"})
	if err != nil {
		t.Fatal(err)
	}

	card3, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Deploy website"})
	if err != nil {
		t.Fatal(err)
	}

	results, err := c.SearchCards(ctx, p.ID, "authentication", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Errorf("SearchCards returned %d results, want 1", len(results))
	}
	if len(results) > 0 && results[0].ID != card1.ID {
		t.Errorf("SearchCards found wrong card: got %s, want %s", results[0].ID, card1.ID)
	}

	// Search for "login" should find card2
	results, err = c.SearchCards(ctx, p.ID, "login", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("SearchCards 'login' returned %d results, want 1", len(results))
	}
	if len(results) > 0 && results[0].ID != card2.ID {
		t.Errorf("SearchCards 'login' found wrong card: got %s, want %s", results[0].ID, card2.ID)
	}

	// Verify that card3 is not found
	results, err = c.SearchCards(ctx, p.ID, "authentication", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ID == card3.ID {
			t.Errorf("SearchCards incorrectly returned card3 (deploy website) for 'authentication'")
		}
	}
}

func TestSearchCardsFindsCardsByBodyWord(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	card1, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Refactor module",
		Body:  "This needs significant cryptography changes",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Update docs",
		Body:  "Write about the new API endpoints",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Search for a word in the body
	results, err := c.SearchCards(ctx, p.ID, "cryptography", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Errorf("SearchCards returned %d results, want 1", len(results))
	}
	if len(results) > 0 && results[0].ID != card1.ID {
		t.Errorf("SearchCards found wrong card: got %s, want %s", results[0].ID, card1.ID)
	}
}

func TestSearchCardsRanksTitleAboveBody(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Card with "search" in title
	card1, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Implement search feature",
		Body:  "This is about other things",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Card with "search" in body only
	_, err = c.CreateCard(ctx, p.ID, b.ID, NewCard{
		Title: "Other task",
		Body:  "We need search capability for this feature",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := c.SearchCards(ctx, p.ID, "search", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Errorf("SearchCards returned %d results, want 2", len(results))
		return
	}

	// card1 should rank higher because "search" is in the title
	if results[0].ID != card1.ID {
		t.Errorf("SearchCards ranked card2 first, but card1 has 'search' in title (higher rank expected)")
	}
}

func TestSearchCardsRespectProjectScoping(t *testing.T) {
	c := testCore(t)
	p1 := seededProject(t, c)
	p2 := seededProject2(t, c)
	b1 := seededBoard(t, c, p1)
	b2 := seededBoard(t, c, p2)
	ctx := t.Context()

	card1, err := c.CreateCard(ctx, p1.ID, b1.ID, NewCard{Title: "unique title xyz"})
	if err != nil {
		t.Fatal(err)
	}

	card2, err := c.CreateCard(ctx, p2.ID, b2.ID, NewCard{Title: "different task"})
	if err != nil {
		t.Fatal(err)
	}

	// Search in p1 should find card1
	results, err := c.SearchCards(ctx, p1.ID, "unique", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Errorf("SearchCards in p1 returned %d results, want 1", len(results))
		return
	}
	if results[0].ID != card1.ID {
		t.Errorf("SearchCards in p1 got wrong card: %s vs %s", results[0].ID, card1.ID)
	}

	// Search in p2 should not find card1
	results, err = c.SearchCards(ctx, p2.ID, "unique", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 0 {
		t.Errorf("SearchCards in p2 should not find card from p1, got %d results", len(results))
	}

	// Search in p2 should find card2
	results, err = c.SearchCards(ctx, p2.ID, "different", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 || results[0].ID != card2.ID {
		t.Errorf("SearchCards in p2 should find card2")
	}
}

func TestSearchCardsExcludesArchived(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	card1, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Active card"})
	if err != nil {
		t.Fatal(err)
	}

	card2, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Archived card"})
	if err != nil {
		t.Fatal(err)
	}

	// Archive card2 by setting archived_at directly
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec("UPDATE card SET archived_at = ? WHERE id = ?", c.clock.NowMS(), card2.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Search for "card" should only find card1
	results, err := c.SearchCards(ctx, p.ID, "card", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Errorf("SearchCards returned %d results, want 1", len(results))
		return
	}
	if results[0].ID != card1.ID {
		t.Errorf("SearchCards found archived card: %s", results[0].ID)
	}
}

func TestFindSimilarOpenCards(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	// Get the done column
	cols, err := c.ListColumns(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	var doneCol *Column
	for i := range cols {
		if cols[i].IsDone {
			doneCol = &cols[i]
			break
		}
	}

	// Create open card with similar title
	openCard, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Implement feature X"})
	if err != nil {
		t.Fatal(err)
	}

	// Create another open card
	otherCard, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Different task"})
	if err != nil {
		t.Fatal(err)
	}

	// Create archived card with similar title
	archivedCard, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Implement feature Y"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec("UPDATE card SET archived_at = ? WHERE id = ?", c.clock.NowMS(), archivedCard.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Move otherCard to done column if one exists
	if doneCol != nil {
		_, err := c.MoveCard(ctx, p.ID, b.ID, CardRef{UUID: otherCard.ID}, doneCol.Name)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Search for similar titles should find openCard only
	results, err := c.FindSimilarOpenCards(ctx, p.ID, "feature", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Should find at least openCard (and possibly others with "feature" in title)
	found := false
	for _, r := range results {
		if r.ID == openCard.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FindSimilarOpenCards should find the open card with 'feature' in title")
	}

	// Should not find archived card
	for _, r := range results {
		if r.ID == archivedCard.ID {
			t.Errorf("FindSimilarOpenCards should not find archived card")
		}
	}
}

func TestFindSimilarOpenCardsExcludesDoneColumn(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()

	openCard, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Fix bug"})
	if err != nil {
		t.Fatal(err)
	}

	doneCard, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Fix bug elsewhere"})
	if err != nil {
		t.Fatal(err)
	}

	// Get the done column
	cols, err := c.ListColumns(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	var doneCol *Column
	for i := range cols {
		if cols[i].IsDone {
			doneCol = &cols[i]
			break
		}
	}

	// Move doneCard to done column if one exists
	if doneCol != nil {
		_, err := c.MoveCard(ctx, p.ID, b.ID, CardRef{UUID: doneCard.ID}, doneCol.Name)
		if err != nil {
			t.Fatal(err)
		}

		// Search should only find openCard
		results, err := c.FindSimilarOpenCards(ctx, p.ID, "bug", 10)
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, r := range results {
			if r.ID == openCard.ID {
				found = true
			}
			if r.ID == doneCard.ID {
				t.Errorf("FindSimilarOpenCards should not find card in done column")
			}
		}
		if !found {
			t.Errorf("FindSimilarOpenCards should find the open card")
		}
	}
}
