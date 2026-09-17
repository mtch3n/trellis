package core

import (
	"strings"
	"testing"
)

func lintKinds(t *testing.T, c *Core, projectID string) (map[string]int, []Diagnostic) {
	t.Helper()
	diagnostics, err := c.Lint(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, f := range diagnostics {
		kinds[f.Kind]++
	}
	return kinds, diagnostics
}

func TestWikilinkResolvesInAnotherProject(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	target, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Runbook"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Setup", Body: "Follow [[/OTHERPROJ/vault/runbook]].\n"}); err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/XPSCTL/vault/setup" {
		t.Errorf("backlinks = %+v", back)
	}
	if _, diagnostics := lintKinds(t, c, p.ID); len(diagnostics) != 0 {
		t.Errorf("diagnostics = %+v, want none", diagnostics)
	}
}

func TestCrossProjectStubIsBackfilled(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Setup", Body: "Follow [[/OTHERPROJ/vault/later]].\n"}); err != nil {
		t.Fatal(err)
	}
	_, diagnostics := lintKinds(t, c, p.ID)
	if len(diagnostics) != 1 || diagnostics[0].Kind != "stub" ||
		!strings.Contains(diagnostics[0].Fix, "trellis --project OTHERPROJ vault new") {
		t.Fatalf("diagnostics = %+v, want one stub pointing at OTHERPROJ", diagnostics)
	}
	later, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Later"})
	if err != nil {
		t.Fatal(err)
	}
	if back, _ := c.Backlinks(ctx, later.ID); len(back) != 1 {
		t.Errorf("backlinks after the target was written = %+v", back)
	}
	if kinds, _ := lintKinds(t, c, p.ID); kinds["stub"] != 0 {
		t.Errorf("the stub was not backfilled: %v", kinds)
	}
}

func TestLinkToAProjectThatDoesNotExistIsAStub(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Setup", Body: "See [[/NOPE/vault/x]].\n"}); err != nil {
		t.Fatalf("writing a link to a missing project must not fail: %v", err)
	}
	_, diagnostics := lintKinds(t, c, p.ID)
	if len(diagnostics) != 1 || diagnostics[0].Kind != "stub" || diagnostics[0].Ref != "/NOPE/vault/x" {
		t.Errorf("diagnostics = %+v", diagnostics)
	}
	if diagnostics[0].Entry != "/XPSCTL/vault/setup" {
		t.Errorf("diagnostic names its entry as %q, want its address", diagnostics[0].Entry)
	}
}

// A relative link means this project, then the vault, as a relative
// argument does.
func TestARelativeLinkFallsBackToTheVault(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	shared, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromoteEntry(ctx, other.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Setup", Body: "Follow [[conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	back, err := c.Backlinks(ctx, shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/XPSCTL/vault/setup" {
		t.Errorf("vault backlinks = %+v", back)
	}
	if kinds, diagnostics := lintKinds(t, c, p.ID); kinds["stub"] != 0 {
		t.Errorf("diagnostics = %+v, want the link resolved", diagnostics)
	}

	// The project's own entry still wins over the vault's.
	own, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Onboarding", Body: "Read [[conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	back, err = c.Backlinks(ctx, own.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Ref != "/XPSCTL/vault/onboarding" {
		t.Errorf("own backlinks = %+v, want the project entry to win", back)
	}
}

func TestTheOldQualifiedFormIsARelativeStub(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Design"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Setup", Body: "See [[XPSCTL/design]].\n"}); err != nil {
		t.Fatal(err)
	}
	_, diagnostics := lintKinds(t, c, p.ID)
	var stubs []string
	for _, f := range diagnostics {
		if f.Kind == "stub" {
			stubs = append(stubs, f.Ref)
		}
	}
	if len(stubs) != 1 || stubs[0] != "XPSCTL/design" {
		t.Errorf("stubs = %v, want the old form reported", stubs)
	}
}

func TestLintNamesAddressesThatNameNoEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Setup", Body: "A card: [[/XPSCTL/cards/XPSCTL-1]]. Broken: [[/xps_ctl/vault/x]].\n"}); err != nil {
		t.Fatal(err)
	}
	kinds, diagnostics := lintKinds(t, c, p.ID)
	if kinds["wrong_collection"] != 1 || kinds["bad_path"] != 1 || kinds["stub"] != 0 {
		t.Errorf("diagnostics = %+v", diagnostics)
	}
}

// An anchor is checked on the entry the link resolved to. Checking by slug
// compared another project's entry with this project's namesake.
func TestAnchorsAreCheckedOnTheLinkedEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Runbook", Body: "## Steps\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Runbook", Body: "## Rollback\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Setup",
		Body: "Local [[runbook#steps]], remote [[/OTHERPROJ/vault/runbook#rollback]].\n"}); err != nil {
		t.Fatal(err)
	}
	if kinds, diagnostics := lintKinds(t, c, p.ID); kinds["broken_anchor"] != 0 {
		t.Errorf("diagnostics = %+v", diagnostics)
	}
}

// A missing heading is found in a target this project does not own: lint
// reads that entry's file rather than trusting its own listing.
func TestMissingHeadingsInForeignAndVaultTargets(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Runbook", Body: "## Rollback\n"}); err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Conventions", Body: "## Naming\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromoteEntry(ctx, other.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Setup", Body: "" +
		"Foreign [[/OTHERPROJ/vault/runbook#rollback]] and [[/OTHERPROJ/vault/runbook#nope]].\n" +
		"Vault [[/GLOBAL/vault/conventions#naming]] and [[conventions#missing]].\n"}); err != nil {
		t.Fatal(err)
	}
	_, diagnostics := lintKinds(t, c, p.ID)
	broken := map[string]string{}
	for _, f := range diagnostics {
		if f.Kind == "broken_anchor" {
			broken[f.Ref] = f.Fix
		}
	}
	if len(broken) != 2 {
		t.Fatalf("broken anchors = %v, want two; diagnostics: %+v", broken, diagnostics)
	}
	if fix := broken["/OTHERPROJ/vault/runbook#nope"]; !strings.Contains(fix, "trellis vault show /OTHERPROJ/vault/runbook") {
		t.Errorf("foreign fix = %q", fix)
	}
	if fix := broken["conventions#missing"]; !strings.Contains(fix, "trellis vault show /GLOBAL/vault/conventions") {
		t.Errorf("vault fix = %q", fix)
	}
}

func TestLinkCardToEntryAcrossProjects(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Roll back"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Runbook", Body: "## Rollback\n"})
	if err != nil {
		t.Fatal(err)
	}
	ref := CardRef{Seq: card.Seq}
	if err := c.LinkCardToEntry(ctx, p.ID, ref, "/OTHERPROJ/vault/runbook#rollback"); err != nil {
		t.Fatalf("LinkCardToEntry: %v", err)
	}
	back, err := c.Backlinks(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].FromType != "card" || back[0].Ref != card.Ref || back[0].Anchor != "rollback" {
		t.Errorf("backlinks = %+v", back)
	}
	err = c.LinkCardToEntry(ctx, p.ID, ref, "/XPSCTL/cards/XPSCTL-1")
	if got := errCode(t, err); got != "wrong_collection" {
		t.Errorf("card address: code = %s", got)
	}
	err = c.LinkCardToEntry(ctx, p.ID, ref, "/OTHERPROJ/vault/missing")
	if got := errCode(t, err); got != "knowledge_not_found" {
		t.Errorf("missing entry: code = %s", got)
	}
}
