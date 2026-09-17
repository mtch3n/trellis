package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/store"
)

// TestACopiedRootActsOnlyOnItsOwnFiles is TRELLIS-36's acceptance test: since
// every path Core computes is derived from the root it is given, rather than
// stored, a storage root that is copied wholesale -- the database included --
// is a real, independent copy. Opening a second Core on the copy, with the
// copy's root, must read, edit, move and serve only the copy's files; the
// original must be untouched, byte for byte, afterward.
func TestACopiedRootActsOnlyOnItsOwnFiles(t *testing.T) {
	ctx := context.Background()

	rootA := filepath.Join(t.TempDir(), "a")
	if err := os.MkdirAll(rootA, 0o700); err != nil {
		t.Fatal(err)
	}
	dbA, err := store.Open(filepath.Join(rootA, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	a := New(dbA, FixedClock{MS: 1_757_000_000_000}, "test:a", rootA)
	p := seededProject(t, a)
	seededBoard(t, a, p)

	projectDoc, err := a.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Project Doc", Body: "project body\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge (project entry): %v", err)
	}
	sharedSeed, err := a.CreateKnowledge(ctx, p.ID, NewKnowledge{Title: "Shared Doc", Body: "shared body\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge (to be escalated): %v", err)
	}
	globalDoc, err := a.EscalateKnowledge(ctx, p.ID, sharedSeed.Slug, "shared across projects")
	if err != nil {
		t.Fatalf("EscalateKnowledge: %v", err)
	}
	source := filepath.Join(t.TempDir(), "evidence.png")
	if err := os.WriteFile(source, []byte("\x89PNG\r\n\x1a\nevidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := a.CreateArtifact(ctx, p.ID, source)
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	// A clean close checkpoints SQLite's WAL, so the copy below (a plain file
	// tree copy, not an online backup) sees consistent bytes.
	if err := dbA.Close(); err != nil {
		t.Fatalf("closing A before copying: %v", err)
	}
	beforeA := snapshotTree(t, rootA)

	rootB := filepath.Join(t.TempDir(), "b")
	if err := copyTree(rootB, rootA); err != nil {
		t.Fatalf("copying root A to B: %v", err)
	}

	dbB, err := store.Open(filepath.Join(rootB, "trellis.db"))
	if err != nil {
		t.Fatalf("opening B: %v", err)
	}
	defer dbB.Close()
	b := New(dbB, FixedClock{MS: 1_757_000_000_001}, "test:b", rootB)

	// Reading acts on B's files only.
	loadedProject, err := b.LoadKnowledge(ctx, p.ID, projectDoc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge on B: %v", err)
	}
	if !underRoot(loadedProject.Path, rootB) {
		t.Errorf("project doc path = %q, want it under B's root %q", loadedProject.Path, rootB)
	}
	loadedGlobal, err := b.ReadKnowledge(ctx, "", "/GLOBAL/knowledge/"+globalDoc.Slug)
	if err != nil {
		t.Fatalf("ReadKnowledge (global) on B: %v", err)
	}
	if !underRoot(loadedGlobal.Path, rootB) {
		t.Errorf("global doc path = %q, want it under B's root %q", loadedGlobal.Path, rootB)
	}
	_, artifactFilePath, err := b.ArtifactFile(ctx, p.ID, artifact.Name)
	if err != nil {
		t.Fatalf("ArtifactFile on B: %v", err)
	}
	if !underRoot(artifactFilePath, rootB) {
		t.Errorf("artifact path = %q, want it under B's root %q", artifactFilePath, rootB)
	}

	// Editing acts on B's files only.
	edited, err := b.EditKnowledge(ctx, p.ID, projectDoc.Slug, "edited on B\n", &loadedProject.Version)
	if err != nil {
		t.Fatalf("EditKnowledge on B: %v", err)
	}
	if !underRoot(edited.Path, rootB) {
		t.Errorf("edited doc path = %q, want it under B's root %q", edited.Path, rootB)
	}
	editedRaw, err := os.ReadFile(edited.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(editedRaw), "edited on B") {
		t.Errorf("B's file was not edited: %s", editedRaw)
	}

	// Moving acts on B's files only.
	moved, err := b.MoveKnowledge(ctx, p.ID, edited.Slug, "moved/on-b", false)
	if err != nil {
		t.Fatalf("MoveKnowledge on B: %v", err)
	}
	if !underRoot(moved.Path, rootB) {
		t.Errorf("moved doc path = %q, want it under B's root %q", moved.Path, rootB)
	}
	if _, err := os.Stat(moved.Path); err != nil {
		t.Errorf("moved file does not exist at %q: %v", moved.Path, err)
	}

	// A's files are byte-identical to before any of B's operations ran.
	afterA := snapshotTree(t, rootA)
	if len(beforeA) != len(afterA) {
		t.Fatalf("A's file count changed: %d -> %d", len(beforeA), len(afterA))
	}
	for rel, hash := range beforeA {
		if afterA[rel] != hash {
			t.Errorf("A's %s changed", rel)
		}
	}
}

// underRoot reports whether path lies inside root.
func underRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// copyTree copies every file under src into dst, preserving its structure.
// Unlike copyDirAtomic (one directory, no recursion), this walks the whole
// tree, which is what copying a storage root needs.
func copyTree(dst, src string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyAtomic(target, path)
	})
}

// snapshotTree hashes every regular file under root, keyed by its path
// relative to root, so two snapshots can be compared byte for byte without
// depending on modification times.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		out[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	return out
}
