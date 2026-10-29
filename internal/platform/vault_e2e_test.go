package platform

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func recallSlugs(t *testing.T, w *world, pr Principal, text string) []string {
	t.Helper()
	hits := w.must(w.p.Recall(t.Context(), pr, "APP", text, RecallOpts{}))
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Slug)
	}
	return out
}

func TestVault_VAULT_C14_DeletedEntryLeavesShowListSearchUntilRestored(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	x := w.entry(a, "runbook", "restart the ingress controller gently")

	w.must(0, w.p.DeleteEntry(ctx, a, "APP", "runbook"))
	_, err := w.p.ShowEntry(ctx, a, "APP", "runbook")
	wantCode(t, err, "entry_not_found")
	if hasEntry(w.must(w.p.ListEntries(ctx, a, "APP")), x.ID) {
		t.Fatal("list returns the deleted entry")
	}
	if hits := w.must(w.p.Search(ctx, a, "APP", "ingress controller", SearchOpts{})); slices.Contains(hitSlugs(hits), "runbook") {
		t.Fatalf("search returns the deleted entry: %v", hitSlugs(hits))
	}

	w.must(w.p.RestoreEntry(ctx, a, "APP", "runbook"))
	got := w.must(w.p.ShowEntry(ctx, a, "APP", "runbook"))
	if got.ID != x.ID || got.Body != "restart the ingress controller gently" {
		t.Fatalf("restored entry = %+v", got)
	}
	if !hasEntry(w.must(w.p.ListEntries(ctx, a, "APP")), x.ID) {
		t.Fatal("list does not return the restored entry")
	}
	if hits := w.must(w.p.Search(ctx, a, "APP", "ingress controller", SearchOpts{})); !slices.Contains(hitSlugs(hits), "runbook") {
		t.Fatalf("search does not return the restored entry: %v", hitSlugs(hits))
	}
}

func TestVault_VAULT_C16_PrivateEntryIsSearchedAndShownButNotRecalled(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	w.must(w.p.CreateEntry(ctx, a, "APP", NewEntry{Slug: "vpn", Title: "vpn", Body: "the wireguard tunnel keys rotate weekly", Private: true}))

	if hits := w.must(w.p.Search(ctx, a, "APP", "wireguard tunnel", SearchOpts{})); !slices.Contains(hitSlugs(hits), "vpn") {
		t.Fatalf("search = %v, want vpn", hitSlugs(hits))
	}
	if got := w.must(w.p.ShowEntry(ctx, a, "APP", "vpn")); !got.Private || got.Body == "" {
		t.Fatalf("show = %+v", got)
	}
	if got := recallSlugs(t, w, a, "how does the wireguard tunnel rotate?"); slices.Contains(got, "vpn") {
		t.Fatalf("recall = %v, want no private entry", got)
	}
}

func TestVault_VAULT_C34_AgentCannotWriteProjectScopeExceptByPublish(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	mine := w.entry(a, "mine", "a personal note")
	entries, revisions := w.count("entry"), w.count("entry_revision")

	_, err := w.p.CreateEntry(ctx, a, "APP", NewEntry{Slug: "shared", Title: "shared", Body: "for everyone", Scope: ScopeProject})
	e := wantCode(t, err, "publish_required")
	if e.Kind != KindUsage || !strings.Contains(e.Fix, "publish") {
		t.Fatalf("create: %+v", e)
	}
	_, err = w.p.EditEntry(ctx, a, "APP", "mine", EntryEdit{Scope: new(ScopeProject), IfVersion: mine.Version})
	e = wantCode(t, err, "publish_required")
	if e.Kind != KindUsage || !strings.Contains(e.Fix, "publish") {
		t.Fatalf("edit: %+v", e)
	}

	if w.count("entry") != entries || w.count("entry_revision") != revisions {
		t.Fatal("a refused write wrote something")
	}
	if r := w.entryRow(mine.ID); r.Scope != ScopePersonal || r.Version != mine.Version {
		t.Fatalf("mine changed: %+v", r)
	}
}

func TestVault_VAULT_C38_CleanEditOfProjectEntryPassesTheGate(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	id := w.gateSeedEntry(a, gateSeed{slug: "deploys", body: "Deploy from main only.", scope: ScopeProject})

	got := w.must(w.p.EditEntry(ctx, b, "APP", "deploys", EntryEdit{Body: new("Deploy from main only, after CI is green."), IfVersion: 1}))
	if got.Version != 2 {
		t.Fatalf("version = %d, want 2", got.Version)
	}
	if r := w.entryRow(id); r.Version != 2 || r.Body != "Deploy from main only, after CI is green." || r.Scope != ScopeProject {
		t.Fatalf("row = %+v", r)
	}
}

func TestVault_VAULT_C39_EditAddingASecretFailsTheGateAndKeepsTheVersion(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	id := w.gateSeedEntry(a, gateSeed{slug: "deploys", body: "Deploy from main only.", scope: ScopeProject})
	w.exec(`UPDATE entry SET version = 3 WHERE id = $1`, id)
	revisions := w.count("entry_revision")

	_, err := w.p.EditEntry(ctx, a, "APP", "deploys", EntryEdit{Body: new(awsKeyBody), IfVersion: 3})
	checks := gateChecks(t, err)
	if !slices.ContainsFunc(checks, func(c GateCheck) bool { return c.Check == "secret" }) {
		t.Fatalf("checks = %+v", checks)
	}
	if r := w.entryRow(id); r.Version != 3 || r.Body != "Deploy from main only." {
		t.Fatalf("row = %+v", r)
	}
	if w.count("entry_revision") != revisions {
		t.Fatal("a refused edit kept a revision")
	}
}

func TestVault_VAULT_C49_OverdueGlobalEntryReturnsToRecallAfterVerify(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	b := w.joinProject("cat", "OPS")
	w.seedEntry(entrySpec{owner: a, scope: ScopeGlobal, slug: "oncall", body: "pager escalation ladder for incidents",
		verify: new(time.Now().Add(-time.Hour))})

	if got := recallSlugs(t, w, a, "what is the pager escalation ladder?"); slices.Contains(got, "oncall") {
		t.Fatalf("recall = %v, want no overdue entry", got)
	}
	w.must(w.p.Verify(ctx, b, "OPS", "oncall"))
	if got := recallSlugs(t, w, a, "what is the pager escalation ladder?"); !slices.Contains(got, "oncall") {
		t.Fatalf("recall after verify = %v, want oncall", got)
	}
}
