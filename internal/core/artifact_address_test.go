package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscalateRefusesASlugTheVaultHolds(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	mine, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Conventions", Body: "mine\n"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := c.CreateEntry(ctx, other.ID, NewEntry{Title: "Conventions", Body: "theirs\n"})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := c.EscalateKnowledge(ctx, p.ID, mine.Slug, "shared")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.EscalateKnowledge(ctx, other.ID, theirs.Slug, "also shared")
	if got := errCode(t, err); got != "global_slug_taken" {
		t.Fatalf("code = %s, want global_slug_taken", got)
	}
	if _, err := os.Stat(theirs.Path); err != nil {
		t.Errorf("the refused entry's file moved: %v", err)
	}
	raw, err := os.ReadFile(moved.Path)
	if err != nil || !strings.Contains(string(raw), "mine") {
		t.Errorf("the vault file was overwritten: %q, %v", raw, err)
	}
}

func TestEscalationBackfillsVaultStubs(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	other := seededProject2(t, c)
	if _, err := c.CreateEntry(ctx, other.ID, NewEntry{
		Title: "Notes", Body: "See [[/GLOBAL/vault/conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	if kinds, _ := lintKinds(t, c, other.ID); kinds["stub"] != 1 {
		t.Fatalf("before escalation: %v, want one stub", kinds)
	}
	target, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: "Conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EscalateKnowledge(ctx, p.ID, target.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if kinds, _ := lintKinds(t, c, other.ID); kinds["stub"] != 0 {
		t.Errorf("after escalation: %v, want the stub resolved", kinds)
	}
}

func screenShot(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "screen shot.png")
	if err := os.WriteFile(source, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestArtifactsAreFoundByIdNameAndAddress(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	a, err := c.CreateArtifact(ctx, p.ID, screenShot(t))
	if err != nil {
		t.Fatal(err)
	}
	const want = "/XPSCTL/artifacts/screen-shot.png"
	if a.Name != "screen-shot.png" || a.Ref != want {
		t.Fatalf("artifact = %+v", a)
	}
	items, err := c.ListArtifacts(ctx, p.ID, "", "")
	if err != nil || len(items) != 1 || items[0].Ref != want {
		t.Fatalf("ListArtifacts = %+v, %v", items, err)
	}
	for _, arg := range []string{a.ID, a.Name, a.Ref} {
		got, err := c.ResolveArtifact(ctx, p.ID, arg)
		if err != nil || got.ID != a.ID || got.Ref != want {
			t.Errorf("ResolveArtifact(%q) = %+v, %v", arg, got, err)
		}
	}
	other := seededProject2(t, c)
	for arg, code := range map[string]string{
		"missing.png":            "artifact_not_found",
		"/XPSCTL/cards/XPSCTL-1": "wrong_collection",
	} {
		_, err := c.ResolveArtifact(ctx, p.ID, arg)
		if got := errCode(t, err); got != code {
			t.Errorf("ResolveArtifact(%q): code = %s, want %s", arg, got, code)
		}
	}
	_, err = c.ResolveArtifact(ctx, other.ID, a.Ref)
	if got := errCode(t, err); got != "wrong_project" {
		t.Errorf("another project's address: code = %s", got)
	}
}

// A row can outlive its file, and its name is still its address.
func TestArtifactNamesStayUniqueWhenTheFileIsGone(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	source := screenShot(t)
	first, err := c.CreateArtifact(ctx, p.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}
	second, err := c.CreateArtifact(ctx, p.ID, source)
	if err != nil {
		t.Fatalf("second artifact: %v", err)
	}
	if second.Name != "screen-shot-2.png" {
		t.Errorf("second name = %q, want screen-shot-2.png", second.Name)
	}
}

func TestGraphNamesArtifactsByAddress(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "attach evidence"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.CreateArtifact(ctx, p.ID, screenShot(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LinkArtifactToCard(ctx, p.ID, card.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	g, err := c.Traverse(ctx, card.ID, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Type == "artifact" && n.Ref != a.Ref {
			t.Errorf("artifact node ref = %q, want %q", n.Ref, a.Ref)
		}
	}
}
