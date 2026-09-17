package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// addArtifact stores a file as an artifact of the project, the way
// `trellis artifact add` does.
func addArtifact(t *testing.T, c *Core, projectID, filename, content string) Artifact {
	t.Helper()
	src := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := c.CreateArtifact(t.Context(), projectID, src)
	if err != nil {
		t.Fatalf("CreateArtifact %s: %v", filename, err)
	}
	return a
}

// setArtifactsInFile rewrites an entry's `artifacts` list the way someone
// editing the file directly would.
func setArtifactsInFile(t *testing.T, path string, names ...string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fm, body, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	fm.Artifacts = names
	if err := os.WriteFile(path, []byte(RenderEntry(fm, body)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// artifactsInFile reads the list straight from the file, not from the database.
func artifactsInFile(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return fm.Artifacts
}

func artifactNamesOf(entry Entry) []string {
	names := []string{}
	for _, r := range entry.Artifacts {
		names = append(names, r.Name)
	}
	return names
}

func TestEntryArtifactResolves(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "standup.mp3", "ID3 recording")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Standup"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, a.Name)

	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("Artifacts = %+v, want one", got.Artifacts)
	}
	ref := got.Artifacts[0]
	if ref.Name != a.Name || ref.Missing || ref.Kind != a.Kind || ref.MIME != a.MIME || ref.Size != a.Size {
		t.Errorf("ref = %+v, want it to describe the stored artifact %+v", ref, a)
	}
}

func TestEntryArtifactThatDoesNotExistIsAStub(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Research"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, "not-yet.pdf")

	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if len(got.Artifacts) != 1 || !got.Artifacts[0].Missing || got.Artifacts[0].Name != "not-yet.pdf" {
		t.Fatalf("Artifacts = %+v, want one missing entry named not-yet.pdf", got.Artifacts)
	}
	if r := got.Artifacts[0]; r.Kind != "" || r.MIME != "" || r.Size != 0 {
		t.Errorf("stub = %+v, want it to carry nothing but its name", r)
	}
}

func TestRemovingANameFromTheFileRemovesTheLink(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "clip.mp3", "ID3 clip")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Clip"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, a.Name)
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}

	setArtifactsInFile(t, entry.Path)
	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if len(got.Artifacts) != 0 {
		t.Errorf("Artifacts = %+v after the name was removed from the file", got.Artifacts)
	}
	var rows int
	if err := c.db.Get(&rows,
		`SELECT COUNT(*) FROM link WHERE from_id = ? AND rel = 'artifact'`, entry.ID); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("%d artifact link rows remain, want 0", rows)
	}
}

// An artifact's name keeps its extension's case, so a lower-cased lookup would
// never find "photo.PNG". The list also keeps the file's order, and a name
// listed twice links once.
func TestArtifactNamesKeepCaseAndOrder(t *testing.T) {
	c, p, _ := vaultCore(t)
	upper := addArtifact(t, c, p.ID, "Photo.PNG", "\x89PNG\r\n\x1a\nx")
	notes := addArtifact(t, c, p.ID, "notes.txt", "plain")
	if upper.Name != "photo.PNG" {
		t.Fatalf("setup: name = %q; this test relies on the extension keeping its case", upper.Name)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Album"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, notes.Name, upper.Name, notes.Name)

	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if names := artifactNamesOf(got); !slices.Equal(names, []string{notes.Name, upper.Name}) {
		t.Errorf("names = %v, want %v", names, []string{notes.Name, upper.Name})
	}
	for _, r := range got.Artifacts {
		if r.Missing {
			t.Errorf("%s did not resolve", r.Name)
		}
	}
}

func TestEditingTheBodyKeepsTheArtifactList(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "report.pdf", "%PDF-1.7\n")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Report"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, a.Name)
	loaded, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}

	if _, err := c.EditEntry(t.Context(), p.ID, entry.Slug, "a new body\n", &loaded.Version); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	if got := artifactsInFile(t, entry.Path); !slices.Equal(got, []string{a.Name}) {
		t.Errorf("file lists %v after a body edit, want [%s]", got, a.Name)
	}
	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Missing {
		t.Errorf("Artifacts = %+v after a body edit", got.Artifacts)
	}
}

// A row can claim a name whose file was removed by hand outside Trellis, or
// whose creation never finished; a new artifact must not reuse the name.
func TestANameTakenInTheDatabaseIsNotReused(t *testing.T) {
	c, p, _ := vaultCore(t)
	if _, err := c.db.Exec(
		`INSERT INTO artifact (id, project_id, name, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, 'x.png', 'image', 'image/png', 3, 'h', 1, 1)`,
		NewID(), p.ID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	a := addArtifact(t, c, p.ID, "x.png", "\x89PNG\r\n\x1a\nx")
	if a.Name != "x-2.png" {
		t.Errorf("name = %q, want x-2.png: x.png is already a name in this project", a.Name)
	}
}

func TestCreatingAnArtifactResolvesAnEarlierStub(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Later"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, "later.pdf")
	// This read writes the stub row. Without it there would be nothing to
	// backfill, and the resync on the next read would hide a missing backfill.
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}

	a := addArtifact(t, c, p.ID, "later.pdf", "%PDF-1.7\n")

	// The file has not changed, so this read does not resync: the resolution
	// can only have come from the backfill.
	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Missing || got.Artifacts[0].Kind != a.Kind {
		t.Errorf("Artifacts = %+v, want later.pdf resolved by the backfill", got.Artifacts)
	}
}

func TestDeletingAnArtifactLeavesEntryLinksAsStubs(t *testing.T) {
	c, p, b := vaultCore(t)
	a := addArtifact(t, c, p.ID, "evidence.png", "\x89PNG\r\n\x1a\nx")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Evidence"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, a.Name)
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "evidence"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := c.LinkArtifactToCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Fatalf("LinkArtifactToCard: %v", err)
	}

	if err := c.DeleteArtifact(t.Context(), p.ID, a.ID); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}

	got, err := c.LoadEntry(t.Context(), p.ID, entry.Slug)
	if err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if len(got.Artifacts) != 1 || !got.Artifacts[0].Missing || got.Artifacts[0].Name != a.Name {
		t.Errorf("Artifacts = %+v, want the entry's link kept as a stub", got.Artifacts)
	}
	var cardLinks int
	if err := c.db.Get(&cardLinks,
		`SELECT COUNT(*) FROM link WHERE from_type = 'card' AND from_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if cardLinks != 0 {
		t.Errorf("%d card links remain, want 0: a card's link lives only in the database", cardLinks)
	}
}

// Unlinking by the artifact's id, not its name, still removes it from the
// entry's list.
func TestUnlinkArtifactFromEntryByID(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "a.png", "\x89PNG\r\n\x1a\na")
	b := addArtifact(t, c, p.ID, "b.png", "\x89PNG\r\n\x1a\nb")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "ByID"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, a.Name, b.Name)
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}

	got, err := c.UnlinkArtifactFromEntry(t.Context(), p.ID, entry.Slug, a.ID)
	if err != nil {
		t.Fatalf("UnlinkArtifactFromEntry: %v", err)
	}
	if names := artifactNamesOf(got); !slices.Equal(names, []string{b.Name}) {
		t.Errorf("names = %v, want [%s]", names, b.Name)
	}
	if file := artifactsInFile(t, entry.Path); !slices.Equal(file, []string{b.Name}) {
		t.Errorf("file lists %v, want [%s]", file, b.Name)
	}
}

func artifactErrCode(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}

func TestResolveArtifactByIDOrName(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "map.png", "\x89PNG\r\n\x1a\nx")

	byID, err := c.ResolveArtifact(t.Context(), p.ID, a.ID)
	if err != nil || byID.ID != a.ID {
		t.Errorf("by id = %+v, %v", byID, err)
	}
	byName, err := c.ResolveArtifact(t.Context(), p.ID, a.Name)
	if err != nil || byName.ID != a.ID {
		t.Errorf("by name = %+v, %v", byName, err)
	}
	if _, err := c.ResolveArtifact(t.Context(), p.ID, "nope.png"); artifactErrCode(err) != "artifact_not_found" {
		t.Errorf("unknown: err = %v, want artifact_not_found", err)
	}
}

func TestLinkArtifactToEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "meeting.mp3", "ID3 meeting")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Meeting"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	got, err := c.LinkArtifactToEntry(t.Context(), p.ID, entry.Slug, a.Name)
	if err != nil {
		t.Fatalf("LinkArtifactToEntry: %v", err)
	}
	if names := artifactNamesOf(got); !slices.Equal(names, []string{a.Name}) || got.Artifacts[0].Missing {
		t.Errorf("Artifacts = %+v", got.Artifacts)
	}
	if file := artifactsInFile(t, entry.Path); !slices.Equal(file, []string{a.Name}) {
		t.Errorf("file lists %v, want [%s]", file, a.Name)
	}
	var logged int
	if err := c.db.Get(&logged,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND action = 'artifact_linked' AND new_value = ?`,
		entry.ID, a.Name); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Errorf("%d artifact_linked events, want 1", logged)
	}

	again, err := c.LinkArtifactToEntry(t.Context(), p.ID, entry.Slug, a.ID)
	if err != nil {
		t.Fatalf("LinkArtifactToEntry again: %v", err)
	}
	if again.Version != got.Version {
		t.Errorf("version %d -> %d: linking an already-listed artifact must not rewrite the file",
			got.Version, again.Version)
	}
}

// A database restored from an older backup can lack an entry's link rows while
// the file still lists the names. Linking must read the list from the file;
// rewriting it from the lagging rows would drop names.
func TestLinkKeepsNamesTheDatabaseHasLost(t *testing.T) {
	c, p, _ := vaultCore(t)
	first := addArtifact(t, c, p.ID, "first.png", "\x89PNG\r\n\x1a\n1")
	second := addArtifact(t, c, p.ID, "second.png", "\x89PNG\r\n\x1a\n2")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Pair"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, first.Name)
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}
	if _, err := c.db.Exec(`DELETE FROM link WHERE from_id = ? AND rel = 'artifact'`, entry.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := c.LinkArtifactToEntry(t.Context(), p.ID, entry.Slug, second.Name); err != nil {
		t.Fatalf("LinkArtifactToEntry: %v", err)
	}
	if file := artifactsInFile(t, entry.Path); !slices.Equal(file, []string{first.Name, second.Name}) {
		t.Errorf("file lists %v, want [%s %s]", file, first.Name, second.Name)
	}
}

func TestUnlinkArtifactFromEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "a.png", "\x89PNG\r\n\x1a\na")
	b := addArtifact(t, c, p.ID, "b.png", "\x89PNG\r\n\x1a\nb")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Two"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, a.Name, b.Name)
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}

	got, err := c.UnlinkArtifactFromEntry(t.Context(), p.ID, entry.Slug, a.Name)
	if err != nil {
		t.Fatalf("UnlinkArtifactFromEntry: %v", err)
	}
	if names := artifactNamesOf(got); !slices.Equal(names, []string{b.Name}) {
		t.Errorf("names = %v, want [%s]", names, b.Name)
	}
	if file := artifactsInFile(t, entry.Path); !slices.Equal(file, []string{b.Name}) {
		t.Errorf("file lists %v, want [%s]", file, b.Name)
	}
	var logged int
	if err := c.db.Get(&logged,
		`SELECT COUNT(*) FROM event WHERE entity_id = ? AND action = 'artifact_unlinked' AND new_value = ?`,
		entry.ID, a.Name); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Errorf("%d artifact_unlinked events, want 1", logged)
	}

	again, err := c.UnlinkArtifactFromEntry(t.Context(), p.ID, entry.Slug, a.Name)
	if err != nil {
		t.Fatalf("UnlinkArtifactFromEntry again: %v", err)
	}
	if again.Version != got.Version {
		t.Errorf("unlinking a name that is not listed rewrote the file")
	}
}

// A stub must be clearable: its artifact no longer exists, so the name cannot
// be resolved, and unlinking must still remove it from the file.
func TestUnlinkClearsANameWhoseArtifactIsGone(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Gone"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, "deleted.pdf")
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}

	got, err := c.UnlinkArtifactFromEntry(t.Context(), p.ID, entry.Slug, "deleted.pdf")
	if err != nil {
		t.Fatalf("UnlinkArtifactFromEntry: %v", err)
	}
	if len(got.Artifacts) != 0 || len(artifactsInFile(t, entry.Path)) != 0 {
		t.Errorf("stub not cleared: Artifacts = %+v, file = %v", got.Artifacts, artifactsInFile(t, entry.Path))
	}
}

func TestUnlinkArtifactFromCard(t *testing.T) {
	c, p, b := vaultCore(t)
	a := addArtifact(t, c, p.ID, "shot.png", "\x89PNG\r\n\x1a\nx")
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "evidence"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := c.LinkArtifactToCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Fatalf("LinkArtifactToCard: %v", err)
	}

	if err := c.UnlinkArtifactFromCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Fatalf("UnlinkArtifactFromCard: %v", err)
	}
	items, err := c.ListArtifacts(t.Context(), p.ID, card.ID, "")
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("card still lists %d artifacts", len(items))
	}
	if err := c.UnlinkArtifactFromCard(t.Context(), p.ID, card.ID, a.ID); err != nil {
		t.Errorf("unlinking again: %v, want no error", err)
	}
}

func TestListArtifactsForAnEntry(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "one.png", "\x89PNG\r\n\x1a\n1")
	b := addArtifact(t, c, p.ID, "two.png", "\x89PNG\r\n\x1a\n2")
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Listed"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	setArtifactsInFile(t, entry.Path, b.Name, a.Name)
	if _, err := c.LoadEntry(t.Context(), p.ID, entry.Slug); err != nil {
		t.Fatalf("LoadEntry: %v", err)
	}

	items, err := c.ListArtifacts(t.Context(), p.ID, "", entry.ID)
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if !slices.Equal(names, []string{b.Name, a.Name}) {
		t.Errorf("names = %v, want [%s %s]", names, b.Name, a.Name)
	}
}
