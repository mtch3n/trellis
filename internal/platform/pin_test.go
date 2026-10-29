package platform

import (
	"slices"
	"testing"
	"time"
	"uuid"
)

func (w *world) pinEntry(owner Principal, slug, scope string, verifyBy *time.Time) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO entry (org_id, project_id, owner_id, scope, slug, title, summary, body, verify_by, created_by)
		VALUES ($1, $2, $3, $4, $5, $5, 'summary of ' || $5, 'body of ' || $5, $6, $7) RETURNING id`,
		w.org, w.project, owner.MemberID, scope, slug, verifyBy, owner.Actor()); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func briefSlugs(b Brief) []string {
	slugs := []string{}
	for _, p := range b.Pins {
		slugs = append(slugs, p.Slug)
	}
	return slugs
}

func TestVault_VAULT_C55_BriefHoldsThePinnedProjectEntrysRecap(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	w.pinEntry(a, "deploy", "project", nil)

	pin := w.must(w.p.PinEntry(ctx, a, "APP", "deploy", "run make deploy, never by hand"))
	if pin.Recap != "run make deploy, never by hand" {
		t.Fatalf("pin recap = %q", pin.Recap)
	}
	for _, pr := range []Principal{a, b, w.system()} {
		brief := w.must(w.p.Brief(ctx, pr, "APP"))
		i := slices.IndexFunc(brief.Pins, func(p Pin) bool { return p.Slug == "deploy" })
		if i < 0 {
			t.Fatalf("%s: brief has no pin for deploy: %v", pr.Actor(), briefSlugs(brief))
		}
		if brief.Pins[i].Recap != "run make deploy, never by hand" {
			t.Fatalf("%s: brief recap = %q", pr.Actor(), brief.Pins[i].Recap)
		}
	}
}

func TestVault_VAULT_C55_PinWithoutRecapFallsBackToTheSummary(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	w.pinEntry(a, "deploy", "project", nil)

	w.must(w.p.PinEntry(ctx, a, "APP", "deploy", ""))
	brief := w.must(w.p.Brief(ctx, a, "APP"))
	if len(brief.Pins) != 1 || brief.Pins[0].Recap != "summary of deploy" {
		t.Fatalf("brief pins = %+v", brief.Pins)
	}
}

func TestVault_VAULT_C56_PrivateAndOverdueGlobalPinsLeaveTheBrief(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	secret := w.pinEntry(a, "secret", "project", nil)
	later := time.Now().Add(24 * time.Hour)
	old := w.pinEntry(a, "old-rule", "global", &later)
	w.pinEntry(a, "kept", "project", nil)

	w.must(w.p.PinEntry(ctx, a, "APP", "secret", "the recap"))
	w.must(w.p.PinEntry(ctx, a, "APP", "old-rule", "the recap"))
	w.must(w.p.PinEntry(ctx, a, "APP", "kept", "the recap"))
	if got := briefSlugs(w.must(w.p.Brief(ctx, b, "APP"))); len(got) != 3 {
		t.Fatalf("before: brief pins = %v", got)
	}

	w.exec(`UPDATE entry SET private = true WHERE id = $1`, secret)
	w.exec(`UPDATE entry SET verify_by = now() - interval '1 day' WHERE id = $1`, old)
	for _, pr := range []Principal{a, b, w.system()} {
		got := briefSlugs(w.must(w.p.Brief(ctx, pr, "APP")))
		if !slices.Equal(got, []string{"kept"}) {
			t.Fatalf("%s: brief pins = %v", pr.Actor(), got)
		}
	}
}

func TestVault_VAULT_C57_PinningAnotherMembersPersonalEntryIsNotFound(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	id := w.pinEntry(b, "notes", "personal", nil)

	_, err := w.p.PinEntry(ctx, a, "APP", "notes", "")
	if e := wantCode(t, err, "entry_not_found"); e.Kind != KindNotFound {
		t.Fatalf("kind = %s", e.Kind)
	}
	_, err = w.p.PinEntry(ctx, a, "APP", id.String(), "")
	wantCode(t, err, "entry_not_found")
	if n := w.count("pin"); n != 0 {
		t.Fatalf("pins stored = %d", n)
	}
	if got := briefSlugs(w.must(w.p.Brief(ctx, a, "APP"))); len(got) != 0 {
		t.Fatalf("ann's brief pins = %v", got)
	}
}
