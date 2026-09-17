package core

import (
	"path/filepath"
	"slices"
	"testing"
)

func (f *mergeFixture) artifact(p Project, name, content string) Artifact {
	f.t.Helper()
	src := filepath.Join(f.t.TempDir(), name)
	writeFile(f.t, src, content)
	a, err := f.c.CreateArtifact(f.t.Context(), p.ID, src)
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

func TestMergeMovesAndCollapsesArtifacts(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	card := f.card(f.api, f.apiBoard, "has files", nil, nil)
	shot := f.artifact(f.api, "shot.png", "api pixels")
	logo := f.artifact(f.api, "logo.png", "same logo")
	monoLogo := f.artifact(f.mono, "logo.png", "same logo")
	for _, a := range []Artifact{shot, logo} {
		if err := f.c.LinkArtifactToCard(ctx, f.api.ID, card.ID, a.ID); err != nil {
			t.Fatal(err)
		}
	}

	plan := f.merge(MergeOptions{Apply: true})

	if plan.Artifacts.Moved != 1 || !slices.Equal(plan.Artifacts.Collapsed, []string{"logo.png"}) {
		t.Errorf("artifacts = %+v", plan.Artifacts)
	}
	got, err := f.c.ResolveArtifact(ctx, f.mono.ID, "shot.png")
	if err != nil || got.ID != shot.ID || got.Ref != "/MONO/artifacts/shot.png" ||
		got.Path != filepath.Join(f.root, "projects", "MONO", "artifacts", "shot.png") {
		t.Fatalf("shot.png = %+v, %v", got, err)
	}
	if readFile(t, got.Path) != "api pixels" {
		t.Error("the moved file lost its content")
	}
	items, err := f.c.ListArtifacts(ctx, f.mono.ID, card.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range items {
		ids = append(ids, a.ID)
	}
	slices.Sort(ids)
	want := []string{shot.ID, monoLogo.ID}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("the card's artifacts = %v, want %v", ids, want)
	}
	if n := f.count(`SELECT count(*) FROM artifact WHERE id = ?`, logo.ID); n != 0 {
		t.Error("the collapsed artifact's row survived")
	}
}

func TestMergeArtifactConflicts(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.artifact(f.api, "shot.png", "api")
	f.artifact(f.mono, "shot.png", "mono")

	plan := f.merge(MergeOptions{})
	if plan.Ready || len(plan.Artifacts.Conflicts) != 1 || plan.Artifacts.Conflicts[0].Name != "shot.png" ||
		plan.Artifacts.Conflicts[0].Vault {
		t.Fatalf("plan = %+v", plan.Artifacts)
	}

	plan = f.merge(MergeOptions{Apply: true, RenameConflicts: true})
	if !slices.Equal(plan.Artifacts.Renamed, []Rename{{From: "shot.png", To: "shot-api.png"}}) {
		t.Errorf("renamed = %+v", plan.Artifacts.Renamed)
	}
	for name, content := range map[string]string{"shot-api.png": "api", "shot.png": "mono"} {
		a, err := f.c.ResolveArtifact(ctx, f.mono.ID, name)
		if err != nil || readFile(t, a.Path) != content {
			t.Errorf("%s = %+v, %v", name, a, err)
		}
	}
}

func TestFreeArtifactNameKeepsTheExtension(t *testing.T) {
	taken := map[string]bool{"shot-api.png": true}
	if got := freeArtifactName("shot.png", "api", taken); got != "shot-api-2.png" {
		t.Errorf("got %s", got)
	}
	if got := freeArtifactName("README", "api", nil); got != "README-api" {
		t.Errorf("got %s", got)
	}
}
