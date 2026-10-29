package platform

import (
	"slices"
	"sync"
	"testing"
)

func TestPlat_PLAT_C25_ClaimCommentAndMoveToDone(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	card := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "ship it"}))

	claimed := w.must(w.p.ClaimCard(ctx, a, "APP", card.Ref, ClaimOpts{}))
	if claimed.ClaimedBy == nil || *claimed.ClaimedBy != a.Actor() {
		t.Fatalf("claimant = %v", claimed.ClaimedBy)
	}
	w.must(w.p.CommentCard(ctx, a, "APP", card.Ref, "done and tested"))
	w.must(w.p.MoveCard(ctx, a, "APP", card.Ref, "done"))

	got := w.must(w.p.ShowCard(ctx, a, "APP", card.Ref))
	if got.Column != "done" || !got.Done {
		t.Fatalf("column = %q done=%v", got.Column, got.Done)
	}
	if got.ClaimedBy != nil || got.ClaimUntil != nil {
		t.Fatalf("card still claimed by %v", *got.ClaimedBy)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != "done and tested" || got.Comments[0].Actor != a.Actor() {
		t.Fatalf("comments = %+v", got.Comments)
	}
}

func TestPlat_PLAT_C26_ExpiredClaimIsTakenWithoutSteal(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	s1 := MemberAgent(a.OrgID, a.MemberID, a.Handle, a.TokenName, "s1")
	s2 := MemberAgent(a.OrgID, a.MemberID, a.Handle, a.TokenName, "s2")
	card := w.must(w.p.CreateCard(ctx, s1, "APP", NewCard{Title: "work"}))
	w.must(w.p.ClaimCard(ctx, s1, "APP", card.Ref, ClaimOpts{}))
	_, err := w.p.ClaimCard(ctx, s2, "APP", card.Ref, ClaimOpts{})
	wantCode(t, err, "contention")

	w.exec(`UPDATE card SET claim_until = now() - interval '1 minute' WHERE id = $1`, card.ID)
	got := w.must(w.p.ClaimCard(ctx, s2, "APP", card.Ref, ClaimOpts{}))
	if got.ClaimedBy == nil || *got.ClaimedBy != s2.Actor() {
		t.Fatalf("claimant = %v, want %s", got.ClaimedBy, s2.Actor())
	}
}

func TestPlat_PLAT_C27_ConcurrentVersionedEditsOneWins(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	w.must(w.p.NewBoard(ctx, a, "APP", "team"))
	w.must(w.p.ShareBoard(ctx, a, "APP", "team"))

	for round := range 10 {
		card := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Board: "team", Title: "v1"}))
		for v := card.Version; v < 4; v++ {
			title := "step"
			card = w.must(w.p.EditCard(ctx, a, "APP", card.Ref, CardEdit{Title: &title, IfVersion: v}))
		}
		if card.Version != 4 {
			t.Fatalf("setup version = %d", card.Version)
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]Card, 2)
		errs := make([]error, 2)
		for i, pr := range []Principal{a, b} {
			wg.Go(func() {
				title := pr.Handle
				<-start
				results[i], errs[i] = w.p.EditCard(ctx, pr, "APP", card.Ref, CardEdit{Title: &title, IfVersion: 4})
			})
		}
		close(start)
		wg.Wait()

		wins := 0
		for i := range 2 {
			if errs[i] == nil {
				wins++
				if results[i].Version != 5 {
					t.Fatalf("round %d: winner version = %d", round, results[i].Version)
				}
				continue
			}
			wantCode(t, errs[i], "conflict")
		}
		if wins != 1 {
			t.Fatalf("round %d: %d edits won", round, wins)
		}
	}
}

func TestPlat_PLAT_C28_ConcurrentCreatesGetDistinctRefs(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	for range 7 {
		w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "earlier"}))
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	refs := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			<-start
			var c Card
			c, errs[i] = w.p.CreateCard(ctx, a, "APP", NewCard{Title: "at once"})
			refs[i] = c.Ref
		})
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(refs)
	if !slices.Equal(refs, []string{"APP-8", "APP-9"}) {
		t.Fatalf("refs = %v", refs)
	}
}
