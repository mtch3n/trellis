package core

import (
	"path/filepath"
	"slices"
	"strings"
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

// A chain of merges, or a link written outside Trellis, can leave a card
// linked straight to both SRC's artifact and DST's byte-identical copy. Once
// the collapse re-points SRC's side at DST's id, the two rows must not
// survive as duplicates.
func TestMergeCollapsedArtifactDoesNotDuplicateACardLink(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	card := f.card(f.api, f.apiBoard, "has files", nil, nil)
	logo := f.artifact(f.api, "logo.png", "same logo")
	monoLogo := f.artifact(f.mono, "logo.png", "same logo")
	if err := f.c.LinkArtifactToCard(ctx, f.api.ID, card.ID, logo.ID); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('card', ?, 'artifact', ?, ?, 'artifact')`,
		card.ID, monoLogo.ID, monoLogo.ID)

	f.merge(MergeOptions{Apply: true})

	items, err := f.c.ListArtifacts(ctx, f.mono.ID, card.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != monoLogo.ID {
		t.Fatalf("card's artifacts after the merge = %+v, want exactly one link to %s", items, monoLogo.ID)
	}
	if n := f.count(`SELECT count(*) FROM link
		WHERE from_type = 'card' AND from_id = ? AND to_type = 'artifact' AND to_id = ?`,
		card.ID, monoLogo.ID); n != 1 {
		t.Errorf("card->artifact link rows = %d, want 1", n)
	}
}

// An entry names its artifacts by name (entry_relations.go), so collapsing the
// artifact it names must leave that name in place, not the id underneath it.
func TestMergeCollapsedArtifactKeepsAnEntryLinksName(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	logo := f.artifact(f.api, "logo.png", "same logo")
	monoLogo := f.artifact(f.mono, "logo.png", "same logo")
	entry := f.entry(f.api, "Design", "design doc\n")
	if _, err := f.c.LinkArtifactToEntry(ctx, f.api.ID, entry.Slug, logo.ID); err != nil {
		t.Fatal(err)
	}

	f.merge(MergeOptions{Apply: true})

	var toRaw string
	if err := f.c.db.Get(&toRaw,
		`SELECT to_raw FROM link WHERE from_type = 'entry' AND to_type = 'artifact' AND to_id = ?`, monoLogo.ID); err != nil {
		t.Fatal(err)
	}
	if toRaw != "logo.png" {
		t.Errorf("entry link to_raw = %q, want the artifact's name", toRaw)
	}
	items, err := f.c.ListArtifacts(ctx, f.mono.ID, "", entry.ID)
	if err != nil || len(items) != 1 || items[0].ID != monoLogo.ID {
		t.Errorf("entry's artifacts after the merge = %+v, %v", items, err)
	}
}

// A SRC entry naming a renamed artifact must follow it: left alone, the
// old name resolves, after the merge, to whatever DST already has under it --
// a different file with the same name, never the one the entry meant.
func TestMergeRenamedArtifactRewritesTheEntryThatNamesIt(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.artifact(f.mono, "shot.png", "mono pixels")
	shot := f.artifact(f.api, "shot.png", "api pixels")
	entry := f.entry(f.api, "Design", "design doc\n")
	if _, err := f.c.LinkArtifactToEntry(ctx, f.api.ID, entry.Slug, shot.ID); err != nil {
		t.Fatal(err)
	}

	plan := f.merge(MergeOptions{Apply: true, RenameConflicts: true})

	if !slices.Equal(plan.Artifacts.Renamed, []Rename{{From: "shot.png", To: "shot-api.png"}}) {
		t.Fatalf("renamed = %+v", plan.Artifacts.Renamed)
	}
	moved, err := f.c.ReadEntry(ctx, f.mono.ID, "design")
	if err != nil {
		t.Fatal(err)
	}
	raw := readFile(t, moved.Path)
	if !strings.Contains(raw, "shot-api.png") || strings.Contains(raw, "\n- shot.png\n") {
		t.Errorf("design's frontmatter after the merge:\n%s", raw)
	}
	items, err := f.c.ListArtifacts(ctx, f.mono.ID, "", moved.ID)
	if err != nil || len(items) != 1 || items[0].ID != shot.ID || items[0].Name != "shot-api.png" {
		t.Errorf("design's artifacts after the merge = %+v, %v", items, err)
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
