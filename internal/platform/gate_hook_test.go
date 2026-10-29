package platform

import (
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestVaultGate_EntryWriteHook(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	shared := w.gateSeedEntry(a, gateSeed{slug: "shared", body: "Shared text.", scope: "project"})

	run := func(before *gateEntry, after gateEntry) error {
		return w.p.Tx(ctx, func(tx *sqlx.Tx) error {
			proj, err := enterProject(tx, a, "APP", OpRead)
			if err != nil {
				return err
			}
			return gateEntryWrite(tx, a, proj, before, after)
		})
	}

	wantCode(t, run(nil, gateEntry{Slug: "new", Scope: "project", Body: "x"}), "publish_required")
	wantCode(t, run(&gateEntry{Scope: "personal"}, gateEntry{Slug: "moved", Scope: "project", Body: "y"}), "publish_required")
	wantCode(t, run(&gateEntry{Scope: "project"}, gateEntry{Slug: "up", Scope: "global", Body: "z"}), "promote_required")
	if err := run(nil, gateEntry{Slug: "mine", Scope: "personal", Body: awsKeyBody}); err != nil {
		t.Fatalf("personal write ran the gate: %v", err)
	}
	before := &gateEntry{ID: shared, Slug: "shared", Scope: "project", Body: "Shared text."}
	if err := run(before, gateEntry{ID: shared, Slug: "shared", Scope: "project", Body: "Shared text, clearer."}); err != nil {
		t.Fatalf("clean edit: %v", err)
	}
	checks := gateChecks(t, run(before, gateEntry{ID: shared, Slug: "shared", Scope: "project", Body: awsKeyBody}))
	if len(checks) != 1 || checks[0].Check != "secret" {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestVaultGate_RecallablePredicate(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	w.gateSeedEntry(a, gateSeed{slug: "fresh", body: "Fresh.", scope: "project"})
	w.gateSeedEntry(a, gateSeed{slug: "hidden", body: "Hidden.", scope: "project", private: true})
	overdue := w.gateSeedEntry(a, gateSeed{slug: "old", body: "Old.", scope: "global"})
	w.exec(`UPDATE entry SET verify_by = now() - interval '1 day' WHERE id = $1`, overdue)

	recallable := func() []string {
		var slugs []string
		if err := w.db.Select(&slugs, `SELECT e.slug FROM entry e WHERE `+entryRecallable+` ORDER BY e.slug`); err != nil {
			t.Fatal(err)
		}
		return slugs
	}
	if got := recallable(); len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("recallable = %v", got)
	}
	w.must(w.p.Verify(ctx, a, "APP", "old"))
	if got := recallable(); len(got) != 2 || got[1] != "old" {
		t.Fatalf("recallable after verify = %v", got)
	}
}
