package core

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/store"
)

func TestArtifactStoresBytesOnDiskAndLinksToCard(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := New(db, FixedClock{MS: 1_757_000_000_000}, "artifact-test", t.TempDir())
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

// review-knowledge #13: the naming loop broke only on os.ErrNotExist; any
// other os.Stat error -- EACCES, most realistically, from an artifact
// directory without search permission -- made it spin forever inside
// Core.Tx, holding SQLite's write lock for the whole process.
// artifactNameTaken already existed to handle exactly this and was never
// wired into the loop.
func TestCreateArtifactStopsOnAStatErrorInsteadOfLoopingForever(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process cannot search")
	}
	c, p, _ := kbCore(t)
	source := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(source, []byte("some text"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := c.artifactDir(p.Key)
	if err != nil {
		t.Fatal(err)
	}
	// No execute bit: stat-ing anything inside dir now fails with EACCES
	// instead of ErrNotExist, on every attempt, forever.
	if err := os.Chmod(dir, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	done := make(chan error, 1)
	go func() {
		_, err := c.CreateArtifact(t.Context(), p.ID, source)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("CreateArtifact should have failed: the artifacts directory cannot be searched")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CreateArtifact did not return: it is spinning on the stat error instead of failing")
	}
}

// review-knowledge #16: unlinking a missing artifact by its address is a
// silent no-op. ResolveArtifact fails not-found, so the fallback "keep the
// reference as written" kept the whole address (/KEY/artifacts/x.png) --
// which never equals the plain name (x.png) editDocArtifacts's list holds --
// so the stub is never found and nothing changes, without an error either.
func TestUnlinkArtifactFromDocByAddressWhenTheArtifactIsGone(t *testing.T) {
	c, p, _ := kbCore(t)
	source := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(source, []byte("some text"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := c.CreateArtifact(t.Context(), p.ID, source)
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Notes"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if _, err := c.LinkArtifactToDoc(t.Context(), p.ID, doc.Slug, artifact.Name); err != nil {
		t.Fatalf("LinkArtifactToDoc: %v", err)
	}
	address := ArtifactAddress(p.Key, artifact.Name)

	if err := c.DeleteArtifact(t.Context(), p.ID, artifact.ID); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}

	if _, err := c.UnlinkArtifactFromDoc(t.Context(), p.ID, doc.Slug, address); err != nil {
		t.Fatalf("UnlinkArtifactFromDoc(%s): %v", address, err)
	}

	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(fm.Artifacts, artifact.Name) {
		t.Errorf("Artifacts = %v, want %s removed", fm.Artifacts, artifact.Name)
	}
}
