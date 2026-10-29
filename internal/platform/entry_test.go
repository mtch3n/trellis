package platform

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"uuid"
)

func (w *world) memberIn(org, project uuid.UUID, handle string) Principal {
	w.t.Helper()
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO member (org_id, handle) VALUES ($1, $2) RETURNING id`, org, handle); err != nil {
		w.t.Fatal(err)
	}
	w.exec(`INSERT INTO project_member (org_id, project_id, member_id) VALUES ($1, $2, $3)`, org, project, id)
	return MemberAgent(org, id, handle, "laptop", "")
}

func (w *world) scope(e Entry, scope string) {
	w.t.Helper()
	verifyBy := "NULL"
	if scope == ScopeGlobal {
		verifyBy = "now() + interval '180 days'"
	}
	w.exec(`UPDATE entry SET scope = $2, verify_by = `+verifyBy+` WHERE id = $1`, e.ID, scope)
}

func (w *world) entry(pr Principal, slug, body string) Entry {
	w.t.Helper()
	return w.must(w.p.CreateEntry(w.t.Context(), pr, "APP", NewEntry{Slug: slug, Title: slug, Body: body}))
}

func hasEntry(entries []Entry, id uuid.UUID) bool {
	return slices.ContainsFunc(entries, func(e Entry) bool { return e.ID == id })
}

func linkTo(t *testing.T, e Entry, target string) EntryLink {
	t.Helper()
	for _, l := range e.Links {
		if l.Target == target {
			return l
		}
	}
	t.Fatalf("%s has no link to %s: %+v", e.Slug, target, e.Links)
	return EntryLink{}
}

func TestVault_VAULT_C1_APersonalEntryIsHiddenFromOtherMembers(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	x := w.entry(a, "notes", "ann's notes")

	got := w.must(w.p.ShowEntry(ctx, a, "APP", "notes"))
	if got.ID != x.ID || got.Body != "ann's notes" {
		t.Fatalf("ann reads %+v", got)
	}
	_, hidden := w.p.ShowEntry(ctx, b, "APP", "notes")
	_, missing := w.p.ShowEntry(ctx, b, "APP", "no-such-entry")
	he, me := wantCode(t, hidden, "entry_not_found"), wantCode(t, missing, "entry_not_found")
	if he.Kind != KindNotFound || me.Kind != KindNotFound || strings.ReplaceAll(he.Msg, "notes", "") != strings.ReplaceAll(me.Msg, "no-such-entry", "") {
		t.Fatalf("hidden %+v differs from missing %+v", he, me)
	}
	if hasEntry(w.must(w.p.ListEntries(ctx, b, "APP")), x.ID) {
		t.Fatal("bob lists ann's personal entry")
	}
}

func TestVault_VAULT_C2_AProjectEntryIsSeenByItsProjectOnly(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	ops := w.newProject(w.org, "OPS")
	c := w.memberIn(w.org, ops, "cat")
	x := w.entry(a, "runbook", "restart the queue")
	w.scope(x, ScopeProject)

	for _, pr := range []Principal{a, b} {
		got := w.must(w.p.ShowEntry(ctx, pr, "APP", "runbook"))
		if got.ID != x.ID {
			t.Fatalf("%s reads %+v", pr.Handle, got)
		}
	}
	_, err := w.p.ShowEntry(ctx, c, "APP", "runbook")
	wantCode(t, err, "project_not_found")
	_, err = w.p.ShowEntry(ctx, c, "OPS", "runbook")
	wantCode(t, err, "entry_not_found")
}

func TestVault_VAULT_C3_AGlobalEntryIsSeenByEveryOrgMember(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	ops := w.newProject(w.org, "OPS")
	c := w.memberIn(w.org, ops, "cat")
	other := w.newOrg("globex")
	otherApp := w.newProject(other, "APP")
	d := w.memberIn(other, otherApp, "dan")
	x := w.entry(a, "style", "use tabs")
	w.scope(x, ScopeGlobal)

	for _, from := range []struct {
		pr      Principal
		project string
	}{{a, "APP"}, {c, "OPS"}} {
		got := w.must(w.p.ShowEntry(ctx, from.pr, from.project, "style"))
		if got.ID != x.ID {
			t.Fatalf("%s in %s reads %+v", from.pr.Handle, from.project, got)
		}
	}
	_, err := w.p.ShowEntry(ctx, d, "APP", "style")
	wantCode(t, err, "entry_not_found")
}

func TestVault_VAULT_C4_TheSystemReadsAPersonalEntryAndChangesNothing(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	x := w.entry(a, "notes", "v1")
	sys := w.system()

	if got := w.must(w.p.ShowEntry(ctx, sys, "APP", "notes")); got.ID != x.ID {
		t.Fatalf("system reads %+v", got)
	}
	_, err := w.p.EditEntry(ctx, sys, "APP", "notes", EntryEdit{Body: new("v2"), IfVersion: 1})
	wantCode(t, err, "read_only")
	_, err = w.p.CreateEntry(ctx, sys, "APP", NewEntry{Title: "sys"})
	wantCode(t, err, "read_only")
	wantCode(t, w.p.DeleteEntry(ctx, sys, "APP", "notes"), "read_only")
	got := w.must(w.p.ShowEntry(ctx, a, "APP", "notes"))
	if got.Body != "v1" || got.Version != 1 || w.count("entry") != 1 {
		t.Fatalf("system changed the entry: %+v", got)
	}
}

func TestVault_VAULT_C5_AnEditWithTheVersionKeepsARevision(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	w.entry(a, "notes", "v1")
	w.must(w.p.EditEntry(ctx, a, "APP", "notes", EntryEdit{Body: new("v2"), IfVersion: 1}))

	got := w.must(w.p.EditEntry(ctx, a, "APP", "notes", EntryEdit{Body: new("v3"), IfVersion: 2}))
	if got.Version != 3 || got.Body != "v3" {
		t.Fatalf("after edit: %+v", got)
	}
	history := w.must(w.p.EntryHistory(ctx, a, "APP", "notes"))
	if len(history) != 3 {
		t.Fatalf("history has %d revisions", len(history))
	}
	for i, want := range []string{"v3", "v2", "v1"} {
		if history[i].Version != int64(3-i) || history[i].Body != want {
			t.Fatalf("revision %d = %+v", i, history[i])
		}
	}
}

func TestVault_VAULT_C6_TwoEditsAtTheSameVersionConflict(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	x := w.entry(a, "runbook", "v1")
	w.scope(x, ScopeProject)
	w.must(w.p.EditEntry(ctx, a, "APP", "runbook", EntryEdit{Body: new("v2"), IfVersion: 1}))

	for version := int64(2); version < 12; version++ {
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for i, pr := range []Principal{a, b} {
			wg.Go(func() {
				<-start
				_, errs[i] = w.p.EditEntry(ctx, pr, "APP", "runbook", EntryEdit{Body: new("by " + pr.Handle), IfVersion: version})
			})
		}
		close(start)
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
				continue
			}
			if e := wantCode(t, err, "conflict"); e.Kind != KindConflict {
				t.Fatalf("kind %s", e.Kind)
			}
		}
		if ok != 1 {
			t.Fatalf("at version %d, %d edits succeeded: %v", version, ok, errs)
		}
		if got := w.must(w.p.ShowEntry(ctx, a, "APP", "runbook")); got.Version != version+1 {
			t.Fatalf("version %d, want %d", got.Version, version+1)
		}
	}
}

func TestVault_VAULT_C7_APersonalSlugIsUniquePerOwner(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	first := w.entry(a, "x", "ann's x")

	w.entry(b, "x", "bob's x")
	_, err := w.p.CreateEntry(ctx, a, "APP", NewEntry{Slug: "x", Title: "x again"})
	e := wantCode(t, err, "entry_exists")
	if e.Kind != KindConflict || !strings.Contains(e.Msg, "x") || !strings.Contains(e.Fix, "x") {
		t.Fatalf("conflict does not name the first: %+v", e)
	}
	if d, _ := e.Detail.(map[string]any); d["id"] != first.ID.String() {
		t.Fatalf("detail %+v does not name %s", e.Detail, first.ID)
	}
	if w.count("entry") != 2 {
		t.Fatalf("%d entries", w.count("entry"))
	}
}

func TestVault_VAULT_C8_HistoryKeepsTheLatestHundredRevisions(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	w.entry(a, "notes", "v1")
	for v := int64(1); v < 100; v++ {
		w.must(w.p.EditEntry(ctx, a, "APP", "notes", EntryEdit{Body: new("next"), IfVersion: v}))
	}
	if n := len(w.must(w.p.EntryHistory(ctx, a, "APP", "notes"))); n != 100 {
		t.Fatalf("%d revisions before the edit", n)
	}

	w.must(w.p.EditEntry(ctx, a, "APP", "notes", EntryEdit{Body: new("v101"), IfVersion: 100}))
	history := w.must(w.p.EntryHistory(ctx, a, "APP", "notes"))
	if len(history) != 100 || history[0].Version != 101 || history[99].Version != 2 {
		t.Fatalf("%d revisions, newest %d, oldest %d", len(history), history[0].Version, history[len(history)-1].Version)
	}
}

func TestVault_VAULT_C9_AWikilinkResolvesToTheNearestScope(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	mine := w.entry(a, "x", "ann's x")
	shared := w.entry(b, "x", "the project x")
	w.scope(shared, ScopeProject)

	byAnn := w.must(w.p.CreateEntry(ctx, a, "APP", NewEntry{Title: "ann links", Body: "see [[x]]"}))
	byBob := w.must(w.p.CreateEntry(ctx, b, "APP", NewEntry{Title: "bob links", Body: "see [[x]]"}))
	if l := linkTo(t, byAnn, "x"); l.Stub || l.EntryID == nil || *l.EntryID != mine.ID {
		t.Fatalf("ann's link = %+v, want %s", l, mine.ID)
	}
	if l := linkTo(t, byBob, "x"); l.Stub || l.EntryID == nil || *l.EntryID != shared.ID {
		t.Fatalf("bob's link = %+v, want %s", l, shared.ID)
	}
}

func TestVault_VAULT_C10_CreatingTheTargetFillsTheStub(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	from := w.entry(a, "from", "see [[y]]")
	if l := linkTo(t, from, "y"); !l.Stub || l.EntryID != nil {
		t.Fatalf("link before y = %+v", l)
	}

	y := w.entry(a, "y", "now y exists")
	got := w.must(w.p.ShowEntry(ctx, a, "APP", "from"))
	if l := linkTo(t, got, "y"); l.Stub || l.EntryID == nil || *l.EntryID != y.ID {
		t.Fatalf("link after y = %+v, want %s", l, y.ID)
	}
}

func TestVault_VAULT_C11_ALinkToAHiddenEntryIsAStub(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	z := w.entry(a, "z", "ann's z")

	byBob := w.must(w.p.CreateEntry(ctx, b, "APP", NewEntry{Title: "bob links", Body: "see [[z]]"}))
	if l := linkTo(t, byBob, "z"); !l.Stub || l.EntryID != nil || l.Owner != "" || l.Scope != "" {
		t.Fatalf("bob's link = %+v", l)
	}
	got := w.must(w.p.ShowEntry(ctx, b, "APP", byBob.Slug))
	if l := linkTo(t, got, "z"); !l.Stub || l.EntryID != nil {
		t.Fatalf("bob's link on show = %+v", l)
	}
	for _, read := range [][]Entry{{got}, w.must(w.p.ListEntries(ctx, b, "APP"))} {
		for _, e := range read {
			if e.ID == z.ID || strings.Contains(e.Owner, "ann") || slices.ContainsFunc(e.Links, func(l EntryLink) bool {
				return l.EntryID != nil && *l.EntryID == z.ID
			}) {
				t.Fatalf("bob reads ann's z through %+v", e)
			}
		}
	}
}

func TestVault_VAULT_C12_ANewProjectHasTheBuiltInTemplates(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")

	var names []string
	for _, tmpl := range w.must(w.p.ListTemplates(ctx, a, "APP")) {
		names = append(names, tmpl.Name)
	}
	want := []string{"decision", "finding", "glossary", "reference", "research", "runbook"}
	if !slices.Equal(names, want) {
		t.Fatalf("templates %v, want %v", names, want)
	}
	e := w.must(w.p.CreateEntry(ctx, a, "APP", NewEntry{Title: "Use Postgres", Template: "decision",
		Sources: []string{"https://example.com/adr"}, Set: map[string]string{"status": "accepted"}}))
	for _, section := range []string{"# Use Postgres", "## Context", "## Options considered", "## Decision", "## Consequences"} {
		if !strings.Contains(e.Body, section) {
			t.Fatalf("body has no %q:\n%s", section, e.Body)
		}
	}
	if e.Template != "decision" || e.Fields["status"] != "accepted" || !slices.Equal(e.Sources, []string{"https://example.com/adr"}) {
		t.Fatalf("entry %+v", e)
	}
	_, err := w.p.CreateEntry(ctx, a, "APP", NewEntry{Title: "No sources", Template: "decision"})
	if e := wantCode(t, err, "template_violation"); !strings.Contains(e.Error(), "sources") {
		t.Fatalf("violation %+v does not name sources", e)
	}
}

func TestVault_VAULT_C13_AnUnknownTemplateIsAUsageError(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")

	_, err := w.p.CreateEntry(ctx, a, "APP", NewEntry{Title: "x", Template: "no-such-template"})
	if e := wantCode(t, err, "unknown_template"); e.Kind != KindUsage {
		t.Fatalf("kind %s", e.Kind)
	}
	if w.count("entry") != 0 || w.count("entry_revision") != 0 || w.count("event") != 0 {
		t.Fatal("an unknown template wrote rows")
	}
}

func TestVault_VAULT_C15_RestoreOverANewEntryConflicts(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	w.entry(a, "x", "old x")
	if err := w.p.DeleteEntry(ctx, a, "APP", "x"); err != nil {
		t.Fatal(err)
	}
	fresh := w.entry(a, "x", "new x")

	_, err := w.p.RestoreEntry(ctx, a, "APP", "x")
	e := wantCode(t, err, "entry_exists")
	if e.Kind != KindConflict {
		t.Fatalf("kind %s", e.Kind)
	}
	if d, _ := e.Detail.(map[string]any); d["id"] != fresh.ID.String() {
		t.Fatalf("detail %+v does not name %s", e.Detail, fresh.ID)
	}
	if got := w.must(w.p.ShowEntry(ctx, a, "APP", "x")); got.ID != fresh.ID {
		t.Fatalf("show after the failed restore %+v", got)
	}
}
