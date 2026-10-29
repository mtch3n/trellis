package platform

import (
	"slices"
	"testing"
)

func (w *world) coordCard(pr Principal, title, body string) Card {
	w.t.Helper()
	return w.must(w.p.CreateCard(w.t.Context(), pr, "APP", NewCard{Title: title, Body: body}))
}

func (w *world) coordRun() {
	w.t.Helper()
	if _, err := w.p.Coordinate(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) coordNotices(pr Principal) []Notice {
	w.t.Helper()
	return w.must(w.p.Notices(w.t.Context(), pr, "APP"))
}

func TestCoord_COORD_C1_OverlappingPersonalCardsNoticeBothOwners(t *testing.T) {
	w := newWorld(t)
	ann, bob := w.join("ann"), w.join("bob")
	annCard := w.coordCard(ann, "Rotate the postgres backup credentials", "")
	bobCard := w.coordCard(bob, "postgres backup credentials rotation script", "")

	w.coordRun()

	annNotices, bobNotices := w.coordNotices(ann), w.coordNotices(bob)
	if len(annNotices) != 1 || len(bobNotices) != 1 {
		t.Fatalf("ann %+v, bob %+v", annNotices, bobNotices)
	}
	a, b := annNotices[0], bobNotices[0]
	if a.Card != annCard.Ref || a.Title != annCard.Title || a.With != "bob" {
		t.Errorf("ann notice = %+v", a)
	}
	if b.Card != bobCard.Ref || b.Title != bobCard.Title || b.With != "ann" {
		t.Errorf("bob notice = %+v", b)
	}
	if want := []string{"backup", "credentials", "postgres", "rotate"}; !slices.Equal(a.Words, want) {
		t.Errorf("ann words = %v, want %v", a.Words, want)
	}
	if want := []string{"backup", "credentials", "postgres", "rotation"}; !slices.Equal(b.Words, want) {
		t.Errorf("bob words = %v, want %v", b.Words, want)
	}
}
