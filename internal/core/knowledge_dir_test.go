package core

import (
	"os"
	"path/filepath"
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
