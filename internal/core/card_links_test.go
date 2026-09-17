package core

import (
	"testing"
)

// A card lists the entries it cites, by address, and loses one on unlink,
// whether it is named the way it was linked or by its address.
func TestCardLinksAndUnlink(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Runbook", Body: "# Runbook\n\n## Steps\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Design"}); err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	ref := CardRef{UUID: card.ID}
	for _, target := range []string{"runbook#steps", "design"} {
		if err := c.LinkCardToEntry(ctx, p.ID, ref, target); err != nil {
			t.Fatal(err)
		}
	}

	links, err := c.CardLinks(ctx, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 || links[0].Raw != "runbook#steps" || links[0].Anchor != "steps" ||
		links[0].To == nil || *links[0].To != "/XPSCTL/vault/runbook" || links[0].Title != "Runbook" {
		t.Fatalf("CardLinks = %+v", links)
	}

	if err := c.UnlinkCardFromEntry(ctx, p.ID, ref, "runbook#steps"); err != nil {
		t.Fatalf("unlink by raw: %v", err)
	}
	if err := c.UnlinkCardFromEntry(ctx, p.ID, ref, "/XPSCTL/vault/design"); err != nil {
		t.Fatalf("unlink by address: %v", err)
	}
	if links, _ := c.CardLinks(ctx, card.ID); len(links) != 0 {
		t.Errorf("links left after unlinking both: %+v", links)
	}
	if err := c.UnlinkCardFromEntry(ctx, p.ID, ref, "design"); !isCode(err, "not_linked") {
		t.Errorf("unlinking twice = %v, want not_linked", err)
	}
}

// A cited entry that is deleted leaves a stub the card still lists.
func TestCardLinksKeepsAStubForADeletedEntry(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Runbook"}); err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LinkCardToEntry(ctx, p.ID, CardRef{UUID: card.ID}, "runbook"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEntry(ctx, p.ID, "runbook"); err != nil {
		t.Fatal(err)
	}
	links, err := c.CardLinks(ctx, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].To != nil || links[0].Raw != "runbook" {
		t.Fatalf("CardLinks = %+v, want one stub", links)
	}
	if err := c.UnlinkCardFromEntry(ctx, p.ID, CardRef{UUID: card.ID}, "runbook"); err != nil {
		t.Errorf("a stub unlinks by its raw text: %v", err)
	}
}
