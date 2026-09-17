package core

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func (f *mergeFixture) entry(p Project, title, body string) Entry {
	f.t.Helper()
	e, err := f.c.CreateEntry(f.t.Context(), p.ID, NewEntry{Title: title, Body: body})
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

func backlinkRefs(t *testing.T, c *Core, entryID string) []string {
	t.Helper()
	links, err := c.Backlinks(t.Context(), entryID)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, l := range links {
		refs = append(refs, l.Ref)
	}
	return refs
}

func TestMergeMovesEntriesAndRewritesAddresses(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	runbook := f.entry(f.api, "Runbook", "Roll back with care.\n\n## Steps\n\nDo it.\n")
	f.entry(f.api, "Index", "See [[runbook#steps]] and [[/API/vault/runbook]].\n")
	cite := f.entry(core, "Citations", "Read [[/API/vault/runbook#steps|the runbook]].\n\n`[[/API/vault/runbook]]`\n")
	f.entry(f.mono, "Overview", "Waits for [[runbook]].\n")
	card := f.card(f.api, f.apiBoard, "linked", nil, nil)
	if err := f.c.LinkCardToEntry(ctx, f.api.ID, ParseCardRef(card.Ref), "/API/vault/runbook#steps"); err != nil {
		t.Fatal(err)
	}

	plan := f.merge(MergeOptions{Apply: true})

	if plan.Entries.Moved != 2 {
		t.Errorf("moved = %d", plan.Entries.Moved)
	}
	rewritten := slices.Clone(plan.EntriesRewritten)
	slices.Sort(rewritten)
	if !slices.Equal(rewritten, []string{"/CORE/vault/citations", "/MONO/vault/index"}) {
		t.Errorf("rewritten = %v", plan.EntriesRewritten)
	}

	moved := filepath.Join(f.root, "projects", "MONO", "vault", "runbook.md")
	entry, err := f.c.ReadEntry(ctx, f.mono.ID, "runbook")
	if err != nil || entry.Path != moved || entry.Ref != "/MONO/vault/runbook" {
		t.Fatalf("runbook = %+v, %v", entry, err)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("moved file: %v", err)
	}

	text := readFile(t, cite.Path)
	if !strings.Contains(text, "[[/MONO/vault/runbook#steps|the runbook]]") ||
		!strings.Contains(text, "`[[/API/vault/runbook]]`") {
		t.Errorf("citations file:\n%s", text)
	}
	index, err := f.c.ReadEntry(ctx, f.mono.ID, "index")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index.BodyMD, "[[runbook#steps]]") || !strings.Contains(index.BodyMD, "[[/MONO/vault/runbook]]") {
		t.Errorf("index body:\n%s", index.BodyMD)
	}

	refs := backlinkRefs(t, f.c, runbook.ID)
	for _, want := range []string{"/CORE/vault/citations", "/MONO/vault/index", "/MONO/vault/overview", "API-1"} {
		if !slices.Contains(refs, want) {
			t.Errorf("backlinks %v lack %s", refs, want)
		}
	}
	if n := f.count(`SELECT count(*) FROM link WHERE from_type = 'card' AND to_raw = '/MONO/vault/runbook#steps'`); n != 1 {
		t.Errorf("card link targets rewritten: %d", n)
	}
}

func TestMergeCollapsesAnIdenticalEntry(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.entry(f.api, "Shared", "Identical body.\n")
	m := f.entry(f.mono, "Shared", "Different for now.\n")
	if err := os.WriteFile(m.Path, []byte(readFile(t, a.Path)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReadEntry(ctx, f.mono.ID, "shared"); err != nil {
		t.Fatal(err)
	}
	f.entry(f.api, "Citer", "See [[shared]].\n")

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Equal(plan.Entries.Collapsed, []string{"shared"}) || plan.Entries.Moved != 1 {
		t.Errorf("entries = %+v", plan.Entries)
	}
	if n := f.count(`SELECT count(*) FROM entry WHERE slug = 'shared'`); n != 1 {
		t.Errorf("%d shared entries remain", n)
	}
	if refs := backlinkRefs(t, f.c, m.ID); !slices.Contains(refs, "/MONO/vault/citer") {
		t.Errorf("backlinks of the survivor = %v", refs)
	}
}

// recordEvent looks up its project from the entry row itself; collapsing
// must record the "collapsed" event before deleting that row, or the event's
// project_id lands NULL -- which retire()'s later re-homing does not match
// either -- and it never reaches DST's project-scoped feed.
func TestMergeCollapsedEntryEventIsProjectScoped(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.entry(f.api, "Shared", "Identical body.\n")
	if err := os.WriteFile(f.entry(f.mono, "Shared", "x\n").Path, []byte(readFile(t, a.Path)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReadEntry(ctx, f.mono.ID, "shared"); err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	events, _, err := f.c.EventFeed(ctx, EventQuery{ProjectID: f.mono.ID, Kinds: []string{"entry"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(events, func(e FeedEvent) bool { return e.Action == "collapsed" }) {
		t.Errorf("MONO's project-scoped feed lacks the collapse event: %+v", events)
	}
}

func TestMergeStopsOnADifferingEntry(t *testing.T) {
	f := newMergeFixture(t)
	f.entry(f.api, "Runbook", "API's way.\n")
	f.entry(f.mono, "Runbook", "MONO's way.\n")
	before := f.snapshot()

	plan := f.merge(MergeOptions{})
	if plan.Ready || len(plan.Entries.Conflicts) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	got := plan.Entries.Conflicts[0]
	if got.Name != "runbook" || got.SrcHash == got.DstHash || got.Vault {
		t.Errorf("conflict = %+v", got)
	}
	_, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true})
	if code := errCode(t, err); code != "merge_conflicts" || !strings.Contains(err.Error(), "--rename-conflicts") {
		t.Errorf("apply: %v", err)
	}
	if after := f.snapshot(); after != before {
		t.Errorf("a refused merge changed something:\n%s\n%s", before, after)
	}
}

func TestMergeRenamesConflictsOnRequest(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	f.entry(f.api, "Runbook", "API's way.\n")
	f.entry(f.mono, "Runbook", "MONO's way.\n")
	f.entry(f.api, "Index", "[[runbook]] and [[/API/vault/runbook#x]]\n")
	home := f.entry(f.mono, "Home", "[[runbook]]\n")
	elsewhere := f.entry(core, "Elsewhere", "[[/API/vault/runbook]]\n")

	plan := f.merge(MergeOptions{Apply: true, RenameConflicts: true})

	if !slices.Equal(plan.Entries.Renamed, []Rename{{From: "runbook", To: "runbook-api"}}) {
		t.Errorf("renamed = %+v", plan.Entries.Renamed)
	}
	index := readFile(t, filepath.Join(f.root, "projects", "MONO", "vault", "index.md"))
	if !strings.Contains(index, "[[runbook-api]] and [[/MONO/vault/runbook-api#x]]") {
		t.Errorf("index:\n%s", index)
	}
	if got := readFile(t, home.Path); !strings.Contains(got, "[[runbook]]") || strings.Contains(got, "runbook-api") {
		t.Errorf("MONO's own link changed:\n%s", got)
	}
	if got := readFile(t, elsewhere.Path); !strings.Contains(got, "[[/MONO/vault/runbook-api]]") {
		t.Errorf("elsewhere:\n%s", got)
	}
	renamed, err := f.c.ReadEntry(ctx, f.mono.ID, "runbook-api")
	if err != nil || !strings.Contains(renamed.BodyMD, "API's way.") {
		t.Errorf("runbook-api = %+v, %v", renamed, err)
	}
}

func TestMergeNeverRenamesAVaultEntry(t *testing.T) {
	f := newMergeFixture(t)
	f.entry(f.api, "Conventions", "API's.\n")
	if _, err := f.c.PromoteEntry(t.Context(), f.api.ID, "conventions", "shared"); err != nil {
		t.Fatal(err)
	}
	f.entry(f.mono, "Conventions", "MONO's.\n")

	plan := f.merge(MergeOptions{RenameConflicts: true})
	if plan.Ready || len(plan.Entries.Conflicts) != 1 || !plan.Entries.Conflicts[0].Vault {
		t.Fatalf("plan = %+v", plan.Entries)
	}
	_, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true, RenameConflicts: true})
	if code := errCode(t, err); code != "merge_conflicts" || !strings.Contains(err.Error(), "demote") {
		t.Errorf("apply: %v", err)
	}
}

func TestMergeMovesAVaultEntryItOwns(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.entry(f.api, "Conventions", "Shared.\n")
	promoted, err := f.c.PromoteEntry(ctx, f.api.ID, "conventions", "shared")
	if err != nil {
		t.Fatal(err)
	}
	vaultPath := promoted.Path

	f.merge(MergeOptions{Apply: true})

	var row struct {
		ProjectID string `db:"project_id"`
		Global    bool   `db:"global"`
		Slug      string `db:"slug"`
	}
	if err := f.c.db.Get(&row, `SELECT project_id, global, slug FROM entry WHERE id = ?`, promoted.ID); err != nil {
		t.Fatal(err)
	}
	if row.ProjectID != f.mono.ID || !row.Global || f.c.entryPath(GlobalKey, true, row.Slug) != vaultPath {
		t.Errorf("vault row = %+v", row)
	}
	got, err := f.c.ReadEntry(ctx, "", "/GLOBAL/vault/conventions")
	if err != nil || got.Ref != "/GLOBAL/vault/conventions" {
		t.Errorf("vault entry = %+v, %v", got, err)
	}
}

func TestMergeEntryPlanMatchesApply(t *testing.T) {
	f := newMergeFixture(t)
	f.entry(f.api, "Runbook", "x\n")
	f.entry(f.api, "Index", "[[/API/vault/runbook]]\n")
	plan := f.merge(MergeOptions{})
	applied := f.merge(MergeOptions{Apply: true})
	applied.Backup, applied.Warnings = "", nil
	if !reflect.DeepEqual(plan, applied) {
		t.Errorf("plan and apply differ:\nplan    %+v\napplied %+v", plan, applied)
	}
	backup := filepath.Join(f.root, "backups")
	entries, err := os.ReadDir(backup)
	if err != nil || len(entries) != 1 {
		t.Fatalf("backups: %v, %v", entries, err)
	}
	copied := filepath.Join(backup, entries[0].Name(), "files", "projects", "API", "vault", "runbook.md")
	if _, err := os.Stat(copied); err != nil {
		t.Errorf("the backup lacks the moved file: %v", err)
	}
}

// A failure after files have moved, and after a citing entry has really
// been rewritten, puts every file back and changes nothing. Two citing
// entries in different projects: references() visits them in ascending id
// order, so blocking whichever one sorts second still lets the other's
// rewrite land on disk before the merge fails -- the half of this test a
// single blocked entry could never reach.
func TestMergeFailureRestoresFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny writes here")
	}
	f := newMergeFixture(t)
	core, _ := f.project("CORE")
	team, _ := f.project("TEAM")
	runbook := f.entry(f.api, "Runbook", "x\n")
	// A revision, so its move is undone too.
	if _, err := f.c.EditEntry(t.Context(), f.api.ID, runbook.Slug, "y\n", &runbook.Version); err != nil {
		t.Fatal(err)
	}
	citeCore := f.entry(core, "Citations", "[[/API/vault/runbook]]\n")
	citeTeam := f.entry(team, "Notes", "[[/API/vault/runbook]]\n")
	free, freeAddr, blocked := citeCore, "/CORE/vault/citations", citeTeam
	if citeTeam.ID < citeCore.ID {
		free, freeAddr, blocked = citeTeam, "/TEAM/vault/notes", citeCore
	}
	freeOriginal := readFile(t, free.Path)
	before := f.snapshot()

	// blocked's directory refuses new files, so its rewrite fails there --
	// but only after free's has already succeeded.
	dir := filepath.Dir(blocked.Path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	applied, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true})
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err == nil {
		t.Fatal("the merge succeeded")
	}
	if !slices.Contains(applied.EntriesRewritten, freeAddr) {
		t.Fatalf("rewritten = %v, want it to include %s: this test proves nothing otherwise",
			applied.EntriesRewritten, freeAddr)
	}
	if got := readFile(t, free.Path); got != freeOriginal {
		t.Errorf("the entry whose rewrite landed was not restored:\n%s", got)
	}
	if after := f.snapshot(); after != before {
		t.Errorf("a failed merge left changes:\nbefore %s\nafter  %s", before, after)
	}
}

// An entry keeps its history through a merge: its revisions move with it,
// and an entry whose links the merge rewrites keeps the text it had.
func TestMergeMovesRevisionsAndKeepsOneForARewrite(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	runbook := f.entry(f.api, "Runbook", "first\n")
	if _, err := f.c.EditEntry(ctx, f.api.ID, runbook.Slug, "second\n", &runbook.Version); err != nil {
		t.Fatal(err)
	}
	cite := f.entry(core, "Citations", "[[/API/vault/runbook]]\n")

	f.merge(MergeOptions{Apply: true})

	// Version 1 is the retained copy in both cases.
	has1 := func(projectID, slug string) {
		t.Helper()
		revs, err := f.c.ListEntryRevisions(ctx, projectID, slug)
		if err != nil || !slices.ContainsFunc(revs, func(r RevisionInfo) bool { return r.Version == 1 }) {
			t.Fatalf("%s revisions = %+v, %v", slug, revs, err)
		}
	}
	has1(f.mono.ID, "runbook")
	moved := filepath.Join(f.root, "projects", "MONO", "vault", "runbook.md")
	if old := readFile(t, revisionFilePath(moved, 1)); !strings.Contains(old, "first") {
		t.Errorf("moved revision = %q, want the first text", old)
	}
	has1(core.ID, cite.Slug)
	if old := readFile(t, revisionFilePath(cite.Path, 1)); !strings.Contains(old, "[[/API/vault/runbook]]") {
		t.Errorf("kept revision = %q, want the text before the rewrite", old)
	}
}

// An edit made on disk and not yet read back still has its links rewritten:
// the files, not the link rows, say who cites SRC.
func TestMergeRewritesLinksTheDatabaseHasNotSeen(t *testing.T) {
	f := newMergeFixture(t)
	core, _ := f.project("CORE")
	f.entry(f.api, "Runbook", "x\n")
	notes := f.entry(core, "Notes", "Nothing yet.\n")
	edited := readFile(t, notes.Path) + "\nSee [[/API/vault/runbook]].\n"
	if err := os.WriteFile(notes.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Contains(plan.EntriesRewritten, "/CORE/vault/notes") {
		t.Errorf("rewritten = %v", plan.EntriesRewritten)
	}
	if got := readFile(t, notes.Path); !strings.Contains(got, "[[/MONO/vault/runbook]]") {
		t.Errorf("notes:\n%s", got)
	}
}

func TestMergeKeepsBothReasonsOfOneActorsNominations(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.entry(f.api, "Shared", "Identical body.\n")
	m := f.entry(f.mono, "Shared", "Different for now.\n")
	if err := os.WriteFile(m.Path, []byte(readFile(t, a.Path)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReadEntry(ctx, f.mono.ID, "shared"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.NominateEntry(ctx, f.api.ID, "shared", "api reason"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.NominateEntry(ctx, f.mono.ID, "shared", "mono reason"); err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	var reasons []string
	if err := f.c.db.Select(&reasons, `SELECT reason FROM nomination WHERE entry_id = ?`, m.ID); err != nil {
		t.Fatal(err)
	}
	if len(reasons) != 1 || !strings.Contains(reasons[0], "mono reason") || !strings.Contains(reasons[0], "api reason") {
		t.Errorf("nominations = %q", reasons)
	}
}

func TestMergePlanSeesAnUntrackedFileInTheWay(t *testing.T) {
	f := newMergeFixture(t)
	f.entry(f.api, "Runbook", "x\n")
	writeFile(t, filepath.Join(f.root, "projects", "MONO", "vault", "runbook.md"), "not tracked")

	plan := f.merge(MergeOptions{})

	if plan.Ready || len(plan.Entries.Conflicts) != 1 ||
		!strings.Contains(plan.Entries.Conflicts[0].Reason, "no entry") {
		t.Errorf("plan = %+v", plan.Entries)
	}
}

// The backup is taken before the apply's transaction. A file changed in
// between stops the apply; it never escapes the backup.
func TestMergeStopsWhenAFileChangesAfterTheBackup(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	entry := f.entry(f.api, "Runbook", "x\n")
	f.c.SetDropDerived(func(context.Context, string) error {
		return os.WriteFile(entry.Path, []byte("edited meanwhile\n"), 0o600)
	})

	_, err := f.c.MergeProjects(ctx, "API", "MONO", MergeOptions{Apply: true})

	if got := errCode(t, err); got != "merge_changed" {
		t.Fatalf("code = %s", got)
	}
	if _, err := f.c.ProjectByKey(ctx, "API"); err != nil {
		t.Errorf("API was merged anyway: %v", err)
	}
	if got := readFile(t, entry.Path); got != "edited meanwhile\n" {
		t.Errorf("the edited file = %q", got)
	}
}

// A decision entry's sources: frontmatter names things by absolute address,
// the same way a wikilink or a trellis link target does, and must follow the
// same three objects through a merge: an entry, an artifact renamed
// here by a conflict, and a card, whose stored ref never changes.
func TestMergeRewritesSourcesAddresses(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	f.entry(f.api, "Runbook", "Roll back with care.\n")
	f.artifact(f.mono, "shot.png", "mono pixels")
	f.artifact(f.api, "shot.png", "api pixels")
	card := f.card(f.api, f.apiBoard, "task", nil, nil)
	if card.Ref != "API-1" {
		t.Fatalf("card ref = %s, want API-1", card.Ref)
	}
	decision, err := f.c.CreateEntry(ctx, core.ID, NewEntry{
		Title: "Adopt X", Template: "decision",
		Sources: []string{"/API/vault/runbook", "/API/artifacts/shot.png", "/API/cards/API-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := f.snapshot()

	plan := f.merge(MergeOptions{RenameConflicts: true})
	if !plan.Ready {
		t.Fatalf("plan not ready: %+v", plan)
	}
	if !slices.Contains(plan.EntriesRewritten, "/CORE/vault/adopt-x") {
		t.Errorf("plan's rewritten = %v", plan.EntriesRewritten)
	}
	if after := f.snapshot(); after != before {
		t.Errorf("a plan changed something:\nbefore %s\nafter  %s", before, after)
	}

	applied := f.merge(MergeOptions{Apply: true, RenameConflicts: true})
	if !slices.Contains(applied.EntriesRewritten, "/CORE/vault/adopt-x") {
		t.Errorf("applied's rewritten = %v", applied.EntriesRewritten)
	}

	got, err := f.c.ReadEntry(ctx, core.ID, decision.Slug)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/MONO/vault/runbook", "/MONO/artifacts/shot-api.png", "/MONO/cards/API-1"}
	if !slices.Equal(got.Sources, want) {
		t.Errorf("sources = %v, want %v", got.Sources, want)
	}

	diagnostics, err := f.c.Lint(ctx, core.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Kind == "template_violation" {
			t.Errorf("diagnostic: %+v", diagnostic)
		}
	}
}

// A project with no entries still has addresses others cite: its artifacts
// and cards. Their sources: items follow the merge all the same.
func TestMergeRewritesSourcesWhenSRCHasNoEntries(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	f.artifact(f.api, "shot.png", "api pixels")
	card := f.card(f.api, f.apiBoard, "task", nil, nil)
	decision, err := f.c.CreateEntry(ctx, core.ID, NewEntry{
		Title: "Adopt Y", Template: "decision",
		Sources: []string{"/API/artifacts/shot.png", "/API/cards/" + card.Ref},
	})
	if err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	got, err := f.c.ReadEntry(ctx, core.ID, decision.Slug)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/MONO/artifacts/shot.png", "/MONO/cards/" + card.Ref}
	if !slices.Equal(got.Sources, want) {
		t.Errorf("sources = %v, want %v", got.Sources, want)
	}
}
