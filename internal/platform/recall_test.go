package platform

import (
	"slices"
	"testing"
	"time"
)

func TestVault_VAULT_C25_RecallLeavesOutPrivateAndOverdue(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	b := w.join("bob")
	w.seedEntry(entrySpec{owner: b, scope: ScopeProject, slug: "fresh", body: "kubernetes ingress timeouts explained"})
	w.seedEntry(entrySpec{owner: a, slug: "secret", body: "kubernetes ingress credentials", private: true})
	w.seedEntry(entrySpec{owner: b, scope: "global", slug: "stale", body: "kubernetes ingress defaults", verify: new(time.Now().Add(-time.Hour))})
	events := w.count("event")

	hits := w.must(w.p.Recall(t.Context(), a, "APP", "why does the kubernetes ingress time out?", RecallOpts{}))
	got := make([]string, 0, len(hits))
	for _, h := range hits {
		got = append(got, h.Slug)
	}
	if !slices.Equal(got, []string{"fresh"}) {
		t.Fatalf("recall = %v, want [fresh]", got)
	}
	if hits[0].Line == "" {
		t.Fatal("hit has no line")
	}
	if n := w.count("event"); n != events {
		t.Fatalf("events %d -> %d, want no injection recorded", events, n)
	}
}

func TestVault_VAULT_C26_EmptyPromptRecallsNothing(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	w.seedEntry(entrySpec{owner: a, scope: ScopeProject, slug: "any", body: "the and of"})
	for _, text := range []string{"", "   ", "the and of"} {
		hits, err := w.p.Recall(t.Context(), a, "APP", text, RecallOpts{})
		if err != nil {
			t.Fatalf("recall %q: %v", text, err)
		}
		if hits == nil || len(hits) != 0 {
			t.Fatalf("recall %q = %v, want an empty list", text, hits)
		}
	}
}
