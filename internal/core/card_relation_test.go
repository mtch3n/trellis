package core

import (
	"fmt"
	"testing"
)

func relationsOf(t *testing.T, c *Core, card Card) map[string]string {
	t.Helper()
	rels, err := c.CardRelations(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rels {
		out[r.Ref] = r.Rel
	}
	return out
}

// A relation is stored once and read from either side, whichever name the
// caller used to state it.
func TestCardRelationsReadFromBothSides(t *testing.T) {
	for _, tc := range []struct{ rel, fromA, fromB string }{
		{"blocked-by", "blocked_by", "blocks"},
		{"blocks", "blocks", "blocked_by"},
		{"resolved_by", "resolved_by", "resolves"},
		{"resolves", "resolves", "resolved_by"},
		{"duplicate-of", "duplicate_of", "duplicated_by"},
		{"duplicated_by", "duplicated_by", "duplicate_of"},
		{"relates-to", "relates_to", "relates_to"},
	} {
		t.Run(tc.rel, func(t *testing.T) {
			c := testCore(t)
			p := seededProject(t, c)
			b := seededBoard(t, c, p)
			a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
			d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

			if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, tc.rel, CardRef{Seq: d.Seq}); err != nil {
				t.Fatalf("RelateCards: %v", err)
			}
			if got := relationsOf(t, c, a); len(got) != 1 || got[d.Ref] != tc.fromA {
				t.Errorf("from a: %v, want %s %s", got, tc.fromA, d.Ref)
			}
			if got := relationsOf(t, c, d); len(got) != 1 || got[a.Ref] != tc.fromB {
				t.Errorf("from d: %v, want %s %s", got, tc.fromB, a.Ref)
			}

			// Stating it again, from either side, adds nothing.
			if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: d.Seq}, tc.fromB, CardRef{Seq: a.Seq}); err != nil {
				t.Fatalf("RelateCards from d: %v", err)
			}
			var links int
			if err := c.db.Get(&links, `SELECT COUNT(*) FROM link WHERE from_type = 'card' AND to_type = 'card'`); err != nil {
				t.Fatal(err)
			}
			if links != 1 {
				t.Errorf("%d links stored, want 1", links)
			}

			// Removing it from the other side removes it.
			if err := c.UnrelateCards(t.Context(), p.ID, CardRef{Seq: d.Seq}, tc.fromB, CardRef{Seq: a.Seq}); err != nil {
				t.Fatalf("UnrelateCards from d: %v", err)
			}
			if got := relationsOf(t, c, a); len(got) != 0 {
				t.Errorf("after unrelate: %v", got)
			}
			err := c.UnrelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, tc.rel, CardRef{Seq: d.Seq})
			if code := errCode(t, err); code != "not_related" && code != "not_blocked" {
				t.Errorf("unrelating twice: %v", err)
			}
		})
	}
}

func TestRelateCardsRefusals(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d"})

	for _, tc := range []struct {
		rel   string
		other Card
		code  string
	}{
		{"fixes", d, "unknown_relation"},
		{"resolves", a, "self_relation"},
		{"blocks", a, "self_block"},
	} {
		err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, tc.rel, CardRef{Seq: tc.other.Seq})
		if code := errCode(t, err); code != tc.code {
			t.Errorf("%s: code = %q (%v), want %s", tc.rel, code, err, tc.code)
		}
	}

	// blocks is blocked_by seen from the other side, so it refuses cycles too.
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "blocked_by", CardRef{Seq: d.Seq}); err != nil {
		t.Fatal(err)
	}
	err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "blocks", CardRef{Seq: d.Seq})
	if code := errCode(t, err); code != "blocked_cycle" {
		t.Errorf("cycle through blocks: %v", err)
	}
}

func TestCardRelationsListDoneStateAndOrder(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	a, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"})
	d1, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d1"})
	d2, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d2"})
	d3, _ := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "d3"})
	for _, r := range []struct {
		rel   string
		other Card
	}{{"resolved_by", d1}, {"duplicate_of", d2}, {"relates_to", d3}} {
		if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, r.rel, CardRef{Seq: r.other.Seq}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.MoveCard(t.Context(), p.ID, b.ID, CardRef{Seq: d1.Seq}, "done"); err != nil {
		t.Fatal(err)
	}

	rels, err := c.CardRelations(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []CardRelation{
		{Rel: "duplicate_of", Ref: d2.Ref, Title: "d2", Column: "backlog"},
		{Rel: "relates_to", Ref: d3.Ref, Title: "d3", Column: "backlog"},
		{Rel: "resolved_by", Ref: d1.Ref, Title: "d1", Column: "done", Done: true},
	}
	if len(rels) != len(want) {
		t.Fatalf("relations = %+v, want %+v", rels, want)
	}
	for i := range want {
		if rels[i] != want[i] {
			t.Errorf("relations[%d] = %+v, want %+v", i, rels[i], want[i])
		}
	}

	var field, value string
	if err := c.db.QueryRow(`SELECT field, new_value FROM event
		WHERE entity_id = ? AND action = 'related' ORDER BY seq LIMIT 1`, a.ID).Scan(&field, &value); err != nil {
		t.Fatal(err)
	}
	if field != "resolved_by" || value != d1.Ref {
		t.Errorf("related event = %s %s, want resolved_by %s", field, value, d1.Ref)
	}
}

func TestCardRelationsSortsNumerically(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	// Seqs 2 and 10: as strings, "-10" sorts before "-2".
	var cards []Card
	for i := 1; i <= 10; i++ {
		card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: fmt.Sprint("card ", i)})
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, card)
	}
	a, x2, x10 := cards[0], cards[1], cards[9]
	if x2.Seq != 2 || x10.Seq != 10 {
		t.Fatalf("seqs = %d, %d; want 2, 10", x2.Seq, x10.Seq)
	}

	// Relate a to both x2 and x10 with the same relation
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "relates_to", CardRef{Seq: x10.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := c.RelateCards(t.Context(), p.ID, CardRef{Seq: a.Seq}, "relates_to", CardRef{Seq: x2.Seq}); err != nil {
		t.Fatal(err)
	}

	rels, err := c.CardRelations(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify x2 comes before x10 (numeric sort, not string sort where X-10 < X-2)
	if len(rels) != 2 {
		t.Fatalf("expected 2 relations, got %d: %+v", len(rels), rels)
	}
	if rels[0].Ref != x2.Ref {
		t.Errorf("first relation ref = %q, want %q (numeric seq %d < %d)", rels[0].Ref, x2.Ref, x2.Seq, x10.Seq)
	}
	if rels[1].Ref != x10.Ref {
		t.Errorf("second relation ref = %q, want %q", rels[1].Ref, x10.Ref)
	}
}
