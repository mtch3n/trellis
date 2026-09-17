package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCreateEntryWithDirNestsTheSlugAndPath(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback runbook", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entry.Slug != "deployment/rollback-runbook" {
		t.Fatalf("slug = %q, want deployment/rollback-runbook", entry.Slug)
	}
	wantSuffix := filepath.Join("deployment", "rollback-runbook.md")
	if !strings.HasSuffix(entry.Path, wantSuffix) {
		t.Fatalf("path = %q, want it to end with %q", entry.Path, wantSuffix)
	}
	if _, err := os.Stat(entry.Path); err != nil {
		t.Fatalf("the file must exist at the nested path: %v", err)
	}
}

func TestCreateEntryWithNoDirStaysAtTheRoot(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Recall ranking"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entry.Slug != "recall-ranking" {
		t.Fatalf("slug = %q, want recall-ranking", entry.Slug)
	}
}

func TestUniqueSlugOperatesOnTheFullPathNotJustTheLeaf(t *testing.T) {
	c, p, _ := vaultCore(t)
	a, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry a: %v", err)
	}
	b, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateEntry b: %v", err)
	}
	if a.Slug != "deployment/rollback" || b.Slug != "docs/rollback" {
		t.Fatalf("slugs = %q, %q — two directories must not force a numeric suffix", a.Slug, b.Slug)
	}
	// Same title, same directory: this pair does collide, and today's suffix
	// behavior is unchanged.
	c2, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry c2: %v", err)
	}
	if c2.Slug != "deployment/rollback-2" {
		t.Fatalf("slug = %q, want deployment/rollback-2", c2.Slug)
	}
}

func TestCreateEntryRejectsABadInDirectory(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "X", Dir: "../etc"}); pathErrCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestCreateEntryTitledLikeAReservedNameGetsASuffix(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "CON"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entry.Slug != "con-2" {
		t.Fatalf("slug = %q, want con-2: \"con\" alone cannot be opened on Windows", entry.Slug)
	}
	if _, err := os.Stat(entry.Path); err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
}

func TestCreateEntryTruncatesAnOverLongTitleSlugAndKeepsTheFullTitle(t *testing.T) {
	c, p, _ := vaultCore(t)
	title := "A title so long that its slugified form has to be truncated to fit the per-segment ceiling of ninety six characters"
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: title})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if len(entry.Slug) > maxPathSegmentLen {
		t.Fatalf("slug %q is %d characters, want <= %d", entry.Slug, len(entry.Slug), maxPathSegmentLen)
	}
	raw, err := os.ReadFile(entry.Path)
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
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	_, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Runbook", Dir: "deployment"})
	if pathErrCode(err) != "similar_directory" {
		t.Fatalf("err = %v, want similar_directory", err)
	}
	if !strings.Contains(err.Error(), "deploy") {
		t.Errorf("error %q does not name the existing directory", err)
	}
}

func TestNewDirCreatesItAnyway(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Runbook", Dir: "deployment", NewDir: true})
	if err != nil {
		t.Fatalf("CreateEntry with NewDir: %v", err)
	}
	if entry.Slug != "deployment/runbook" {
		t.Fatalf("slug = %q, want deployment/runbook", entry.Slug)
	}
}

func TestWritingIntoAnExistingDirectoryIsNeverRefused(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "First", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateEntry first: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Second", Dir: "deployment", NewDir: true}); err != nil {
		t.Fatalf("CreateEntry second: %v", err)
	}
	// "deploy" now resembles "deployment", which also exists, but "deploy"
	// itself is an existing directory and writing into it is never refused.
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Third", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateEntry third into the existing deploy/: %v", err)
	}
}

func TestApiDoesNotMatchApisLegacyAsADirectory(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "X", Dir: "apis-legacy"}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Y", Dir: "api"}); err != nil {
		t.Fatalf("api must not be refused as resembling apis-legacy: %v", err)
	}
}

func TestBareLeafOpensTheUniqueMatch(t *testing.T) {
	c, p, _ := vaultCore(t)
	created, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	got, err := c.LoadEntry(t.Context(), p.ID, "rollback")
	if err != nil {
		t.Fatalf("LoadEntry by bare leaf: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("got %q, want %q", got.Slug, created.Slug)
	}
}

func TestBareLeafAmbiguityListsCandidatesAndOpensNeither(t *testing.T) {
	c, p, _ := vaultCore(t)
	a, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry a: %v", err)
	}
	b, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateEntry b: %v", err)
	}
	_, err = c.LoadEntry(t.Context(), p.ID, "rollback")
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
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"}); err != nil {
		t.Fatalf("CreateEntry a: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "docs"}); err != nil {
		t.Fatalf("CreateEntry b: %v", err)
	}
	got, err := c.LoadEntry(t.Context(), p.ID, "deployment/rollback")
	if err != nil {
		t.Fatalf("LoadEntry by full path: %v", err)
	}
	if got.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q", got.Slug)
	}
}

func TestBareLeafNotFoundIsTheOrdinaryNotFoundError(t *testing.T) {
	c, p, _ := vaultCore(t)
	_, err := c.LoadEntry(t.Context(), p.ID, "nope")
	if pathErrCode(err) != "entry_not_found" {
		t.Fatalf("err = %v, want entry_not_found", err)
	}
}

func TestDeleteEntryByBareLeaf(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if err := c.DeleteEntry(t.Context(), p.ID, "rollback"); err != nil {
		t.Fatalf("DeleteEntry by bare leaf: %v", err)
	}
	if _, err := os.Stat(entry.Path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

// rm never bare-leaf-resolves into the global vault: a project's rm is
// scoped to its own vault, even though show/edit/pin already look there.
func TestDeleteEntryDoesNotBareLeafIntoTheGlobalVault(t *testing.T) {
	c, p, _ := vaultCore(t)
	other := seededProject2(t, c)
	entry, err := c.CreateEntry(t.Context(), other.ID, NewEntry{Title: "Shared", Dir: "docs"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.PromoteEntry(t.Context(), other.ID, entry.Slug, "cross-project"); err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	// LoadEntry from an unrelated project finds it in the vault...
	if _, err := c.LoadEntry(t.Context(), p.ID, "shared"); err != nil {
		t.Fatalf("LoadEntry should find the global entry: %v", err)
	}
	// ...but DeleteEntry from that same unrelated project must not.
	if err := c.DeleteEntry(t.Context(), p.ID, "shared"); pathErrCode(err) != "entry_not_found" {
		t.Fatalf("err = %v, want entry_not_found: rm must not reach into another project's promoted entry", err)
	}
}

func TestMoveEntryUpdatesSlugPathAndFile(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	oldPath := entry.Path
	moved, err := c.MoveEntry(t.Context(), p.ID, entry.Slug, "deployment/rollback-runbook", false)
	if err != nil {
		t.Fatalf("MoveEntry: %v", err)
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
	if _, err := c.LoadEntry(t.Context(), p.ID, "deployment/rollback-runbook"); err != nil {
		t.Fatalf("the row must resolve at the new path: %v", err)
	}
}

func TestMoveEntryRefusesToReplaceAnExistingSlug(t *testing.T) {
	c, p, _ := vaultCore(t)
	a, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "A"})
	if err != nil {
		t.Fatalf("CreateEntry a: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "B"}); err != nil {
		t.Fatalf("CreateEntry b: %v", err)
	}
	_, err = c.MoveEntry(t.Context(), p.ID, a.Slug, "b", false)
	if pathErrCode(err) != "slug_taken" {
		t.Fatalf("err = %v, want slug_taken", err)
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Fatalf("a's file must be untouched: %v", err)
	}
}

func TestMoveEntryChecksTheDestinationDirectoryForResemblance(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Notes", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Runbook"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.MoveEntry(t.Context(), p.ID, entry.Slug, "deployment/runbook", false); pathErrCode(err) != "similar_directory" {
		t.Fatalf("err = %v, want similar_directory", err)
	}
	moved, err := c.MoveEntry(t.Context(), p.ID, entry.Slug, "deployment/runbook", true)
	if err != nil {
		t.Fatalf("MoveEntry with newDir: %v", err)
	}
	if moved.Slug != "deployment/runbook" {
		t.Fatalf("slug = %q", moved.Slug)
	}
}

func TestMoveEntryRefusesAGlobalEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Shared"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason"); err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if _, err := c.MoveEntry(t.Context(), p.ID, entry.Slug, "renamed", false); pathErrCode(err) != "global_entry" {
		t.Fatalf("err = %v, want global_entry", err)
	}
}

// review-knowledge #8: MoveEntry gave ref to resolveSlug directly instead
// of parsing it through readEntryArg first, so the canonical address show,
// search and recall all print (/KEY/vault/x) was rejected as
// entry_not_found.
func TestMoveEntryAcceptsTheCanonicalAddress(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	address := "/" + p.Key + "/vault/" + entry.Slug
	moved, err := c.MoveEntry(t.Context(), p.ID, address, "deploy/rollback", false)
	if err != nil {
		t.Fatalf("MoveEntry(%s): %v", address, err)
	}
	if moved.Slug != "deploy/rollback" {
		t.Errorf("slug = %q, want deploy/rollback", moved.Slug)
	}
}

// The address spec requires an address naming another project to be
// refused with wrong_project, not treated as not-found.
func TestMoveEntryRefusesAnotherProjectsAddress(t *testing.T) {
	c, p, _ := vaultCore(t)
	p2 := seededProject2(t, c)
	entry, err := c.CreateEntry(t.Context(), p2.ID, NewEntry{Title: "Elsewhere"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	address := "/" + p2.Key + "/vault/" + entry.Slug
	if _, err := c.MoveEntry(t.Context(), p.ID, address, "new-name", false); pathErrCode(err) != "wrong_project" {
		t.Fatalf("err = %v, want wrong_project", err)
	}
}

// review-knowledge #7: a move changes an entry's address exactly the way
// promote and demote do, and resolveEntryStubs must run for it too, so a
// wikilink written to the new path before the move backfills instead of
// staying a stub until the referrer's own file next changes.
func TestMoveEntryBackfillsStubsThatNameTheNewPath(t *testing.T) {
	c, p, _ := vaultCore(t)
	referrer, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Index", Body: "[[deploy/rollback]]\n"})
	if err != nil {
		t.Fatalf("CreateEntry referrer: %v", err)
	}
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateEntry target: %v", err)
	}

	diagnostics, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stubbedBefore bool
	for _, f := range diagnostics {
		if f.Kind == "stub" && f.Entry == referrer.Ref {
			stubbedBefore = true
		}
	}
	if !stubbedBefore {
		t.Fatal("the link must be a stub before the move: nothing lives at deploy/rollback yet")
	}

	if _, err := c.MoveEntry(t.Context(), p.ID, target.Slug, "deploy/rollback", false); err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}

	diagnostics, err = c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range diagnostics {
		if f.Kind == "stub" && f.Entry == referrer.Ref {
			t.Errorf("still a stub after the move resolved it: %+v", f)
		}
	}
}

func TestMoveEntryMovesTheRevisionDirectoryIfPresent(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Standup"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	// Simulate the revision-history feature having already captured a
	// version: a hidden directory named ".<filename>" beside the entry.
	oldRevDir := revisionDir(entry.Path)
	if err := os.MkdirAll(oldRevDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRevDir, "1.md"), []byte("version one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved, err := c.MoveEntry(t.Context(), p.ID, entry.Slug, "deployment/standup", false)
	if err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}
	if _, err := os.Stat(oldRevDir); !os.IsNotExist(err) {
		t.Fatalf("old revision directory must be gone: %v", err)
	}
	newRevDir := revisionDir(moved.Path)
	got, err := os.ReadFile(filepath.Join(newRevDir, "1.md"))
	if err != nil {
		t.Fatalf("revision file must have moved with the entry: %v", err)
	}
	if string(got) != "version one\n" {
		t.Errorf("revision content = %q", got)
	}
}

func TestMoveEntryIsFineWithNoRevisionDirectory(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Plain"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.MoveEntry(t.Context(), p.ID, entry.Slug, "elsewhere", false); err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}
}

func TestMoveEntryByBareLeaf(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	moved, err := c.MoveEntry(t.Context(), p.ID, "rollback", "docs/rollback", false)
	if err != nil {
		t.Fatalf("MoveEntry by bare leaf: %v", err)
	}
	if moved.ID != entry.ID || moved.Slug != "docs/rollback" {
		t.Fatalf("moved = %+v", moved)
	}
}

func TestPromoteEntryPreservesTheSubpath(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	global, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason")
	if err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if global.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q, want the subpath preserved", global.Slug)
	}
	if filepath.Base(filepath.Dir(global.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved on disk too", global.Path)
	}
}

func TestDemoteEntryPreservesTheSubpath(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason"); err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	back, err := c.DemoteEntry(t.Context(), entry.Slug, "reason")
	if err != nil {
		t.Fatalf("DemoteEntry: %v", err)
	}
	if back.Slug != "deployment/rollback" {
		t.Fatalf("slug = %q, want the subpath preserved", back.Slug)
	}
	if filepath.Base(filepath.Dir(back.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved on disk too", back.Path)
	}
}

func TestPromoteEntryMovesTheRevisionDirectory(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	oldRevDir := revisionDir(entry.Path)
	if err := os.MkdirAll(oldRevDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRevDir, "1.md"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	global, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason")
	if err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if _, err := os.Stat(revisionDir(global.Path)); err != nil {
		t.Fatalf("revision directory must have moved: %v", err)
	}
}

// review-knowledge #3: promoting a nested entry must move its revision
// directory to sit beside the entry at its new subpath, not to the vault
// root -- moving it to <global>/.rollback.md instead of
// <global>/deployment/.rollback.md detaches its history (revisionDir(dest)
// then names a directory that does not exist) and collides with a
// root-level "rollback" promoted from elsewhere afterward.
func TestPromoteDemoteMoveTheRevisionDirectoryForANestedSlug(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "v2\n", &entry.Version); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}

	global, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason")
	if err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if filepath.Base(filepath.Dir(global.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved", global.Path)
	}
	// The bug's destination: the revision directory at the vault root
	// instead of beside the entry under deployment/.
	rootRevDir := filepath.Join(filepath.Dir(filepath.Dir(global.Path)), ".rollback.md")
	if _, err := os.Stat(rootRevDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the revision directory landed at the vault root (%s) instead of beside the entry", rootRevDir)
	}
	for v := int64(1); v <= 2; v++ {
		if _, err := os.Stat(revisionFilePath(global.Path, v)); err != nil {
			t.Errorf("version %d missing after promote at %s: %v", v, revisionDir(global.Path), err)
		}
	}

	back, err := c.DemoteEntry(t.Context(), global.Slug, "reason")
	if err != nil {
		t.Fatalf("DemoteEntry: %v", err)
	}
	if filepath.Base(filepath.Dir(back.Path)) != "deployment" {
		t.Fatalf("path = %q, want the subpath preserved after demote", back.Path)
	}
	for v := int64(1); v <= 2; v++ {
		if _, err := os.Stat(revisionFilePath(back.Path, v)); err != nil {
			t.Errorf("version %d missing after demote at %s: %v", v, revisionDir(back.Path), err)
		}
	}
}

// DemoteEntry and VerifyEntry look a global entry up by slug with a
// direct query, not through loadEntry/resolveSlug, so they need their own fix
// for a directory-shaped slug: today they flatten the input with a
// whole-string Slugify, which would turn "deployment/rollback" into
// "deployment-rollback" and never find the row.
func TestDemoteEntryResolvesADirectoryShapedSlug(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason"); err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if _, err := c.DemoteEntry(t.Context(), "deployment/rollback", "reason"); err != nil {
		t.Fatalf("DemoteEntry by full path: %v", err)
	}
}

func TestVerifyEntryResolvesADirectoryShapedSlug(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "reason"); err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}
	if err := c.VerifyEntry(t.Context(), "deployment/rollback"); err != nil {
		t.Fatalf("VerifyEntry by full path: %v", err)
	}
}

func TestListEntriesFiltersByTagsRequiringAll(t *testing.T) {
	c, p, _ := vaultCore(t)
	both, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Both", Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("CreateEntry both: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "OnlyA", Tags: []string{"a"}}); err != nil {
		t.Fatalf("CreateEntry onlyA: %v", err)
	}
	entries, err := c.ListEntries(t.Context(), p.ID, EntryFilter{Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != both.ID {
		t.Fatalf("entries = %+v, want only %q", entries, both.Slug)
	}
}

func TestListEntriesScopesToADirectoryAndItsSubtree(t *testing.T) {
	c, p, _ := vaultCore(t)
	root, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry root: %v", err)
	}
	nested, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Setup", Dir: "deployment/aws", NewDir: true})
	if err != nil {
		t.Fatalf("CreateEntry nested: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Elsewhere", Dir: "docs"}); err != nil {
		t.Fatalf("CreateEntry elsewhere: %v", err)
	}
	entries, err := c.ListEntries(t.Context(), p.ID, EntryFilter{Dir: "deployment"})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want the 2 entries under deployment/", entries)
	}
	ids := map[string]bool{entries[0].ID: true, entries[1].ID: true}
	if !ids[root.ID] || !ids[nested.ID] {
		t.Errorf("entries = %+v, want %q and %q", entries, root.Slug, nested.Slug)
	}
}

func diagnosticsOfKind(diagnostics []Diagnostic, kind string) []Diagnostic {
	var out []Diagnostic
	for _, f := range diagnostics {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestLintReportsDeepDirectories(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment/aws/runbooks"}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	diagnostics, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := diagnosticsOfKind(diagnostics, "deep_directory"); len(got) != 1 {
		t.Fatalf("diagnostics = %+v, want one deep_directory", diagnostics)
	}
}

func TestLintDoesNotReportDeepDirectoryAtDepthTwo(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment/aws"}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	diagnostics, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := diagnosticsOfKind(diagnostics, "deep_directory"); len(got) != 0 {
		t.Fatalf("diagnostics = %+v, want none", got)
	}
}

func TestLintReportsALongDirectoryName(t *testing.T) {
	c, p, _ := vaultCore(t)
	long := "a-directory-name-that-is-well-past-thirty-characters"
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "X", Dir: long}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	diagnostics, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	got := diagnosticsOfKind(diagnostics, "long_directory_name")
	if len(got) != 1 || got[0].Ref != long {
		t.Fatalf("diagnostics = %+v, want one naming %q", diagnostics, long)
	}
}

func TestLintReportsSimilarDirectories(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "A", Dir: "deploy"}); err != nil {
		t.Fatalf("CreateEntry a: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "B", Dir: "deployment", NewDir: true}); err != nil {
		t.Fatalf("CreateEntry b: %v", err)
	}
	diagnostics, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if got := diagnosticsOfKind(diagnostics, "similar_directory"); len(got) == 0 {
		t.Fatalf("diagnostics = %+v, want at least one similar_directory", diagnostics)
	}
}

func TestWikilinkToADirectoryPathResolves(t *testing.T) {
	c, p, _ := vaultCore(t)
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry target: %v", err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Runbook index",
		Body: "See [[deployment/rollback]] for the steps.\n"})
	if err != nil {
		t.Fatalf("CreateEntry entry: %v", err)
	}
	back, err := c.Backlinks(t.Context(), target.ID)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 || back[0].Title != entry.Title {
		t.Fatalf("Backlinks = %+v, want one from %q", back, entry.Title)
	}
}

func TestWikilinkBareLeafResolvesTheUniqueMatch(t *testing.T) {
	c, p, _ := vaultCore(t)
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry target: %v", err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Index", Body: "See [[rollback]].\n"})
	if err != nil {
		t.Fatalf("CreateEntry entry: %v", err)
	}
	back, err := c.Backlinks(t.Context(), target.ID)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 || back[0].Title != entry.Title {
		t.Fatalf("Backlinks = %+v, want one from %q", back, entry.Title)
	}
}

func TestWikilinkToAnAmbiguousLeafStaysAStubAndLintReportsIt(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "deployment"}); err != nil {
		t.Fatalf("CreateEntry a: %v", err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback", Dir: "docs"}); err != nil {
		t.Fatalf("CreateEntry b: %v", err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Index", Body: "See [[rollback]].\n"})
	if err != nil {
		t.Fatalf("CreateEntry entry: %v", err)
	}
	diagnostics, err := c.Lint(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	var found bool
	for _, f := range diagnostics {
		if f.Kind == "ambiguous_link" && f.Entry == entry.Ref {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v, want an ambiguous_link for %s", diagnostics, entry.Ref)
	}
}

func TestMoveEntryRewritesInboundWikilinks(t *testing.T) {
	c, p, _ := vaultCore(t)
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatalf("CreateEntry target: %v", err)
	}
	referrer, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Index",
		Body: "See [[rollback]] for the steps.\n"})
	if err != nil {
		t.Fatalf("CreateEntry referrer: %v", err)
	}
	moved, err := c.MoveEntry(t.Context(), p.ID, target.Slug, "deployment/rollback", false)
	if err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}
	raw, err := os.ReadFile(referrer.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[[deployment/rollback]]") {
		t.Fatalf("referrer body = %q, want the wikilink rewritten to the new path", raw)
	}
	back, err := c.Backlinks(t.Context(), moved.ID)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("Backlinks = %+v, want the link to survive the move", back)
	}
}

// Only links that resolve to the moved entry change, whatever form they
// take, and the referring entry is written the way any Trellis edit is.
func TestMoveEntryRewritesOnlyLinksToTheEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	target, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Rollback", Dir: "ops"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Other"}); err != nil {
		t.Fatal(err)
	}
	body := "See [[ops/rollback#steps|the steps]], [[/" + p.Key + "/vault/ops/rollback]] and [[rollback]].\n" +
		"Not [[other]], and not `[[ops/rollback]]` in code.\n"
	referrer, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Index", Body: body})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.MoveEntry(ctx, p.ID, target.Slug, "deploy/rollback-plan", false); err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}

	got, err := c.LoadEntry(ctx, p.ID, referrer.Slug)
	if err != nil {
		t.Fatal(err)
	}
	want := "See [[deploy/rollback-plan#steps|the steps]], [[/" + p.Key + "/vault/deploy/rollback-plan]] and [[deploy/rollback-plan]].\n" +
		"Not [[other]], and not `[[ops/rollback]]` in code.\n"
	if !strings.Contains(got.BodyMD, want) {
		t.Fatalf("body = %q, want it to contain %q", got.BodyMD, want)
	}
	if got.Version != referrer.Version+1 {
		t.Errorf("referrer version = %d, want %d", got.Version, referrer.Version+1)
	}
	revs, err := c.ListEntryRevisions(ctx, p.ID, referrer.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 {
		t.Error("no revision kept of the referrer's prior content")
	}
	diagnostics, err := c.Lint(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range diagnostics {
		if f.Kind == "stub" || f.Kind == "ambiguous_link" {
			t.Errorf("lint after the move: %+v", f)
		}
	}
}

// review-knowledge #6: a wikilink rewrite is a Trellis write like any
// other, and the spec's copy table calls for the new file to be captured
// after any Trellis write -- not only the version the rewrite replaced.
// Without it, a referrer edited directly right after a move loses the
// rewrite's own version: refreshFromFile jumps straight from N to N+2.
func TestMoveEntryCapturesTheReferrersRewrittenVersion(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	target, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatal(err)
	}
	referrer, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Index", Body: "[[rollback]]\n"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.MoveEntry(ctx, p.ID, target.Slug, "deploy/rollback", false); err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}

	moved, err := c.LoadEntry(ctx, p.ID, referrer.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Version != referrer.Version+1 {
		t.Fatalf("referrer version = %d, want %d", moved.Version, referrer.Version+1)
	}

	revs, err := c.ListEntryRevisions(ctx, p.ID, referrer.Slug)
	if err != nil {
		t.Fatal(err)
	}
	var gotNewVersion bool
	for _, r := range revs {
		if r.Version == moved.Version {
			gotNewVersion = true
		}
	}
	if !gotNewVersion {
		t.Fatalf("revisions = %+v, want version %d (the rewrite's own new version) retained", revs, moved.Version)
	}

	retained, err := os.ReadFile(revisionFilePath(moved.Path, moved.Version))
	if err != nil {
		t.Fatalf("reading the retained version: %v", err)
	}
	current, err := os.ReadFile(moved.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(retained) != string(current) {
		t.Errorf("retained version %d = %q, want it to match the rewritten file %q", moved.Version, retained, current)
	}
}

// A move that fails after rewriting a referrer puts the referrer back.
func TestMoveEntryFailureRestoresRewrittenReferrers(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process cannot write")
	}
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	target, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "First", Body: "[[rollback]]\n"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Second", Body: "[[rollback]]\n", Dir: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	lockedDir := filepath.Dir(second.Path)
	if err := os.Chmod(lockedDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o700) })

	if _, err := c.MoveEntry(ctx, p.ID, target.Slug, "deploy/rollback", false); err == nil {
		t.Fatal("MoveEntry succeeded; the locked referrer should have failed it")
	}

	if raw, err := os.ReadFile(first.Path); err != nil || string(raw) != string(firstRaw) {
		t.Errorf("first referrer = %q (%v), want it restored", raw, err)
	}
	if _, err := os.Stat(target.Path); err != nil {
		t.Errorf("the entry did not move back: %v", err)
	}
	back, err := c.LoadEntry(ctx, p.ID, target.Slug)
	if err != nil || back.Slug != "rollback" {
		t.Errorf("entry after the failed move = %+v, %v", back.Slug, err)
	}
}

// review-knowledge #4: once the closure sets done = true, a failure that
// only shows up when tx.Commit() itself is called -- an ambiguous outcome,
// not a statement error -- must undo a referrer's rewritten wikilink the
// same way a failure inside the closure already does. A trigger that fires
// on the entry's own row update inserts a row whose foreign key SQLite is
// told to check only at COMMIT (defer_foreign_keys), so the closure
// completes normally and only the commit fails.
func TestMoveEntryCommitFailureUndoesRewrittenReferrersToo(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	target, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Rollback"})
	if err != nil {
		t.Fatal(err)
	}
	referrer, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Index", Body: "[[rollback]]\n"})
	if err != nil {
		t.Fatal(err)
	}
	referrerRaw, err := os.ReadFile(referrer.Path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.db.Exec(`CREATE TABLE canary (id INTEGER PRIMARY KEY, target TEXT REFERENCES entry(id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`CREATE TRIGGER canary_trg AFTER UPDATE OF slug ON entry
		BEGIN INSERT INTO canary (target) VALUES ('does-not-exist'); END`); err != nil {
		t.Fatal(err)
	}
	// Deferred until COMMIT of the very next transaction (SQLite resets this
	// at the end of every transaction, so it must be set immediately before
	// the one call under test).
	if _, err := c.db.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}

	if _, err := c.MoveEntry(ctx, p.ID, target.Slug, "deploy/rollback", false); err == nil {
		t.Fatal("MoveEntry succeeded; the deferred foreign key violation should have failed its commit")
	}

	raw, err := os.ReadFile(referrer.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(referrerRaw) {
		t.Errorf("the referrer was not restored after the commit failure:\ngot  %q\nwant %q", raw, referrerRaw)
	}
	if _, err := os.Stat(target.Path); err != nil {
		t.Errorf("the entry did not move back: %v", err)
	}
	back, err := c.LoadEntry(ctx, p.ID, target.Slug)
	if err != nil || back.Slug != "rollback" {
		t.Errorf("entry after the failed commit = %+v, %v", back.Slug, err)
	}
}
