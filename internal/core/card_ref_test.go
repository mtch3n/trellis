package core

import (
	"testing"
)

// twoProjectsWithCards sets up two projects with default boards and a card in each.
func twoProjectsWithCards(t *testing.T) (*Core, Project, Board) {
	c := testCore(t)
	ctx := t.Context()

	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	// Create a card in the first project
	_, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "first"})
	if err != nil {
		t.Fatal(err)
	}

	// Create a second project
	p2 := seededProject2(t, c)
	seededBoard(t, c, p2)

	return c, p, b
}

// moveCardTo relocates a card by hand, the way a project merge does.
func moveCardTo(t *testing.T, c *Core, cardID string, p Project, seq int64) Board {
	t.Helper()
	b, err := c.SelectBoard(t.Context(), p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var col string
	if err := c.db.Get(&col, `SELECT id FROM column_ WHERE board_id = ? ORDER BY position LIMIT 1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`UPDATE card SET project_id = ?, board_id = ?, column_id = ?, seq = ? WHERE id = ?`,
		p.ID, b.ID, col, seq, cardID); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestACardKeepsItsRefInAnotherProject(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	other, err := c.ProjectByKey(ctx, "OTHERPROJ")
	if err != nil {
		t.Fatal(err)
	}
	local, err := c.GetCard(ctx, p.ID, CardRef{Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	ob := moveCardTo(t, c, local.ID, other, 2)

	// After moving a card to another project, it can be found using its original ref
	got, err := c.GetCard(ctx, other.ID, ParseCardRef("xpsctl-1"))
	if err != nil || got.ID != local.ID || got.Ref != "XPSCTL-1" {
		t.Fatalf("XPSCTL-1 in OTHERPROJ = %+v, %v", got, err)
	}

	// CardHolder can find where a moved card went
	holder, found, err := c.CardHolder(ctx, "xpsctl-1")
	if err != nil || !found || holder.Key != "OTHERPROJ" {
		t.Errorf("CardHolder = %+v, %v, %v", holder, found, err)
	}
	if _, found, _ := c.CardHolder(ctx, "12"); found {
		t.Error("a bare number has no holder")
	}

	// Next card in the destination project has the correct sequence
	next, err := c.CreateCard(ctx, other.ID, ob.ID, NewCard{Title: "next"})
	if err != nil || next.Ref != "OTHERPROJ-3" {
		t.Errorf("next card = %s, %v; want OTHERPROJ-3", next.Ref, err)
	}
}

func TestListingsShowTheStoredRef(t *testing.T) {
	c, p, pb := twoProjectsWithCards(t)
	ctx := t.Context()
	blocked, err := c.CreateCard(ctx, p.ID, pb.ID, NewCard{Title: "waits on moved"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BlockCard(ctx, p.ID, ParseCardRef(blocked.Ref), CardRef{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Moved notes", Body: "about the sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LinkCardToEntry(ctx, p.ID, CardRef{Seq: 1}, entry.Slug); err != nil {
		t.Fatal(err)
	}
	// Rewrite the stored ref so every listing must read the column: nothing
	// can reach "RENAMED-9" by joining key and seq.
	if _, err := c.db.Exec(`UPDATE card SET ref = 'RENAMED-9' WHERE project_id = ? AND seq = 1`, p.ID); err != nil {
		t.Fatal(err)
	}

	blockers, err := c.Blockers(ctx, blocked.ID)
	if err != nil || len(blockers) != 1 || blockers[0].Ref != "RENAMED-9" {
		t.Errorf("Blockers = %+v, %v", blockers, err)
	}
	links, err := c.Backlinks(ctx, entry.ID)
	if err != nil || len(links) != 1 || links[0].Ref != "RENAMED-9" {
		t.Errorf("Backlinks = %+v, %v", links, err)
	}
	var id string
	if err := c.db.Get(&id, `SELECT id FROM card WHERE ref = 'RENAMED-9'`); err != nil {
		t.Fatal(err)
	}
	g, err := c.Traverse(ctx, id, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range g.Nodes {
		found = found || (n.ID == id && n.Ref == "RENAMED-9")
	}
	if !found {
		t.Errorf("Traverse nodes = %+v, want the start card as RENAMED-9", g.Nodes)
	}
}
