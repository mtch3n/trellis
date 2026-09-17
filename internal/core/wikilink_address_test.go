package core

import (
	"strings"
	"testing"
)

func lintKinds(t *testing.T, c *Core, projectID string) (map[string]int, []LintFinding) {
	t.Helper()
	findings, err := c.Lint(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, f := range findings {
		kinds[f.Kind]++
	}
	return kinds, findings
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
	if _, findings := lintKinds(t, c, p.ID); len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
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
	_, findings := lintKinds(t, c, p.ID)
	if len(findings) != 1 || findings[0].Kind != "stub" ||
		!strings.Contains(findings[0].Fix, "trellis --project OTHERPROJ knowledge new") {
		t.Fatalf("findings = %+v, want one stub pointing at OTHERPROJ", findings)
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
	_, findings := lintKinds(t, c, p.ID)
	if len(findings) != 1 || findings[0].Kind != "stub" || findings[0].Ref != "/NOPE/vault/x" {
		t.Errorf("findings = %+v", findings)
	}
	if findings[0].Entry != "/XPSCTL/vault/setup" {
		t.Errorf("finding names its entry as %q, want its address", findings[0].Entry)
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
	if _, err := c.EscalateKnowledge(ctx, other.ID, shared.Slug, "shared"); err != nil {
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
	if kinds, findings := lintKinds(t, c, p.ID); kinds["stub"] != 0 {
		t.Errorf("findings = %+v, want the link resolved", findings)
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
	_, findings := lintKinds(t, c, p.ID)
	var stubs []string
	for _, f := range findings {
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
	kinds, findings := lintKinds(t, c, p.ID)
	if kinds["wrong_collection"] != 1 || kinds["bad_path"] != 1 || kinds["stub"] != 0 {
		t.Errorf("findings = %+v", findings)
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
	if kinds, findings := lintKinds(t, c, p.ID); kinds["broken_anchor"] != 0 {
		t.Errorf("findings = %+v", findings)
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
	if _, err := c.EscalateKnowledge(ctx, other.ID, shared.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Setup", Body: "" +
		"Foreign [[/OTHERPROJ/vault/runbook#rollback]] and [[/OTHERPROJ/vault/runbook#nope]].\n" +
		"Vault [[/GLOBAL/vault/conventions#naming]] and [[conventions#missing]].\n"}); err != nil {
		t.Fatal(err)
	}
	_, findings := lintKinds(t, c, p.ID)
	broken := map[string]string{}
	for _, f := range findings {
		if f.Kind == "broken_anchor" {
			broken[f.Ref] = f.Fix
		}
	}
	if len(broken) != 2 {
		t.Fatalf("broken anchors = %v, want two; findings: %+v", broken, findings)
	}
	if fix := broken["/OTHERPROJ/vault/runbook#nope"]; !strings.Contains(fix, "trellis knowledge show /OTHERPROJ/vault/runbook") {
		t.Errorf("foreign fix = %q", fix)
	}
	if fix := broken["conventions#missing"]; !strings.Contains(fix, "trellis knowledge show /GLOBAL/vault/conventions") {
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
		t.Fatalf("LinkCardToDoc: %v", err)
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
