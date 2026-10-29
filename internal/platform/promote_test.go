package platform

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func (w *world) makeMaintainer(pr Principal) {
	w.exec(`UPDATE project_member SET role = 'maintainer' WHERE member_id = $1`, pr.MemberID)
}

func (w *world) makeAdmin(pr Principal) {
	w.exec(`UPDATE member SET role = 'admin' WHERE id = $1`, pr.MemberID)
}

func TestVault_VAULT_C40_MaintainerDemotesToOwnersPersonalScope(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b, m := w.join("ann"), w.join("bob"), w.join("max")
	w.makeMaintainer(m)
	id := w.gateSeedEntry(a, gateSeed{slug: "old-way", body: "Do it the old way.", scope: "project"})
	w.exec(`UPDATE entry SET labels = '{gate}' WHERE id = $1`, id)

	got := w.must(w.p.Demote(ctx, m, "APP", "old-way"))
	if got.Scope != "personal" {
		t.Fatalf("demoted = %+v", got)
	}
	row := w.entryRow(id)
	if row.Scope != "personal" || row.OwnerID != a.MemberID {
		t.Fatalf("row = %+v", row)
	}
	err := w.p.Nominate(ctx, b, "APP", "old-way", "still useful")
	wantCode(t, err, "entry_not_found")
}

func TestVault_VAULT_C41_NonMaintainerCannotDemote(t *testing.T) {
	w := newWorld(t)
	a, b := w.join("ann"), w.join("bob")
	id := w.gateSeedEntry(a, gateSeed{slug: "keep-me", body: "Keep this.", scope: "project"})
	before := w.entryRow(id)

	_, err := w.p.Demote(t.Context(), b, "APP", "keep-me")
	wantCode(t, err, "not_maintainer")
	if w.entryRow(id) != before {
		t.Fatal("entry changed")
	}
}

func TestVault_VAULT_C44_OwnerPromotesToGlobal(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	c := w.joinProject("cat", "WEB")
	id := w.gateSeedEntry(a, gateSeed{slug: "tz-rule", body: "Store times in UTC.", scope: "project"})

	got := w.must(w.p.Promote(ctx, a, "APP", "tz-rule", "every service stores times"))
	if got.Scope != "global" || got.VerifyBy == nil {
		t.Fatalf("promoted = %+v", got)
	}
	ahead := time.Until(*got.VerifyBy)
	if ahead < 179*24*time.Hour || ahead > 181*24*time.Hour {
		t.Fatalf("verify_by is %v ahead", ahead)
	}
	var inProject int
	if err := w.db.Get(&inProject, `SELECT count(*) FROM entry WHERE project_id = $1 AND scope = 'project'`, w.project); err != nil {
		t.Fatal(err)
	}
	if inProject != 0 || w.entryRow(id).Scope != "global" {
		t.Fatalf("entry still in the project")
	}
	var reason, by string
	if err := w.db.Get(&reason, `SELECT promote_reason FROM entry WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := w.db.Get(&by, `SELECT promoted_by FROM entry WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if reason != "every service stores times" || by != a.Actor() {
		t.Fatalf("reason %q by %q", reason, by)
	}
	seen := w.must(w.p.Verify(ctx, c, "WEB", "tz-rule"))
	if seen.Slug != "tz-rule" {
		t.Fatalf("other project sees %+v", seen)
	}
}

func TestVault_VAULT_C45_NonOwnerCannotPromote(t *testing.T) {
	w := newWorld(t)
	a, b := w.join("ann"), w.join("bob")
	id := w.gateSeedEntry(a, gateSeed{slug: "anns", body: "Ann's rule.", scope: "project"})
	before := w.entryRow(id)

	_, err := w.p.Promote(t.Context(), b, "APP", "anns", "I like it")
	wantCode(t, err, "not_owner")
	if w.entryRow(id) != before {
		t.Fatal("entry changed")
	}
}

func TestVault_VAULT_C46_PersonalOrReasonlessPromoteRefused(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	personal := w.gateSeedEntry(a, gateSeed{slug: "draft", body: "Not shared yet."})
	project := w.gateSeedEntry(a, gateSeed{slug: "shared", body: "Shared.", scope: "project"})
	beforePersonal, beforeProject := w.entryRow(personal), w.entryRow(project)

	_, err := w.p.Promote(ctx, a, "APP", "draft", "good")
	e := wantCode(t, err, "not_published")
	if e.Kind != KindPolicy || !strings.Contains(e.Fix, "publish") {
		t.Fatalf("error = %+v", e)
	}
	_, err = w.p.Promote(ctx, a, "APP", "shared", "  ")
	e = wantCode(t, err, "reason_required")
	if e.Kind != KindUsage {
		t.Fatalf("error = %+v", e)
	}
	if w.entryRow(personal) != beforePersonal || w.entryRow(project) != beforeProject {
		t.Fatal("entry changed")
	}
}

func TestVault_VAULT_C47_AdminDemotesGlobalBackToItsProject(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b, z := w.join("ann"), w.join("bob"), w.join("zed")
	w.makeAdmin(z)
	id := w.gateSeedEntry(a, gateSeed{slug: "wide", body: "Wide rule.", scope: "project"})
	w.must(w.p.Promote(ctx, a, "APP", "wide", "applies to all"))
	before := w.entryRow(id)

	_, err := w.p.Demote(ctx, b, "APP", "wide")
	wantCode(t, err, "not_admin")
	if w.entryRow(id) != before {
		t.Fatal("entry changed")
	}

	got := w.must(w.p.Demote(ctx, z, "APP", "wide"))
	if got.Scope != "project" {
		t.Fatalf("demoted = %+v", got)
	}
	row := w.entryRow(id)
	if row.Scope != "project" || row.Project != "APP" || row.VerifyBy != "" {
		t.Fatalf("row = %+v", row)
	}
}

func TestVault_VAULT_C48_ConcurrentPromoteOnceThenConflict(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	s1 := MemberAgent(a.OrgID, a.MemberID, a.Handle, a.TokenName, "s1")
	s2 := MemberAgent(a.OrgID, a.MemberID, a.Handle, a.TokenName, "s2")
	for round := range 5 {
		slug := "both-" + string(rune('a'+round))
		w.gateSeedEntry(a, gateSeed{slug: slug, body: "text " + slug, scope: "project"})

		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, pr := range []Principal{s1, s2} {
			wg.Go(func() {
				_, errs[i] = w.p.Promote(ctx, pr, "APP", slug, "both sessions agree")
			})
		}
		wg.Wait()

		ok, conflicts := 0, 0
		for _, err := range errs {
			e, isErr := errors.AsType[*Error](err)
			switch {
			case err == nil:
				ok++
			case isErr && e.Kind == KindConflict:
				conflicts++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if ok != 1 || conflicts != 1 {
			t.Fatalf("round %d: errs = %v", round, errs)
		}
		var globals int
		if err := w.db.Get(&globals, `SELECT count(*) FROM entry WHERE slug = $1 AND scope = 'global'`, slug); err != nil {
			t.Fatal(err)
		}
		if globals != 1 {
			t.Fatalf("round %d: %d global entries", round, globals)
		}
	}
}
