package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func kbCore(t *testing.T) (*Core, Project, Board) {
	t.Helper()
	c := testCore(t)
	c.WithKBRoot(t.TempDir())
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	return c, p, b
}

func TestCreateKnowledgeWritesFileAndRow(t *testing.T) {
	c, p, _ := kbCore(t)

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Concurrency model", Template: "decision", Summary: "Leases, not locks",
		Sources: []string{"https://example.com/design-notes"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Slug != "concurrency-model" || doc.Ref != "/XPSCTL/knowledge/concurrency-model" {
		t.Errorf("slug/ref = %q/%q", doc.Slug, doc.Ref)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
	fm, body, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Concurrency model" || fm.Template != "decision" {
		t.Errorf("frontmatter = %+v", fm)
	}
	if !strings.Contains(body, "## Options considered") {
		t.Error("body should come from the decision template")
	}

	// A second entry with the same title gets a distinct slug, like board slugs.
	dup, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Concurrency model"})
	if err != nil {
		t.Fatal(err)
	}
	if dup.Slug != "concurrency-model-2" {
		t.Errorf("second slug = %q, want concurrency-model-2", dup.Slug)
	}
}

func TestExternalEditWins(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes"})
	if err != nil {
		t.Fatal(err)
	}

	// Someone edits the file in Obsidian: new title, new body, new tag.
	edited := RenderDoc(Frontmatter{Title: "Edited elsewhere", Tags: []string{"sqlite"}},
		"Changed by hand. See [[nowhere]].\n")
	if err := os.WriteFile(doc.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make the change visible to a stat-based check regardless of timer
	// granularity.
	past := time.Unix(0, 0)
	_ = os.Chtimes(doc.Path, past, past)

	got, err := c.LoadKnowledge(t.Context(), p.ID, "notes")
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if got.Title != "Edited elsewhere" {
		t.Errorf("Title = %q, want the file's title: the file always wins", got.Title)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "sqlite" {
		t.Errorf("Tags = %v, want [sqlite] from the edited frontmatter", got.Tags)
	}
	if got.Version <= doc.Version {
		t.Errorf("Version = %d, want a bump after the external edit", got.Version)
	}
}

func TestWikilinksResolveAndStub(t *testing.T) {
	c, p, _ := kbCore(t)
	target, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Concurrency model", Body: "# Concurrency model\n\n## Parallel safety\n\nLeases.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Design",
		Body: "Builds on [[concurrency-model#parallel safety]] and [[not-written-yet]].\n" +
			"Also [[concurrency-model#no such heading]].\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	back, err := c.Backlinks(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Fatalf("Backlinks = %+v, want both references from %s", back, src.Slug)
	}

	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, f := range findings {
		kinds[f.Kind]++
	}
	if kinds["stub"] != 1 {
		t.Errorf("stubs = %d, want 1 ([[not-written-yet]]); findings: %+v", kinds["stub"], findings)
	}
	if kinds["broken_anchor"] != 1 {
		t.Errorf("broken anchors = %d, want 1; findings: %+v", kinds["broken_anchor"], findings)
	}
}

func TestLinkCardToDocAndOrphanLint(t *testing.T) {
	c, p, b := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Runbook", Body: "# Runbook\n\n## Steps\n\n1. Do it.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	findings, _ := c.Lint(t.Context(), p.ID)
	if len(findings) != 1 || findings[0].Kind != "orphan" {
		t.Fatalf("findings = %+v, want one orphan", findings)
	}

	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LinkCardToDoc(t.Context(), p.ID, CardRef{Seq: card.Seq}, "runbook#steps"); err != nil {
		t.Fatalf("LinkCardToDoc: %v", err)
	}

	back, err := c.Backlinks(t.Context(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].FromType != "card" || back[0].Anchor != "steps" {
		t.Fatalf("Backlinks = %+v, want the card with anchor steps", back)
	}
	if findings, _ := c.Lint(t.Context(), p.ID); len(findings) != 0 {
		t.Errorf("findings = %+v, want none: the entry is referenced now", findings)
	}
}

func TestDeleteKnowledgeRemovesFileAndStubsInboundLinks(t *testing.T) {
	c, p, _ := kbCore(t)
	target, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Source", Body: "See [[target]].\n"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteKnowledge(t.Context(), p.ID, "target"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target.Path); !os.IsNotExist(err) {
		t.Errorf("file still present at %s", target.Path)
	}
	findings, _ := c.Lint(t.Context(), p.ID)
	if len(findings) != 1 || findings[0].Kind != "stub" {
		t.Errorf("findings = %+v, want the dangling reference reported as a stub", findings)
	}
	if _, err := os.Stat(filepath.Dir(target.Path)); err != nil {
		t.Errorf("the vault directory should survive: %v", err)
	}
}

func TestSearchSpansCardsAndKnowledge(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{
		Title: "fix wal checkpoint stall"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "WAL checkpointing", Body: "Readers never block writers.\n"}); err != nil {
		t.Fatal(err)
	}

	hits, err := c.Search(t.Context(), p.ID, "wal", SearchOpts{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
	}
	if !kinds["card"] || !kinds["knowledge"] {
		t.Errorf("hits = %+v, want one of each kind", hits)
	}
}

func TestSearchByLabelCoversBothStores(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateLabel(t.Context(), p.ID, "research", "a spike or investigation"); err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "spike", Labels: []string{"research"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Spike results", Labels: []string{"research"}, Body: "spike findings\n"}); err != nil {
		t.Fatal(err)
	}
	hits, err := c.Search(t.Context(), p.ID, "spike", SearchOpts{Label: "research"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("hits = %+v, want the card and the entry (card %s)", hits, card.Ref)
	}
}

func TestSearchMatchesTheSummary(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Concurrency model", Summary: "Leases, not locks; writes renew",
		Body: "# Concurrency model\n\nDetail elsewhere.\n",
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := c.Search(t.Context(), p.ID, "leases", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Errorf("hits = %+v, want the entry whose summary carries the term", hits)
	}
}

// A reference may be written before the entry it names. Creating that entry is
// what turns the stub into a real edge, so the backlink appears without the
// referring document being touched again.
func TestCreatingEntryResolvesInboundStubs(t *testing.T) {
	c, p, _ := kbCore(t)
	src, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Design", Body: "Depends on [[lease-protocol]].\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Kind != "stub" {
		t.Fatalf("findings = %+v, want the forward reference reported as a stub", findings)
	}

	target, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Lease protocol", Body: "How leases are taken.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.Slug != "lease-protocol" {
		t.Fatalf("Slug = %q, want lease-protocol", target.Slug)
	}

	back, err := c.Backlinks(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Title != src.Title {
		t.Errorf("Backlinks = %+v, want the reference from %s to have resolved", back, src.Slug)
	}
	findings, err = c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Kind == "stub" {
			t.Errorf("findings = %+v, want no stub once the target exists", findings)
			break
		}
	}
}

// A deleted entry leaves its inbound references as stubs; re-creating it under
// the same slug must adopt them again rather than stranding them.
func TestRecreatingEntryReclaimsStubbedLinks(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Lease protocol", Body: "First cut.\n",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Design", Body: "Depends on [[lease-protocol]].\n",
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteKnowledge(t.Context(), p.ID, "lease-protocol"); err != nil {
		t.Fatal(err)
	}
	again, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Lease protocol", Body: "Second cut.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(t.Context(), again.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 {
		t.Errorf("Backlinks = %+v, want the stubbed reference reclaimed", back)
	}
}

func TestProvenanceDefaultsToAuthored(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Lease renewal"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Provenance != "authored" {
		t.Errorf("provenance = %q, want authored", doc.Provenance)
	}
}

func TestProvenanceReachesBothTheFileAndTheRow(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()

	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
		Title: "Lease renewal", Provenance: "extracted",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Provenance != "extracted" {
		t.Errorf("row provenance = %q", doc.Provenance)
	}
	// The file is the source of truth, so the mark has to be in it.
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "provenance: extracted") {
		t.Errorf("frontmatter is missing provenance:\n%s", raw)
	}
}

func TestProvenanceSurvivesAnEdit(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()

	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
		Title: "Lease renewal", Provenance: "prompted",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	// A mark that does not survive an edit cannot anchor an experiment.
	edited, err := c.EditKnowledge(ctx, p.ID, doc.Slug, "rewritten body\n", &doc.Version)
	if err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if edited.Provenance != "prompted" {
		t.Errorf("provenance after edit = %q, want prompted", edited.Provenance)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "provenance: prompted") {
		t.Errorf("edit dropped provenance from the file:\n%s", raw)
	}
}

func TestUnknownProvenanceIsRejectedWithTheAllowedSet(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Lease renewal", Provenance: "vibes",
	})
	if err == nil {
		t.Fatal("an invented provenance was accepted")
	}
	for _, want := range []string{"authored", "prompted", "extracted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q: %v", want, err)
		}
	}
}

func TestProvenanceIsReadBackFromTheFile(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()

	doc, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Lease renewal"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "provenance: authored", "provenance: extracted", 1)
	if edited == string(raw) {
		t.Fatal("expected a provenance line to rewrite")
	}
	if err := os.WriteFile(doc.Path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// The file is the record; editing it out of band must win.
	got, err := c.ReadKnowledge(ctx, p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("ReadKnowledge: %v", err)
	}
	if got.Provenance != "extracted" {
		t.Errorf("provenance = %q, want the file's value", got.Provenance)
	}
}

func TestListKnowledgeFiltersByTypeAndProvenance(t *testing.T) {
	c, p, _ := kbCore(t)
	ctx := t.Context()

	mk := func(title, template, prov string, sources ...string) {
		t.Helper()
		if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
			Title: title, Template: template, Provenance: prov, Sources: sources,
		}); err != nil {
			t.Fatalf("CreateKnowledge %s: %v", title, err)
		}
	}
	mk("Chosen storage", "decision", "authored", "https://example.com/storage-comparison")
	mk("Measured latency", "finding", "authored", "https://example.com/latency-numbers")
	mk("Overheard preference", "", "extracted")

	cases := []struct {
		name string
		f    KnowledgeFilter
		want int
	}{
		{"zero value lists everything", KnowledgeFilter{}, 3},
		{"one type", KnowledgeFilter{Templates: []string{"decision"}}, 1},
		{"two types", KnowledgeFilter{Templates: []string{"decision", "finding"}}, 2},
		{"one provenance", KnowledgeFilter{Provenances: []string{"extracted"}}, 1},
		{"both dimensions", KnowledgeFilter{
			Templates: []string{"decision"}, Provenances: []string{"authored"}}, 1},
		{"both dimensions, no overlap", KnowledgeFilter{
			Templates: []string{"decision"}, Provenances: []string{"extracted"}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			docs, err := c.ListKnowledge(ctx, p.ID, tc.f)
			if err != nil {
				t.Fatalf("ListKnowledge: %v", err)
			}
			if len(docs) != tc.want {
				t.Errorf("got %d entries, want %d", len(docs), tc.want)
			}
		})
	}
}

func TestKnowledgeFilterBuildsTheSameQueryEveryTime(t *testing.T) {
	// The clauses come out of a map, and map order reaching the query would
	// make the same filter produce different SQL between runs.
	f := KnowledgeFilter{Templates: []string{"decision"}, Provenances: []string{"extracted"}}
	first, _ := f.where()
	for range 20 {
		if got, _ := f.where(); got != first {
			t.Fatalf("where() = %q then %q", first, got)
		}
	}
}

func TestCreateKnowledgeSetWritesExtraFields(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Rollback the API", Template: "runbook", Set: map[string]string{"owner": "alice"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Extra["owner"] != "alice" {
		t.Errorf("Extra = %+v, want owner: alice", fm.Extra)
	}
}

func TestCreateKnowledgeSetRefusesANamedField(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "X", Set: map[string]string{"title": "Y"},
	})
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "reserved_field" {
		t.Fatalf("err = %v, want reserved_field", err)
	}
}

func TestCreateKnowledgeRejectsAMissingRequiredField(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: reject\nrequired: [owner]\n---\n# {{title}}\n\nOwner: {{owner}}\n"
	if err := os.WriteFile(filepath.Join(dir, "strict.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Template: "strict"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "owner") {
		t.Fatalf("err = %v, want template_violation naming owner", err)
	}
	docs, err := c.ListKnowledge(t.Context(), p.ID, KnowledgeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Error("a rejected template must write nothing")
	}
}

func TestCreateKnowledgeWarnsInsteadOfRejecting(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: warn\nrequired: [owner]\n---\n# {{title}}\n"
	if err := os.WriteFile(filepath.Join(dir, "lenient.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Template: "lenient"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "owner") {
		t.Errorf("Warnings = %v, want one naming owner", doc.Warnings)
	}
}

func TestCreateKnowledgeChecksSectionsOnlyWhenBodyIsSupplied(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: reject\n---\n# {{title}}\n\n## Steps\n"
	if err := os.WriteFile(filepath.Join(dir, "sectioned.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "From skeleton", Template: "sectioned",
	}); err != nil {
		t.Fatalf("skeleton body: %v", err)
	}

	_, err = c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Missing a section", Template: "sectioned", Body: "# Missing a section\n\nNo headings here.\n",
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(strings.Join(e.Problems, "\n"), "Steps") {
		t.Fatalf("err = %v, want template_violation naming Steps", err)
	}
}

func TestCreateKnowledgeRecordsSourcesAndReadsThemBack(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Cited", Sources: []string{"https://example.com", "  "},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if len(doc.Sources) != 1 || doc.Sources[0] != "https://example.com" {
		t.Fatalf("Sources = %v, want the blank entry dropped", doc.Sources)
	}

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Sources) != 1 || got.Sources[0] != "https://example.com" {
		t.Errorf("Sources after reload = %v", got.Sources)
	}
}

func TestEditKnowledgeReplacesSources(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Backfilled", Sources: []string{"https://example.com"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	next := []string{"https://example.com", "https://example.org"}
	got, err := c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug,
		KnowledgeEdit{Sources: &next, IfVersion: &doc.Version})
	if err != nil {
		t.Fatalf("EditKnowledgeFields: %v", err)
	}
	if len(got.Sources) != 2 {
		t.Errorf("Sources = %v, want two", got.Sources)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(fm.Sources) != 2 {
		t.Errorf("file lists %v, want two sources", fm.Sources)
	}
}

// review-knowledge #14: a revision directory can outlive the entry it
// belonged to when its file and row are removed outside Trellis -- the
// exact state --orphan-history exists to clean up. A new entry created at
// the same slug before that runs must not adopt that stale history as its
// own: its "version 1" would then really be an old, possibly private,
// entry's last version.
func TestCreateKnowledgeRefusesAStaleRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Old", Body: "the old private body\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := os.Stat(revisionFilePath(doc.Path, 1)); err != nil {
		t.Fatalf("version 1 must exist before the simulated outside removal: %v", err)
	}
	// What removing a file and its row outside Trellis leaves behind: the
	// revision directory survives on its own.
	if err := os.Remove(doc.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`DELETE FROM knowledge WHERE id = ?`, doc.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Old", Body: "brand new body\n"}); !isCode(err, "stale_history") {
		t.Fatalf("err = %v, want stale_history", err)
	}
	// The refusal must not have written anything either.
	if _, err := os.Stat(doc.Path); !os.IsNotExist(err) {
		t.Errorf("CreateKnowledge left a file behind despite refusing: %v", err)
	}
}

// review-knowledge #15: removing `template:` from a file by hand must clear
// the row's template too. cmpOr kept the old value whenever the file's was
// empty, which is right for Title (never meant to become blank) but wrong
// here: an empty template is a meaningful value, "none", not "unknown, keep
// the old one".
func TestRemovingTemplateByHandClearsTheRow(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Plan", Template: "runbook"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Template != "runbook" {
		t.Fatalf("Template = %q, want runbook", doc.Template)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, body, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	fm.Template = ""
	if err := os.WriteFile(doc.Path, []byte(RenderDoc(fm, body)), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if got.Template != "" {
		t.Errorf("Template = %q after removing the key by hand, want empty", got.Template)
	}
}

func TestKnowledgeFieldsFromSet(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Test entry",
		Set:   map[string]string{"owner": "alice", "severity": "high"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Fields == nil {
		t.Error("Fields should be non-nil")
	}
	if doc.Fields["owner"] != "alice" {
		t.Errorf("Fields[owner] = %v, want alice", doc.Fields["owner"])
	}
	if doc.Fields["severity"] != "high" {
		t.Errorf("Fields[severity] = %v, want high", doc.Fields["severity"])
	}
}

func TestKnowledgeFieldsEmpty(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Plain entry",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Fields == nil {
		t.Error("Fields should be non-nil even when empty")
	}
	if len(doc.Fields) != 0 {
		t.Errorf("Fields = %v, want empty map", doc.Fields)
	}
}

func TestKnowledgeFieldsFromYAMLList(t *testing.T) {
	c, p, _ := kbCore(t)
	// Create a knowledge entry, then modify the file to add a YAML list field
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Team members",
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	// Modify the file to add a YAML list field
	fm := Frontmatter{
		Title: "Team members",
		Extra: map[string]any{
			"members": []any{"alice", "bob", "charlie"},
		},
	}
	body := "# Team members\n\nThis is our team.\n"
	raw := RenderDoc(fm, body)
	if err := os.WriteFile(doc.Path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make the change visible to a stat-based check
	past := time.Unix(0, 0)
	_ = os.Chtimes(doc.Path, past, past)

	// Load it and check Fields
	loaded, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	members, ok := loaded.Fields["members"].([]string)
	if !ok {
		t.Fatalf("Fields[members] = %v, want []string", loaded.Fields["members"])
	}
	if len(members) != 3 || members[0] != "alice" || members[1] != "bob" || members[2] != "charlie" {
		t.Errorf("members = %v, want [alice bob charlie]", members)
	}
}
