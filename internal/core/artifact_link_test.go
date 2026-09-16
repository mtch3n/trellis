package core

import (
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
	if err := os.WriteFile(path, []byte(RenderDoc(fm, body)), 0o600); err != nil {
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

// insertDuplicateArtifact adds a second row with a's name and a different path,
// which is what a storage root that moved between two `artifact add` calls
// leaves behind.
func insertDuplicateArtifact(t *testing.T, c *Core, projectID string, a Artifact) {
	t.Helper()
	if _, err := c.db.Exec(
		`INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NewCardID(), projectID, a.Name, filepath.Join(t.TempDir(), a.Name),
		a.Kind, a.MIME, a.Size, a.ContentHash, 1, 1); err != nil {
		t.Fatalf("insert duplicate: %v", err)
	}
}

func artifactNamesOf(doc Knowledge) []string {
	names := []string{}
	for _, r := range doc.Artifacts {
		names = append(names, r.Name)
	}
	return names
}

func TestEntryArtifactResolves(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "standup.mp3", "ID3 recording")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Standup"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
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
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Research"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, "not-yet.pdf")

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || !got.Artifacts[0].Missing || got.Artifacts[0].Name != "not-yet.pdf" {
		t.Fatalf("Artifacts = %+v, want one missing entry named not-yet.pdf", got.Artifacts)
	}
	if r := got.Artifacts[0]; r.Kind != "" || r.MIME != "" || r.Size != 0 {
		t.Errorf("stub = %+v, want it to carry nothing but its name", r)
	}
}

func TestRemovingANameFromTheFileRemovesTheLink(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "clip.mp3", "ID3 clip")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Clip"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	setArtifactsInFile(t, doc.Path)
	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 0 {
		t.Errorf("Artifacts = %+v after the name was removed from the file", got.Artifacts)
	}
	var rows int
	if err := c.db.Get(&rows,
		`SELECT COUNT(*) FROM link WHERE from_id = ? AND rel = 'artifact'`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("%d artifact link rows remain, want 0", rows)
	}
}

func TestANameSharedByTwoArtifactsIsAStub(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "diagram.png", "\x89PNG\r\n\x1a\nfirst")
	insertDuplicateArtifact(t, c, p.ID, a)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Design"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || !got.Artifacts[0].Missing {
		t.Errorf("Artifacts = %+v, want an ambiguous name left unresolved", got.Artifacts)
	}
}

// An artifact's name keeps its extension's case, so a lower-cased lookup would
// never find "photo.PNG". The list also keeps the file's order, and a name
// listed twice links once.
func TestArtifactNamesKeepCaseAndOrder(t *testing.T) {
	c, p, _ := kbCore(t)
	upper := addArtifact(t, c, p.ID, "Photo.PNG", "\x89PNG\r\n\x1a\nx")
	notes := addArtifact(t, c, p.ID, "notes.txt", "plain")
	if upper.Name != "photo.PNG" {
		t.Fatalf("setup: name = %q; this test relies on the extension keeping its case", upper.Name)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Album"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, notes.Name, upper.Name, notes.Name)

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
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
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "report.pdf", "%PDF-1.7\n")
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Report"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	setArtifactsInFile(t, doc.Path, a.Name)
	if _, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug); err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}

	if _, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, "a new body\n", nil); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}
	if got := artifactsInFile(t, doc.Path); !slices.Equal(got, []string{a.Name}) {
		t.Errorf("file lists %v after a body edit, want [%s]", got, a.Name)
	}
	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Missing {
		t.Errorf("Artifacts = %+v after a body edit", got.Artifacts)
	}
}
