package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCreateKnowledgeWithDirNestsTheSlugAndPath(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback runbook", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Slug != "deployment/rollback-runbook" {
		t.Fatalf("slug = %q, want deployment/rollback-runbook", doc.Slug)
	}
	wantSuffix := filepath.Join("deployment", "rollback-runbook.md")
	if !strings.HasSuffix(doc.Path, wantSuffix) {
		t.Fatalf("path = %q, want it to end with %q", doc.Path, wantSuffix)
	}
	if _, err := os.Stat(doc.Path); err != nil {
		t.Fatalf("the file must exist at the nested path: %v", err)
	}
}

func TestCreateKnowledgeWithNoDirStaysAtTheRoot(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Slug != "recall-ranking" {
		t.Fatalf("slug = %q, want recall-ranking", doc.Slug)
	}
}

func TestUniqueSlugOperatesOnTheFullPathNotJustTheLeaf(t *testing.T) {
	c, p, _ := kbCore(t)
	a, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	b, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	if a.Slug != "deployment/rollback" || b.Slug != "docs/rollback" {
		t.Fatalf("slugs = %q, %q — two directories must not force a numeric suffix", a.Slug, b.Slug)
	}
	// Same title, same directory: this pair does collide, and today's suffix
	// behavior is unchanged.
	c2, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge c2: %v", err)
	}
	if c2.Slug != "deployment/rollback-2" {
		t.Fatalf("slug = %q, want deployment/rollback-2", c2.Slug)
	}
}

func TestCreateKnowledgeRejectsABadInDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Dir: "../etc"}); pathErrCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestCreateKnowledgeTitledLikeAReservedNameGetsASuffix(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "CON"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if doc.Slug != "con-2" {
		t.Fatalf("slug = %q, want con-2: \"con\" alone cannot be opened on Windows", doc.Slug)
	}
	if _, err := os.Stat(doc.Path); err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
}

func TestCreateKnowledgeTruncatesAnOverLongTitleSlugAndKeepsTheFullTitle(t *testing.T) {
	c, p, _ := kbCore(t)
	title := "A title so long that its slugified form has to be truncated to fit the per-segment ceiling of ninety six characters"
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: title})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if len(doc.Slug) > maxPathSegmentLen {
		t.Fatalf("slug %q is %d characters, want <= %d", doc.Slug, len(doc.Slug), maxPathSegmentLen)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != title {
		t.Errorf("frontmatter title = %q, want the untruncated %q", fm.Title, title)
	}
}

func TestCreatingADirectoryThatResemblesAnExistingOneIsRefused(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook", Dir: "deployment"})
	if pathErrCode(err) != "similar_directory" {
		t.Fatalf("err = %v, want similar_directory", err)
	}
	if !strings.Contains(err.Error(), "deploy") {
		t.Errorf("error %q does not name the existing directory", err)
	}
}

func TestNewDirCreatesItAnyway(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook", Dir: "deployment", NewDir: true})
	if err != nil {
		t.Fatalf("CreateKnowledge with NewDir: %v", err)
	}
	if doc.Slug != "deployment/runbook" {
		t.Fatalf("slug = %q, want deployment/runbook", doc.Slug)
	}
}

func TestWritingIntoAnExistingDirectoryIsNeverRefused(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "First", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge first: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Second", Dir: "deployment", NewDir: true}); err != nil {
		t.Fatalf("CreateKnowledge second: %v", err)
	}
	// "deploy" now resembles "deployment", which also exists, but "deploy"
	// itself is an existing directory and writing into it is never refused.
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Third", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge third into the existing deploy/: %v", err)
	}
}

func TestApiDoesNotMatchApisLegacyAsADirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Dir: "apis-legacy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Y", Dir: "api"}); err != nil {
		t.Fatalf("api must not be refused as resembling apis-legacy: %v", err)
	}
}

func TestBareLeafOpensTheUniqueMatch(t *testing.T) {
	c, p, _ := kbCore(t)
	created, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	got, err := c.LoadKnowledge(t.Context(), p.ID, "rollback")
	if err != nil {
		t.Fatalf("LoadKnowledge by bare leaf: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("got %q, want %q", got.Slug, created.Slug)
	}
}

func TestBareLeafAmbiguityListsCandidatesAndOpensNeither(t *testing.T) {
	c, p, _ := kbCore(t)
	a, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	b, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	_, err = c.LoadKnowledge(t.Context(), p.ID, "rollback")
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "ambiguous_slug" {
		t.Fatalf("err = %v, want ambiguous_slug", err)
	}
	detail, ok := e.Detail.([]string)
	if !ok || !slices.Contains(detail, a.Slug) || !slices.Contains(detail, b.Slug) {
		t.Errorf("Detail = %v, want both %q and %q", e.Detail, a.Slug, b.Slug)
	}
}

func TestFullPathStillResolvesExactlyEvenWhenALeafIsAmbiguous(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"}); err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "docs"}); err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	got, err := c.LoadKnowledge(t.Context(), p.ID, "deployment/rollback")
	if err != nil {
		t.Fatalf("LoadKnowledge by full path: %v", err)
	}
	if got.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q", got.Slug)
	}
}

func TestBareLeafNotFoundIsTheOrdinaryNotFoundError(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.LoadKnowledge(t.Context(), p.ID, "nope")
	if pathErrCode(err) != "knowledge_not_found" {
		t.Fatalf("err = %v, want knowledge_not_found", err)
	}
}

func TestDeleteKnowledgeByBareLeaf(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := c.DeleteKnowledge(t.Context(), p.ID, "rollback"); err != nil {
		t.Fatalf("DeleteKnowledge by bare leaf: %v", err)
	}
	if _, err := os.Stat(doc.Path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

// rm never bare-leaf-resolves into the global vault: a project's rm is
// scoped to its own vault, even though show/edit/pin already look there.
func TestDeleteKnowledgeDoesNotBareLeafIntoTheGlobalVault(t *testing.T) {
	c, p, _ := kbCore(t)
	other := seededProject2(t, c)
	doc, err := c.CreateKnowledge(t.Context(), other.ID, NewKnowledge{Title: "Shared", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), other.ID, doc.Slug, "cross-project"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	// LoadKnowledge from an unrelated project finds it in the vault...
	if _, err := c.LoadKnowledge(t.Context(), p.ID, "shared"); err != nil {
		t.Fatalf("LoadKnowledge should find the global entry: %v", err)
	}
	// ...but DeleteKnowledge from that same unrelated project must not.
	if err := c.DeleteKnowledge(t.Context(), p.ID, "shared"); pathErrCode(err) != "knowledge_not_found" {
		t.Fatalf("err = %v, want knowledge_not_found: rm must not reach into another project's escalated entry", err)
	}
}

func TestMoveKnowledgeUpdatesSlugPathAndFile(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	oldPath := doc.Path
	moved, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/rollback-runbook", false)
	if err != nil {
		t.Fatalf("MoveKnowledge: %v", err)
	}
	if moved.Slug != "deployment/rollback-runbook" {
		t.Fatalf("slug = %q", moved.Slug)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("the old file must be gone: %v", err)
	}
	if _, err := os.Stat(moved.Path); err != nil {
		t.Fatalf("the new file must exist: %v", err)
	}
	if _, err := c.LoadKnowledge(t.Context(), p.ID, "deployment/rollback-runbook"); err != nil {
		t.Fatalf("the row must resolve at the new path: %v", err)
	}
}

func TestMoveKnowledgeRefusesToReplaceAnExistingSlug(t *testing.T) {
	c, p, _ := kbCore(t)
	a, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "A"})
	if err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "B"}); err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	_, err = c.MoveKnowledge(t.Context(), p.ID, a.Slug, "b", false)
	if pathErrCode(err) != "slug_taken" {
		t.Fatalf("err = %v, want slug_taken", err)
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Fatalf("a's file must be untouched: %v", err)
	}
}

func TestMoveKnowledgeChecksTheDestinationDirectoryForResemblance(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Runbook"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/runbook", false); pathErrCode(err) != "similar_directory" {
		t.Fatalf("err = %v, want similar_directory", err)
	}
	moved, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/runbook", true)
	if err != nil {
		t.Fatalf("MoveKnowledge with newDir: %v", err)
	}
	if moved.Slug != "deployment/runbook" {
		t.Fatalf("slug = %q", moved.Slug)
	}
}

func TestMoveKnowledgeRefusesAGlobalEntry(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Shared"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if _, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "renamed", false); pathErrCode(err) != "global_entry" {
		t.Fatalf("err = %v, want global_entry", err)
	}
}

func TestMoveKnowledgeMovesTheRevisionDirectoryIfPresent(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Standup"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	// Simulate the revision-history feature having already captured a
	// version: a hidden directory named ".<filename>" beside the entry.
	oldRevDir := revisionDirFor(doc.Path)
	if err := os.MkdirAll(oldRevDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRevDir, "1.md"), []byte("version one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "deployment/standup", false)
	if err != nil {
		t.Fatalf("MoveKnowledge: %v", err)
	}
	if _, err := os.Stat(oldRevDir); !os.IsNotExist(err) {
		t.Fatalf("old revision directory must be gone: %v", err)
	}
	newRevDir := revisionDirFor(moved.Path)
	got, err := os.ReadFile(filepath.Join(newRevDir, "1.md"))
	if err != nil {
		t.Fatalf("revision file must have moved with the entry: %v", err)
	}
	if string(got) != "version one\n" {
		t.Errorf("revision content = %q", got)
	}
}

func TestMoveKnowledgeIsFineWithNoRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Plain"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.MoveKnowledge(t.Context(), p.ID, doc.Slug, "elsewhere", false); err != nil {
		t.Fatalf("MoveKnowledge: %v", err)
	}
}

func TestMoveKnowledgeByBareLeaf(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	moved, err := c.MoveKnowledge(t.Context(), p.ID, "rollback", "docs/rollback", false)
	if err != nil {
		t.Fatalf("MoveKnowledge by bare leaf: %v", err)
	}
	if moved.ID != doc.ID || moved.Slug != "docs/rollback" {
		t.Fatalf("moved = %+v", moved)
	}
}

func TestEscalateKnowledgePreservesTheSubpath(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	global, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if global.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q, want the subpath preserved", global.Slug)
	}
	if filepath.Base(filepath.Dir(global.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved on disk too", global.Path)
	}
}

func TestDemoteKnowledgePreservesTheSubpath(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	back, err := c.DemoteKnowledge(t.Context(), doc.Slug, "reason")
	if err != nil {
		t.Fatalf("DemoteKnowledge: %v", err)
	}
	if back.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q, want the subpath preserved", back.Slug)
	}
	if filepath.Base(filepath.Dir(back.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved on disk too", back.Path)
	}
}

func TestEscalateKnowledgeMovesTheRevisionDirectory(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	oldRevDir := revisionDirFor(doc.Path)
	if err := os.MkdirAll(oldRevDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRevDir, "1.md"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	global, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if _, err := os.Stat(revisionDirFor(global.Path)); err != nil {
		t.Fatalf("revision directory must have moved: %v", err)
	}
}

// DemoteKnowledge and VerifyKnowledge look a global entry up by slug with a
// direct query, not through loadDoc/resolveSlug, so they need their own fix
// for a directory-shaped slug: today they flatten the input with a
// whole-string Slugify, which would turn "deployment/rollback" into
// "deployment-rollback" and never find the row.
func TestDemoteKnowledgeResolvesADirectoryShapedSlug(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if _, err := c.DemoteKnowledge(t.Context(), "deployment/rollback", "reason"); err != nil {
		t.Fatalf("DemoteKnowledge by full path: %v", err)
	}
}

func TestVerifyKnowledgeResolvesADirectoryShapedSlug(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.EscalateKnowledge(t.Context(), p.ID, doc.Slug, "reason"); err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	if err := c.VerifyKnowledge(t.Context(), "deployment/rollback"); err != nil {
		t.Fatalf("VerifyKnowledge by full path: %v", err)
	}
}

func TestListKnowledgeFiltersByTagsRequiringAll(t *testing.T) {
	c, p, _ := kbCore(t)
	both, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Both", Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("CreateKnowledge both: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "OnlyA", Tags: []string{"a"}}); err != nil {
		t.Fatalf("CreateKnowledge onlyA: %v", err)
	}
	docs, err := c.ListKnowledge(t.Context(), p.ID, KnowledgeFilter{Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("ListKnowledge: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != both.ID {
		t.Fatalf("docs = %+v, want only %q", docs, both.Slug)
	}
}

func TestListKnowledgeScopesToADirectoryAndItsSubtree(t *testing.T) {
	c, p, _ := kbCore(t)
	root, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateKnowledge root: %v", err)
	}
	nested, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Setup", Dir: "deployment/aws", NewDir: true})
	if err != nil {
		t.Fatalf("CreateKnowledge nested: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Elsewhere", Dir: "docs"}); err != nil {
		t.Fatalf("CreateKnowledge elsewhere: %v", err)
	}
	docs, err := c.ListKnowledge(t.Context(), p.ID, KnowledgeFilter{Dir: "deployment"})
	if err != nil {
		t.Fatalf("ListKnowledge: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %+v, want the 2 entries under deployment/", docs)
	}
	ids := map[string]bool{docs[0].ID: true, docs[1].ID: true}
	if !ids[root.ID] || !ids[nested.ID] {
		t.Errorf("docs = %+v, want %q and %q", docs, root.Slug, nested.Slug)
	}
}

func findingsOfKind(findings []LintFinding, kind string) []LintFinding {
	var out []LintFinding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestLintReportsDeepDirectories(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment/aws/runbooks"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := findingsOfKind(findings, "deep_directory"); len(got) != 1 {
		t.Fatalf("findings = %+v, want one deep_directory", findings)
	}
}

func TestLintDoesNotReportDeepDirectoryAtDepthTwo(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Rollback", Dir: "deployment/aws"}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := findingsOfKind(findings, "deep_directory"); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestLintReportsALongDirectoryName(t *testing.T) {
	c, p, _ := kbCore(t)
	long := "a-directory-name-that-is-well-past-thirty-characters"
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Dir: long}); err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	got := findingsOfKind(findings, "long_directory_name")
	if len(got) != 1 || got[0].Ref != long {
		t.Fatalf("findings = %+v, want one naming %q", findings, long)
	}
}

func TestLintReportsSimilarDirectories(t *testing.T) {
	c, p, _ := kbCore(t)
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "A", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateKnowledge a: %v", err)
	}
	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "B", Dir: "deployment", NewDir: true}); err != nil {
		t.Fatalf("CreateKnowledge b: %v", err)
	}
	findings, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := findingsOfKind(findings, "similar_directory"); len(got) == 0 {
		t.Fatalf("findings = %+v, want at least one similar_directory", findings)
	}
}
