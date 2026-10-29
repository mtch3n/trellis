package platform

import (
	"fmt"
	"strings"
	"testing"
)

func TestVault_VAULT_C42_NominationsRankedByNominationCount(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	w.gateSeedEntry(a, gateSeed{slug: "once", body: "Nominated once.", scope: "project"})
	w.gateSeedEntry(a, gateSeed{slug: "twice", body: "Nominated twice.", scope: "project"})

	w.must(0, w.p.Nominate(ctx, a, "APP", "once", "handy"))
	w.must(0, w.p.Nominate(ctx, a, "APP", "twice", "every repo needs it"))
	w.must(0, w.p.Nominate(ctx, b, "APP", "twice", "I re-derived it twice"))

	got := w.must(w.p.Nominations(ctx, b, "APP"))
	var order []string
	for _, n := range got {
		if len(order) == 0 || order[len(order)-1] != n.Slug {
			order = append(order, n.Slug)
		}
	}
	if fmt.Sprint(order) != "[twice once]" {
		t.Fatalf("order = %v, nominations = %+v", order, got)
	}
	if got[0].Nominations != 2 {
		t.Fatalf("first = %+v", got[0])
	}
}

func TestVault_VAULT_C43_EleventhWaitingEntryRefused(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	for i := range 11 {
		w.gateSeedEntry(a, gateSeed{slug: fmt.Sprintf("entry-%d", i), body: fmt.Sprintf("Body %d.", i), scope: "project"})
	}
	for i := range 10 {
		w.must(0, w.p.Nominate(ctx, a, "APP", fmt.Sprintf("entry-%d", i), "worth it"))
	}

	err := w.p.Nominate(ctx, a, "APP", "entry-10", "worth it too")
	e := wantCode(t, err, "nomination_queue_full")
	if e.Kind != KindPolicy || !strings.Contains(e.Fix, "nominations") {
		t.Fatalf("error = %+v", e)
	}
	if n := w.count("nomination"); n != 10 {
		t.Fatalf("%d nominations", n)
	}
}
