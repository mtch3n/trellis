package platform

import (
	"slices"
	"testing"

	"github.com/jmoiron/sqlx"
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

func (w *world) coordNoticeCount() int {
	w.t.Helper()
	return w.count("coord_notice")
}

func TestCoord_COORD_C2_TooFewOrTooSmallAShareOfLexemesIsNoOverlap(t *testing.T) {
	t.Run("two shared lexemes", func(t *testing.T) {
		w := newWorld(t)
		ann, bob := w.join("ann"), w.join("bob")
		w.coordCard(ann, "postgres backup plan", "")
		w.coordCard(bob, "postgres backup credentials", "")
		w.coordRun()
		if n := w.coordNoticeCount(); n != 0 {
			t.Fatalf("%d notices, want 0", n)
		}
	})
	t.Run("three shared lexemes under 30 percent", func(t *testing.T) {
		w := newWorld(t)
		ann, bob := w.join("ann"), w.join("bob")
		w.coordCard(ann, "postgres backup credentials", "alpha bravo charlie delta echo foxtrot golf hotel")
		w.coordCard(bob, "postgres backup credentials", "india juliet kilo lima mike november oscar papa quebec")
		w.coordRun()
		if n := w.coordNoticeCount(); n != 0 {
			t.Fatalf("%d notices, want 0", n)
		}
	})
}

func (w *world) coordJoinProject(key string, members ...Principal) {
	w.t.Helper()
	project := w.newProject(w.org, key)
	for _, m := range members {
		w.exec(`INSERT INTO project_member (org_id, project_id, member_id) VALUES ($1, $2, $3)`, w.org, project, m.MemberID)
		err := w.p.Tx(w.t.Context(), func(tx *sqlx.Tx) error {
			_, err := createBoard(tx, project, m.MemberID, m.Handle)
			return err
		})
		if err != nil {
			w.t.Fatal(err)
		}
	}
}

func TestCoord_COORD_C3_SameOwnerDoneColumnOrOtherProjectIsNoOverlap(t *testing.T) {
	const title = "Rotate the postgres backup credentials"
	t.Run("same member", func(t *testing.T) {
		w := newWorld(t)
		ann := w.join("ann")
		w.join("bob")
		w.coordCard(ann, title, "")
		w.coordCard(ann, "postgres backup credentials rotation script", "")
		w.coordRun()
		if n := w.coordNoticeCount(); n != 0 {
			t.Fatalf("%d notices, want 0", n)
		}
	})
	t.Run("done column", func(t *testing.T) {
		w := newWorld(t)
		ann, bob := w.join("ann"), w.join("bob")
		w.must(w.p.CreateCard(t.Context(), ann, "APP", NewCard{Title: title, Column: "done"}))
		w.coordCard(bob, "postgres backup credentials rotation script", "")
		w.coordRun()
		if n := w.coordNoticeCount(); n != 0 {
			t.Fatalf("%d notices, want 0", n)
		}
	})
	t.Run("different projects", func(t *testing.T) {
		w := newWorld(t)
		ann, bob := w.join("ann"), w.join("bob")
		w.coordJoinProject("OPS", ann, bob)
		w.coordCard(ann, title, "")
		w.must(w.p.CreateCard(t.Context(), bob, "OPS", NewCard{Title: "postgres backup credentials rotation script"}))
		w.coordRun()
		if n := w.coordNoticeCount(); n != 0 {
			t.Fatalf("%d notices, want 0", n)
		}
	})
}
