package platform

import (
	"strings"
	"testing"
	"uuid"
)

func TestPlat_PLAT_C16_AnotherMembersBoardIsNotListed(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	plans := w.must(w.p.NewBoard(ctx, a, "APP", "plans"))
	onPlans := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Board: "plans", Title: "secret plan"}))
	onPersonal := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "secret note"}))

	if !hasBoard(w.must(w.p.ListBoards(ctx, a, "APP")), plans.ID) {
		t.Fatal("ann does not list her own board")
	}
	boards := w.must(w.p.ListBoards(ctx, b, "APP"))
	for _, bd := range boards {
		if bd.OwnerID == a.MemberID {
			t.Fatalf("bob lists ann's board %q", bd.Name)
		}
	}
	cards := w.must(w.p.ListCards(ctx, b, "APP", ""))
	if hasCard(cards, onPlans.ID) || hasCard(cards, onPersonal.ID) {
		t.Fatal("bob lists ann's cards")
	}
	_, err := w.p.ListCards(ctx, b, "APP", "plans")
	wantCode(t, err, "board_not_found")
	_, err = w.p.ListCards(ctx, b, "APP", plans.ID.String())
	wantCode(t, err, "board_not_found")
}

func TestPlat_PLAT_C17_SharingShowsTheBoardAndUnsharingHidesIt(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	card := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "draft"}))
	w.must(w.p.CommentCard(ctx, a, "APP", card.Ref, "first thought"))

	shared := w.must(w.p.ShareBoard(ctx, a, "APP", "ann"))
	if shared.Scope != ScopeProject {
		t.Fatalf("scope after share = %q", shared.Scope)
	}
	if !hasBoard(w.must(w.p.ListBoards(ctx, b, "APP")), shared.ID) {
		t.Fatal("bob does not list the shared board")
	}
	if !hasCard(w.must(w.p.ListCards(ctx, b, "APP", "ann")), card.ID) {
		t.Fatal("bob does not list the card on the shared board")
	}
	seen := w.must(w.p.ShowCard(ctx, b, "APP", card.Ref))
	if len(seen.Comments) != 1 || seen.Comments[0].Body != "first thought" {
		t.Fatalf("bob reads comments %+v", seen.Comments)
	}
	if len(seen.Events) == 0 {
		t.Fatal("bob reads no events")
	}

	w.must(w.p.UnshareBoard(ctx, a, "APP", "ann"))
	if hasBoard(w.must(w.p.ListBoards(ctx, b, "APP")), shared.ID) {
		t.Fatal("bob still lists the board after unshare")
	}
	_, err := w.p.ShowCard(ctx, b, "APP", card.Ref)
	wantCode(t, err, "card_not_found")
	_, err = w.p.ListCards(ctx, b, "APP", "ann")
	wantCode(t, err, "board_not_found")
}

func TestPlat_PLAT_C18_OnlyTheOwnerSharesUnsharesOrRenames(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	w.must(w.p.NewBoard(ctx, a, "APP", "team"))
	before := w.must(w.p.ShareBoard(ctx, a, "APP", "team"))

	calls := map[string]func() (Board, error){
		"share":   func() (Board, error) { return w.p.ShareBoard(ctx, b, "APP", "team") },
		"unshare": func() (Board, error) { return w.p.UnshareBoard(ctx, b, "APP", "team") },
		"rename":  func() (Board, error) { return w.p.RenameBoard(ctx, b, "APP", "team", "mine") },
	}
	for name, call := range calls {
		_, err := call()
		if e := wantCode(t, err, "not_owner"); e.Kind != KindPolicy {
			t.Fatalf("%s: kind = %s", name, e.Kind)
		}
	}
	for _, bd := range w.must(w.p.ListBoards(ctx, a, "APP")) {
		if bd.ID == before.ID && (bd.Name != "team" || bd.Scope != ScopeProject || !bd.UpdatedAt.Equal(before.UpdatedAt)) {
			t.Fatalf("board changed: %+v", bd)
		}
	}
}

func TestPlat_PLAT_C19_InvisibleCardErrsLikeAMissingOne(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	for range 3 {
		w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "mine"}))
	}
	_, hidden := w.p.ShowCard(ctx, b, "APP", "APP-3")
	_, missing := w.p.ShowCard(ctx, b, "APP", "APP-999")
	h := wantCode(t, hidden, "card_not_found")
	m := wantCode(t, missing, "card_not_found")
	if h.Kind != m.Kind || h.Fix != m.Fix || h.Detail != nil || m.Detail != nil {
		t.Fatalf("errors differ: %+v vs %+v", h, m)
	}
	if strings.ReplaceAll(m.Msg, "APP-999", "APP-3") != h.Msg {
		t.Fatalf("messages differ: %q vs %q", h.Msg, m.Msg)
	}
}

func TestPlat_PLAT_C20_AdminsAndMaintainersDoNotSeePersonalBoards(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	w.exec(`UPDATE member SET role = 'admin' WHERE id = $1`, b.MemberID)
	w.exec(`UPDATE project_member SET role = 'maintainer' WHERE member_id = $1`, b.MemberID)
	card := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "mine"}))

	for _, bd := range w.must(w.p.ListBoards(ctx, b, "APP")) {
		if bd.OwnerID == a.MemberID {
			t.Fatal("the admin lists ann's personal board")
		}
	}
	_, err := w.p.ShowCard(ctx, b, "APP", card.Ref)
	wantCode(t, err, "card_not_found")
}

func TestPlat_PLAT_C21_AccessFollowsTheFixedOrder(t *testing.T) {
	org, other := uuid.New(), uuid.New()
	me, them := uuid.New(), uuid.New()
	agent := MemberAgent(org, me, "ann", "laptop", "")
	system := System(org, "job")
	foreignSystem := System(other, "job")
	mine := func(scope string) Target { return Target{OrgID: org, OwnerID: me, Scope: scope} }
	theirs := func(scope string) Target { return Target{OrgID: org, OwnerID: them, Scope: scope} }
	elsewhere := Target{OrgID: other, OwnerID: me, Scope: ScopeProject}

	cases := []struct {
		name   string
		pr     Principal
		member bool
		t      Target
		op     Op
		want   Verdict
	}{
		{"org mismatch beats membership, system, owner and scope", agent, true, elsewhere, OpRead, NoProject},
		{"org mismatch for the system", foreignSystem, false, mine(ScopeProject), OpRead, NoProject},
		{"non-member owner of a project board", agent, false, mine(ScopeProject), OpRead, NoProject},
		{"non-member owner of a personal board", agent, false, mine(ScopePersonal), OpOwn, NoProject},
		{"system reads a personal board", system, false, theirs(ScopePersonal), OpRead, Allow},
		{"system reads a project board", system, true, theirs(ScopeProject), OpRead, Allow},
		{"system writes a personal board", system, false, theirs(ScopePersonal), OpWrite, ReadOnly},
		{"system owns nothing", system, false, theirs(ScopeProject), OpOwn, ReadOnly},
		{"owner reads personal", agent, true, mine(ScopePersonal), OpRead, Allow},
		{"owner writes personal", agent, true, mine(ScopePersonal), OpWrite, Allow},
		{"owner shares personal", agent, true, mine(ScopePersonal), OpOwn, Allow},
		{"owner unshares project", agent, true, mine(ScopeProject), OpOwn, Allow},
		{"member reads project board", agent, true, theirs(ScopeProject), OpRead, Allow},
		{"member writes project board", agent, true, theirs(ScopeProject), OpWrite, Allow},
		{"member shares another's project board", agent, true, theirs(ScopeProject), OpOwn, NotOwner},
		{"member reads another's personal board", agent, true, theirs(ScopePersonal), OpRead, Hidden},
		{"member writes another's personal board", agent, true, theirs(ScopePersonal), OpWrite, Hidden},
		{"member shares another's personal board", agent, true, theirs(ScopePersonal), OpOwn, Hidden},
		{"member writes the project itself", agent, true, Target{OrgID: org, Scope: ScopeProject}, OpWrite, Allow},
		{"system writes the project itself", system, false, Target{OrgID: org, Scope: ScopeProject}, OpWrite, ReadOnly},
	}
	for _, c := range cases {
		if got := Decide(c.pr, c.member, c.t, c.op); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}

	for _, orgOK := range []bool{true, false} {
		for _, member := range []bool{true, false} {
			for _, sys := range []bool{true, false} {
				for _, owner := range []bool{true, false} {
					for _, scope := range []string{ScopePersonal, ScopeProject} {
						for _, op := range []Op{OpRead, OpWrite, OpOwn} {
							pr := agent
							if sys {
								pr = system
							}
							tg := Target{OrgID: org, OwnerID: them, Scope: scope}
							if !orgOK {
								tg.OrgID = other
							}
							if owner {
								tg.OwnerID = me
							}
							got := Decide(pr, member, tg, op)
							var want Verdict
							switch {
							case !orgOK, !member && !sys:
								want = NoProject
							case sys && op == OpRead:
								want = Allow
							case sys:
								want = ReadOnly
							case owner:
								want = Allow
							case scope == ScopeProject && op == OpOwn:
								want = NotOwner
							case scope == ScopeProject:
								want = Allow
							default:
								want = Hidden
							}
							if got != want {
								t.Errorf("org=%v member=%v system=%v owner=%v scope=%s op=%d: got %d, want %d",
									orgOK, member, sys, owner, scope, op, got, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestPlat_PLAT_C22_RemovedMemberSeesNoProject(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	c, b := w.join("cid"), w.join("bob")
	card := w.must(w.p.CreateCard(ctx, c, "APP", NewCard{Title: "left behind"}))
	var board uuid.UUID
	if err := w.db.Get(&board, `SELECT id FROM board WHERE owner_id = $1`, c.MemberID); err != nil {
		t.Fatal(err)
	}
	w.exec(`DELETE FROM project_member WHERE member_id = $1`, c.MemberID)
	title := "x"

	calls := map[string]func() error{
		"ListBoards":   func() error { _, err := w.p.ListBoards(ctx, c, "APP"); return err },
		"NewBoard":     func() error { _, err := w.p.NewBoard(ctx, c, "APP", "again"); return err },
		"RenameBoard":  func() error { _, err := w.p.RenameBoard(ctx, c, "APP", "cid", "y"); return err },
		"ShareBoard":   func() error { _, err := w.p.ShareBoard(ctx, c, "APP", "cid"); return err },
		"UnshareBoard": func() error { _, err := w.p.UnshareBoard(ctx, c, "APP", "cid"); return err },
		"CreateCard":   func() error { _, err := w.p.CreateCard(ctx, c, "APP", NewCard{Title: "x"}); return err },
		"ShowCard":     func() error { _, err := w.p.ShowCard(ctx, c, "APP", card.Ref); return err },
		"ListCards":    func() error { _, err := w.p.ListCards(ctx, c, "APP", ""); return err },
		"EditCard": func() error {
			_, err := w.p.EditCard(ctx, c, "APP", card.Ref, CardEdit{Title: &title, IfVersion: card.Version})
			return err
		},
		"MoveCard":     func() error { _, err := w.p.MoveCard(ctx, c, "APP", card.Ref, "done"); return err },
		"BlockCard":    func() error { _, err := w.p.BlockCard(ctx, c, "APP", card.Ref, card.Ref); return err },
		"CommentCard":  func() error { _, err := w.p.CommentCard(ctx, c, "APP", card.Ref, "x"); return err },
		"ClaimCard":    func() error { _, err := w.p.ClaimCard(ctx, c, "APP", card.Ref, ClaimOpts{}); return err },
		"ClaimNext":    func() error { _, err := w.p.ClaimNext(ctx, c, "APP", "", 0); return err },
		"RenewClaim":   func() error { _, err := w.p.RenewClaim(ctx, c, "APP", card.Ref, 0); return err },
		"ReleaseClaim": func() error { _, err := w.p.ReleaseClaim(ctx, c, "APP", card.Ref); return err },
	}
	for name, call := range calls {
		e := wantCode(t, call(), "project_not_found")
		if e.Kind != KindNotFound {
			t.Fatalf("%s: kind = %s", name, e.Kind)
		}
	}

	if !hasBoard(w.must(w.p.ListBoards(ctx, w.system(), "APP")), board) {
		t.Fatal("the system does not list the removed member's board")
	}
	if hasBoard(w.must(w.p.ListBoards(ctx, b, "APP")), board) {
		t.Fatal("bob lists the removed member's board")
	}
}

func TestPlat_PLAT_C23_SystemReadsEveryPersonalBoard(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	ca := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "ann's"}))
	cb := w.must(w.p.CreateCard(ctx, b, "APP", NewCard{Title: "bob's"}))
	sys := w.system()

	boards := w.must(w.p.ListBoards(ctx, sys, "APP"))
	if !hasBoard(boards, ca.BoardID) || !hasBoard(boards, cb.BoardID) {
		t.Fatalf("the system lists %+v", boards)
	}
	if !hasCard(w.must(w.p.ListCards(ctx, sys, "APP", ca.BoardID.String())), ca.ID) {
		t.Fatal("the system does not list ann's card")
	}
	if !hasCard(w.must(w.p.ListCards(ctx, sys, "APP", "bob")), cb.ID) {
		t.Fatal("the system does not list bob's card")
	}
	if got := w.must(w.p.ShowCard(ctx, sys, "APP", ca.Ref)); got.Title != "ann's" {
		t.Fatalf("the system reads %+v", got)
	}
}

func TestPlat_PLAT_C24_SystemWritesNothing(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	card := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "ann's"}))
	other := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "other"}))
	sys := w.system()
	title := "x"
	tables := []string{"board", "board_column", "card", "comment", "blocker", "event"}
	before := map[string]int{}
	for _, tb := range tables {
		before[tb] = w.count(tb)
	}

	calls := map[string]func() error{
		"NewBoard":     func() error { _, err := w.p.NewBoard(ctx, sys, "APP", "sys"); return err },
		"RenameBoard":  func() error { _, err := w.p.RenameBoard(ctx, sys, "APP", "ann", "y"); return err },
		"ShareBoard":   func() error { _, err := w.p.ShareBoard(ctx, sys, "APP", "ann"); return err },
		"UnshareBoard": func() error { _, err := w.p.UnshareBoard(ctx, sys, "APP", "ann"); return err },
		"CreateCard": func() error {
			_, err := w.p.CreateCard(ctx, sys, "APP", NewCard{Board: "ann", Title: "x"})
			return err
		},
		"EditCard": func() error {
			_, err := w.p.EditCard(ctx, sys, "APP", card.Ref, CardEdit{Title: &title, IfVersion: card.Version})
			return err
		},
		"MoveCard":     func() error { _, err := w.p.MoveCard(ctx, sys, "APP", card.Ref, "done"); return err },
		"BlockCard":    func() error { _, err := w.p.BlockCard(ctx, sys, "APP", card.Ref, other.Ref); return err },
		"CommentCard":  func() error { _, err := w.p.CommentCard(ctx, sys, "APP", card.Ref, "x"); return err },
		"ClaimCard":    func() error { _, err := w.p.ClaimCard(ctx, sys, "APP", card.Ref, ClaimOpts{}); return err },
		"ClaimNext":    func() error { _, err := w.p.ClaimNext(ctx, sys, "APP", "ann", 0); return err },
		"RenewClaim":   func() error { _, err := w.p.RenewClaim(ctx, sys, "APP", card.Ref, 0); return err },
		"ReleaseClaim": func() error { _, err := w.p.ReleaseClaim(ctx, sys, "APP", card.Ref); return err },
	}
	for name, call := range calls {
		e := wantCode(t, call(), "read_only")
		if e.Kind != KindPolicy {
			t.Fatalf("%s: kind = %s", name, e.Kind)
		}
	}
	for _, tb := range tables {
		if n := w.count(tb); n != before[tb] {
			t.Fatalf("%s rows %d -> %d", tb, before[tb], n)
		}
	}
	got := w.must(w.p.ShowCard(ctx, a, "APP", card.Ref))
	if got.Version != card.Version || got.ClaimedBy != nil {
		t.Fatalf("card changed: %+v", got)
	}

	_, err := w.db.Exec(`INSERT INTO token (member_id, name, kind, hash) VALUES ($1, 'sys', 'system', $2)`,
		a.MemberID, make([]byte, 32))
	if err == nil {
		t.Fatal("the store accepts a token of kind system")
	}
}
