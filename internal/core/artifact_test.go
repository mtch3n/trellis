package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/store"
)

func TestArtifactStoresBytesOnDiskAndLinksToCard(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := New(db, FixedClock{MS: 1_757_000_000_000}, "artifact-test").WithKBRoot(t.TempDir())
	project, err := c.CreateProject(t.Context(), "ARTIFACT", false)
	if err != nil {
		t.Fatal(err)
	}
	board, err := c.SelectBoard(t.Context(), project.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(t.Context(), project.ID, board.ID, NewCard{Title: "attach evidence"})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "screen shot.png")
	if err := os.WriteFile(source, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := c.CreateArtifact(t.Context(), project.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Path == source {
		t.Fatal("artifact was not copied into Trellis storage")
	}
	if _, err := os.Stat(artifact.Path); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := db.Get(&body, "SELECT COALESCE((SELECT body_md FROM comment LIMIT 1), '')"); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		t.Fatal("artifact bytes unexpectedly stored in the database")
	}
	if err := c.LinkArtifactToCard(t.Context(), project.ID, card.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	items, err := c.ListArtifacts(t.Context(), project.ID, card.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != artifact.ID {
		t.Fatalf("linked artifacts = %+v", items)
	}
}
