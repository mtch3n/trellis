package core

import "testing"

func TestTraverseFollowsBlockedChains(t *testing.T) {
	c, p, b := kbCore(t)
	cards, err := c.ImportCards(t.Context(), p.ID, b.ID, []ImportCard{
		{ID: "c", Title: "third"},
		{ID: "b", Title: "second", BlockedBy: []string{"c"}},
		{ID: "a", Title: "first", BlockedBy: []string{"b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := cards[2]

	// One hop sees only the direct blocker.
	shallow, err := c.Traverse(t.Context(), a.ID, 1, []string{"blocked_by"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(shallow.Nodes) != 2 {
		t.Errorf("depth 1 reached %d nodes, want 2", len(shallow.Nodes))
	}

	deep, err := c.Traverse(t.Context(), a.ID, 3, []string{"blocked_by"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(deep.Nodes) != 3 {
		t.Fatalf("depth 3 reached %d nodes, want the whole chain", len(deep.Nodes))
	}
	if len(deep.Edges) != 2 {
		t.Errorf("edges = %+v, want 2", deep.Edges)
	}

	// Reverse answers "what breaks if I change this".
	rev, err := c.Traverse(t.Context(), cards[0].ID, 3, []string{"blocked_by"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Nodes) != 3 {
		t.Errorf("reverse reached %d nodes, want 3", len(rev.Nodes))
	}
}
