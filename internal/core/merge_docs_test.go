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

func (f *mergeFixture) doc(p Project, title, body string) Knowledge {
	f.t.Helper()
	d, err := f.c.CreateKnowledge(f.t.Context(), p.ID, NewKnowledge{Title: title, Body: body})
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}

func backlinkRefs(t *testing.T, c *Core, docID string) []string {
	t.Helper()
	links, err := c.Backlinks(t.Context(), docID)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, l := range links {
		refs = append(refs, l.Ref)
	}
	return refs
}

func TestMergeMovesDocumentsAndRewritesAddresses(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	runbook := f.doc(f.api, "Runbook", "Roll back with care.\n\n## Steps\n\nDo it.\n")
	f.doc(f.api, "Index", "See [[runbook#steps]] and [[/API/knowledge/runbook]].\n")
	cite := f.doc(core, "Citations", "Read [[/API/knowledge/runbook#steps|the runbook]].\n\n`[[/API/knowledge/runbook]]`\n")
	f.doc(f.mono, "Overview", "Waits for [[runbook]].\n")
	card := f.card(f.api, f.apiBoard, "linked", nil, nil)
	if err := f.c.LinkCardToDoc(ctx, f.api.ID, ParseCardRef(card.Ref), "/API/knowledge/runbook#steps"); err != nil {
		t.Fatal(err)
	}

	plan := f.merge(MergeOptions{Apply: true})

	if plan.Knowledge.Moved != 2 {
		t.Errorf("moved = %d", plan.Knowledge.Moved)
	}
	rewritten := slices.Clone(plan.DocumentsRewritten)
	slices.Sort(rewritten)
	if !slices.Equal(rewritten, []string{"/CORE/knowledge/citations", "/MONO/knowledge/index"}) {
		t.Errorf("rewritten = %v", plan.DocumentsRewritten)
	}

	moved := filepath.Join(f.root, "projects", "MONO", "knowledge", "runbook.md")
	doc, err := f.c.ReadKnowledge(ctx, f.mono.ID, "runbook")
	if err != nil || doc.Path != moved || doc.Ref != "/MONO/knowledge/runbook" {
		t.Fatalf("runbook = %+v, %v", doc, err)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("moved file: %v", err)
	}

	text := readFile(t, cite.Path)
	if !strings.Contains(text, "[[/MONO/knowledge/runbook#steps|the runbook]]") ||
		!strings.Contains(text, "`[[/API/knowledge/runbook]]`") {
		t.Errorf("citations file:\n%s", text)
	}
	index, err := f.c.ReadKnowledge(ctx, f.mono.ID, "index")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index.BodyMD, "[[runbook#steps]]") || !strings.Contains(index.BodyMD, "[[/MONO/knowledge/runbook]]") {
		t.Errorf("index body:\n%s", index.BodyMD)
	}

	refs := backlinkRefs(t, f.c, runbook.ID)
	for _, want := range []string{"/CORE/knowledge/citations", "/MONO/knowledge/index", "/MONO/knowledge/overview", "API-1"} {
		if !slices.Contains(refs, want) {
			t.Errorf("backlinks %v lack %s", refs, want)
		}
	}
	if n := f.count(`SELECT count(*) FROM link WHERE from_type = 'card' AND to_raw = '/MONO/knowledge/runbook#steps'`); n != 1 {
		t.Errorf("card link targets rewritten: %d", n)
	}
}

func TestMergeCollapsesAnIdenticalDocument(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.doc(f.api, "Shared", "Identical body.\n")
	m := f.doc(f.mono, "Shared", "Different for now.\n")
	if err := os.WriteFile(m.Path, []byte(readFile(t, a.Path)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReadKnowledge(ctx, f.mono.ID, "shared"); err != nil {
		t.Fatal(err)
	}
	f.doc(f.api, "Citer", "See [[shared]].\n")

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Equal(plan.Knowledge.Collapsed, []string{"shared"}) || plan.Knowledge.Moved != 1 {
		t.Errorf("knowledge = %+v", plan.Knowledge)
	}
	if n := f.count(`SELECT count(*) FROM knowledge WHERE slug = 'shared'`); n != 1 {
		t.Errorf("%d shared entries remain", n)
	}
	if refs := backlinkRefs(t, f.c, m.ID); !slices.Contains(refs, "/MONO/knowledge/citer") {
		t.Errorf("backlinks of the survivor = %v", refs)
	}
}

func TestMergeStopsOnADifferingDocument(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Runbook", "API's way.\n")
	f.doc(f.mono, "Runbook", "MONO's way.\n")
	before := f.snapshot()

	plan := f.merge(MergeOptions{})
	if plan.Ready || len(plan.Knowledge.Conflicts) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	got := plan.Knowledge.Conflicts[0]
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
	f.doc(f.api, "Runbook", "API's way.\n")
	f.doc(f.mono, "Runbook", "MONO's way.\n")
	f.doc(f.api, "Index", "[[runbook]] and [[/API/knowledge/runbook#x]]\n")
	home := f.doc(f.mono, "Home", "[[runbook]]\n")
	elsewhere := f.doc(core, "Elsewhere", "[[/API/knowledge/runbook]]\n")

	plan := f.merge(MergeOptions{Apply: true, RenameConflicts: true})

	if !slices.Equal(plan.Knowledge.Renamed, []Rename{{From: "runbook", To: "runbook-api"}}) {
		t.Errorf("renamed = %+v", plan.Knowledge.Renamed)
	}
	index := readFile(t, filepath.Join(f.root, "projects", "MONO", "knowledge", "index.md"))
	if !strings.Contains(index, "[[runbook-api]] and [[/MONO/knowledge/runbook-api#x]]") {
		t.Errorf("index:\n%s", index)
	}
	if got := readFile(t, home.Path); !strings.Contains(got, "[[runbook]]") || strings.Contains(got, "runbook-api") {
		t.Errorf("MONO's own link changed:\n%s", got)
	}
	if got := readFile(t, elsewhere.Path); !strings.Contains(got, "[[/MONO/knowledge/runbook-api]]") {
		t.Errorf("elsewhere:\n%s", got)
	}
	renamed, err := f.c.ReadKnowledge(ctx, f.mono.ID, "runbook-api")
	if err != nil || !strings.Contains(renamed.BodyMD, "API's way.") {
		t.Errorf("runbook-api = %+v, %v", renamed, err)
	}
}

func TestMergeNeverRenamesAVaultEntry(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Conventions", "API's.\n")
	if _, err := f.c.EscalateKnowledge(t.Context(), f.api.ID, "conventions", "shared"); err != nil {
		t.Fatal(err)
	}
	f.doc(f.mono, "Conventions", "MONO's.\n")

	plan := f.merge(MergeOptions{RenameConflicts: true})
	if plan.Ready || len(plan.Knowledge.Conflicts) != 1 || !plan.Knowledge.Conflicts[0].Vault {
		t.Fatalf("plan = %+v", plan.Knowledge)
	}
	_, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true, RenameConflicts: true})
	if code := errCode(t, err); code != "merge_conflicts" || !strings.Contains(err.Error(), "demote") {
		t.Errorf("apply: %v", err)
	}
}

func TestMergeMovesAVaultEntryItOwns(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.doc(f.api, "Conventions", "Shared.\n")
	escalated, err := f.c.EscalateKnowledge(ctx, f.api.ID, "conventions", "shared")
	if err != nil {
		t.Fatal(err)
	}
	var vaultPath string
	if err := f.c.db.Get(&vaultPath, `SELECT path FROM knowledge WHERE id = ?`, escalated.ID); err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	var row struct {
		ProjectID string `db:"project_id"`
		Global    bool   `db:"global"`
		Path      string `db:"path"`
	}
	if err := f.c.db.Get(&row, `SELECT project_id, global, path FROM knowledge WHERE id = ?`, escalated.ID); err != nil {
		t.Fatal(err)
	}
	if row.ProjectID != f.mono.ID || !row.Global || row.Path != vaultPath {
		t.Errorf("vault row = %+v", row)
	}
	got, err := f.c.ReadKnowledge(ctx, "", "/GLOBAL/knowledge/conventions")
	if err != nil || got.Ref != "/GLOBAL/knowledge/conventions" {
		t.Errorf("vault entry = %+v, %v", got, err)
	}
}

func TestMergeDocumentPlanMatchesApply(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Runbook", "x\n")
	f.doc(f.api, "Index", "[[/API/knowledge/runbook]]\n")
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
	copied := filepath.Join(backup, entries[0].Name(), "files", "projects", "API", "knowledge", "runbook.md")
	if _, err := os.Stat(copied); err != nil {
		t.Errorf("the backup lacks the moved file: %v", err)
	}
}

// A failure after files have moved puts every file back and changes nothing.
func TestMergeFailureRestoresFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not deny writes here")
	}
	f := newMergeFixture(t)
	core, _ := f.project("CORE")
	runbook := f.doc(f.api, "Runbook", "x\n")
	// A revision, so its move is undone too.
	if _, err := f.c.EditKnowledge(t.Context(), f.api.ID, runbook.Slug, "y\n", &runbook.Version); err != nil {
		t.Fatal(err)
	}
	cite := f.doc(core, "Citations", "[[/API/knowledge/runbook]]\n")
	before := f.snapshot()

	// The rewrite of CORE's document happens after API's files moved; a
	// directory that refuses new files makes it fail there.
	dir := filepath.Dir(cite.Path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	_, err := f.c.MergeProjects(t.Context(), "API", "MONO", MergeOptions{Apply: true})
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err == nil {
		t.Fatal("the merge succeeded")
	}
	if after := f.snapshot(); after != before {
		t.Errorf("a failed merge left changes:\nbefore %s\nafter  %s", before, after)
	}
}

// An entry keeps its history through a merge: its revisions move with it,
// and a document whose links the merge rewrites keeps the text it had.
func TestMergeMovesRevisionsAndKeepsOneForARewrite(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	core, _ := f.project("CORE")
	runbook := f.doc(f.api, "Runbook", "first\n")
	if _, err := f.c.EditKnowledge(ctx, f.api.ID, runbook.Slug, "second\n", &runbook.Version); err != nil {
		t.Fatal(err)
	}
	cite := f.doc(core, "Citations", "[[/API/knowledge/runbook]]\n")

	f.merge(MergeOptions{Apply: true})

	// Version 1 is the retained copy in both cases.
	has1 := func(projectID, slug string) {
		t.Helper()
		revs, err := f.c.ListKnowledgeRevisions(ctx, projectID, slug)
		if err != nil || !slices.ContainsFunc(revs, func(r RevisionInfo) bool { return r.Version == 1 }) {
			t.Fatalf("%s revisions = %+v, %v", slug, revs, err)
		}
	}
	has1(f.mono.ID, "runbook")
	moved := filepath.Join(f.root, "projects", "MONO", "knowledge", "runbook.md")
	if old := readFile(t, revisionFilePath(moved, 1)); !strings.Contains(old, "first") {
		t.Errorf("moved revision = %q, want the first text", old)
	}
	has1(core.ID, cite.Slug)
	if old := readFile(t, revisionFilePath(cite.Path, 1)); !strings.Contains(old, "[[/API/knowledge/runbook]]") {
		t.Errorf("kept revision = %q, want the text before the rewrite", old)
	}
}

// An edit made on disk and not yet read back still has its links rewritten:
// the files, not the link rows, say who cites SRC.
func TestMergeRewritesLinksTheDatabaseHasNotSeen(t *testing.T) {
	f := newMergeFixture(t)
	core, _ := f.project("CORE")
	f.doc(f.api, "Runbook", "x\n")
	notes := f.doc(core, "Notes", "Nothing yet.\n")
	edited := readFile(t, notes.Path) + "\nSee [[/API/knowledge/runbook]].\n"
	if err := os.WriteFile(notes.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Contains(plan.DocumentsRewritten, "/CORE/knowledge/notes") {
		t.Errorf("rewritten = %v", plan.DocumentsRewritten)
	}
	if got := readFile(t, notes.Path); !strings.Contains(got, "[[/MONO/knowledge/runbook]]") {
		t.Errorf("notes:\n%s", got)
	}
}

func TestMergeKeepsBothReasonsOfOneActorsNominations(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.doc(f.api, "Shared", "Identical body.\n")
	m := f.doc(f.mono, "Shared", "Different for now.\n")
	if err := os.WriteFile(m.Path, []byte(readFile(t, a.Path)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ReadKnowledge(ctx, f.mono.ID, "shared"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.NominateKnowledge(ctx, f.api.ID, "shared", "api reason"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.NominateKnowledge(ctx, f.mono.ID, "shared", "mono reason"); err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	var reasons []string
	if err := f.c.db.Select(&reasons, `SELECT reason FROM nomination WHERE knowledge_id = ?`, m.ID); err != nil {
		t.Fatal(err)
	}
	if len(reasons) != 1 || !strings.Contains(reasons[0], "mono reason") || !strings.Contains(reasons[0], "api reason") {
		t.Errorf("nominations = %q", reasons)
	}
}

func TestMergePlanSeesAnUntrackedFileInTheWay(t *testing.T) {
	f := newMergeFixture(t)
	f.doc(f.api, "Runbook", "x\n")
	writeFile(t, filepath.Join(f.root, "projects", "MONO", "knowledge", "runbook.md"), "not tracked")

	plan := f.merge(MergeOptions{})

	if plan.Ready || len(plan.Knowledge.Conflicts) != 1 ||
		!strings.Contains(plan.Knowledge.Conflicts[0].Reason, "no entry") {
		t.Errorf("plan = %+v", plan.Knowledge)
	}
}

// The backup is taken before the apply's transaction. A file changed in
// between stops the apply; it never escapes the backup.
func TestMergeStopsWhenAFileChangesAfterTheBackup(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	doc := f.doc(f.api, "Runbook", "x\n")
	f.c.SetDropDerived(func(context.Context, string) error {
		return os.WriteFile(doc.Path, []byte("edited meanwhile\n"), 0o600)
	})

	_, err := f.c.MergeProjects(ctx, "API", "MONO", MergeOptions{Apply: true})

	if got := errCode(t, err); got != "merge_changed" {
		t.Fatalf("code = %s", got)
	}
	if _, err := f.c.ProjectByKey(ctx, "API"); err != nil {
		t.Errorf("API was merged anyway: %v", err)
	}
	if got := readFile(t, doc.Path); got != "edited meanwhile\n" {
		t.Errorf("the edited file = %q", got)
	}
}
