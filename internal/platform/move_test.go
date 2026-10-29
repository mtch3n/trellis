package platform

import (
	"slices"
	"strings"
	"testing"
)

func TestVault_MoveRenamesTheSlugAndKeepsARevision(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	w.entry(a, "linker", "see [[after]]")
	x := w.entry(a, "before", "the body")

	_, err := w.p.MoveEntry(ctx, a, "APP", "before", "after", 0)
	wantCode(t, err, "version_required")
	got := w.must(w.p.MoveEntry(ctx, a, "APP", "before", "after", x.Version))
	if got.Slug != "after" || got.Version != x.Version+1 || got.Body != "the body" {
		t.Fatalf("moved = %+v", got)
	}
	_, err = w.p.ShowEntry(ctx, a, "APP", "before")
	wantCode(t, err, "entry_not_found")
	if l := linkTo(t, w.must(w.p.ShowEntry(ctx, a, "APP", "linker")), "after"); l.Stub {
		t.Fatalf("the stub to after was not filled: %+v", l)
	}
	if h := w.must(w.p.EntryHistory(ctx, a, "APP", "after")); len(h) != 2 {
		t.Fatalf("history = %+v", h)
	}

	w.entry(a, "taken", "x")
	_, err = w.p.MoveEntry(ctx, a, "APP", "after", "taken", got.Version)
	wantCode(t, err, "entry_exists")
}

func TestVault_DiffShowsTheChangeBetweenRevisions(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	x := w.entry(a, "notes", "first line")
	w.must(w.p.EditEntry(ctx, a, "APP", "notes", EntryEdit{Body: new("second line"), IfVersion: x.Version}))

	d := w.must(w.p.DiffEntry(ctx, a, "APP", "notes", 0, 0))
	if d.From != 1 || d.To != 2 || !strings.Contains(d.Diff, "-first line") || !strings.Contains(d.Diff, "+second line") {
		t.Fatalf("diff = %+v", d)
	}
	_, err := w.p.DiffEntry(ctx, a, "APP", "notes", 7, 0)
	wantCode(t, err, "revision_not_retained")
	_, err = w.p.DiffEntry(ctx, a, "APP", "notes", 0, 1)
	wantCode(t, err, "no_earlier_revision")
}

func TestVault_LintAndHealthReportStubsAndOrphans(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	w.entry(a, "alone", "nothing links here")
	w.entry(a, "hub", "see [[missing]]")
	w.entry(b, "bobs", "bob's own")

	diags := w.must(w.p.Lint(ctx, a, "APP"))
	has := func(kind, entry, ref string) bool {
		return slices.ContainsFunc(diags, func(d Diagnostic) bool { return d.Kind == kind && d.Entry == entry && d.Ref == ref })
	}
	if !has("stub", "hub", "missing") || !has("orphan", "alone", "") || has("orphan", "hub", "") {
		t.Fatalf("lint = %+v", diags)
	}
	if slices.ContainsFunc(diags, func(d Diagnostic) bool { return d.Entry == "bobs" }) {
		t.Fatalf("lint names bob's personal entry: %+v", diags)
	}

	lines := w.must(w.p.Health(ctx, a, "APP"))
	count := func(what string) int {
		i := slices.IndexFunc(lines, func(l HealthLine) bool { return l.What == what })
		if i < 0 {
			t.Fatalf("health has no line %q: %+v", what, lines)
		}
		return lines[i].Count
	}
	if count("entries") != 2 || count("stubs") != 1 || count("orphans") != 1 {
		t.Fatalf("health = %+v", lines)
	}
}
