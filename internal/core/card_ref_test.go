package core

import (
	"errors"
	"strings"
	"testing"
)

// twoProjectsWithCards gives XPSCTL and OTHERPROJ one card each, both seq 1.
func twoProjectsWithCards(t *testing.T) (*Core, Project, Board) {
	t.Helper()
	c := testCore(t)
	p := seededProject(t, c)
	pb := seededBoard(t, c, p)
	o := seededProject2(t, c)
	ob := seededBoard(t, c, o)
	if _, err := c.CreateCard(t.Context(), p.ID, pb.ID, NewCard{Title: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCard(t.Context(), o.ID, ob.ID, NewCard{Title: "remote"}); err != nil {
		t.Fatal(err)
	}
	return c, p, pb
}

func TestAQualifiedRefIsNeverRescoped(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	for _, ref := range []string{"1", "XPSCTL-1", "xpsctl-1", "/XPSCTL/cards/XPSCTL-1"} {
		card, err := c.GetCard(ctx, p.ID, ParseCardRef(ref))
		if err != nil || card.Ref != "XPSCTL-1" {
			t.Errorf("GetCard(%q) = %s, %v", ref, card.Ref, err)
		}
	}

	_, err := c.GetCard(ctx, p.ID, ParseCardRef("OTHERPROJ-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Fatalf("OTHERPROJ-1 in XPSCTL: code = %s, want wrong_project", got)
	}
	if te, _ := errors.AsType[*Error](err); !strings.Contains(te.Fix, "/OTHERPROJ/cards/OTHERPROJ-1") {
		t.Errorf("fix = %q, want the card's address", te.Fix)
	}

	_, err = c.GetCard(ctx, p.ID, ParseCardRef("NOPE-1"))
	if got := errCode(t, err); got != "card_not_found" {
		t.Errorf("a project that does not exist: code = %s", got)
	}
}

// An address names its project even when its ref's prefix is the current
// key: /OTHERPROJ/cards/XPSCTL-1 is not XPSCTL's card 1.
func TestACardAddressKeepsItsProject(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	_, err := c.GetCard(ctx, p.ID, ParseCardRef("/OTHERPROJ/cards/XPSCTL-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Fatalf("code = %s, want wrong_project", got)
	}
	if te, _ := errors.AsType[*Error](err); !strings.Contains(te.Msg, "OTHERPROJ") {
		t.Errorf("message = %q, want it to name OTHERPROJ", te.Msg)
	}
	_, err = c.GetCard(ctx, p.ID, ParseCardRef("/NOPE/cards/XPSCTL-1"))
	if got := errCode(t, err); got != "card_not_found" {
		t.Errorf("an address to a missing project: code = %s", got)
	}
	// And the prefix check still holds inside the right project.
	_, err = c.GetCard(ctx, p.ID, ParseCardRef("/XPSCTL/cards/OTHERPROJ-1"))
	if got := errCode(t, err); got != "wrong_project" {
		t.Errorf("a foreign prefix under the right project: code = %s", got)
	}
}

func TestABlockerMustBeInTheSameProject(t *testing.T) {
	c, p, _ := twoProjectsWithCards(t)
	ctx := t.Context()
	err := c.BlockCard(ctx, p.ID, CardRef{Seq: 1}, ParseCardRef("OTHERPROJ-1"))
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("BlockCard: code = %s", got)
	}
	err = c.UnblockCard(ctx, p.ID, CardRef{Seq: 1}, ParseCardRef("OTHERPROJ-1"))
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("UnblockCard: code = %s", got)
	}
	// The address's project differs from the prefix, which is this project's.
	err = c.BlockCard(ctx, p.ID, CardRef{Seq: 1}, ParseCardRef("/OTHERPROJ/cards/XPSCTL-1"))
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("BlockCard by a foreign address: code = %s", got)
	}
}

func TestImportRefusesABlockerInAnotherProject(t *testing.T) {
	c, p, b := twoProjectsWithCards(t)
	ctx := t.Context()
	_, err := c.ImportCards(ctx, p.ID, b.ID, []ImportCard{{Title: "waits", BlockedBy: []string{"OTHERPROJ-1"}}})
	if got := errCode(t, err); got != "cross_project_block" {
		t.Errorf("foreign blocker: code = %s", got)
	}
	_, err = c.ImportCards(ctx, p.ID, b.ID, []ImportCard{{Title: "waits", BlockedBy: []string{"typo-1"}}})
	if got := errCode(t, err); got != "unknown_blocker" {
		t.Errorf("a handle that is no card: code = %s", got)
	}
	cards, err := c.ImportCards(ctx, p.ID, b.ID, []ImportCard{{Title: "waits", BlockedBy: []string{"/XPSCTL/cards/XPSCTL-1"}}})
	if err != nil || len(cards) != 1 {
		t.Errorf("a blocker given as an address: %v", err)
	}
}
