package core

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestFileStageRollsBackInReverseOrder(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "projects", "API", "knowledge", "a.md")
	dst := filepath.Join(root, "projects", "MONO", "knowledge", "a.md")
	other := filepath.Join(root, "projects", "CORE", "knowledge", "b.md")
	writeFile(t, src, "moved")
	writeFile(t, other, "before")

	s := &fileStage{}
	if err := s.move(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.rewrite(other, []byte("after")); err != nil {
		t.Fatal(err)
	}
	// A move's source stays put until finalize: a process that dies before
	// commit must find it exactly where its row still says it is.
	if !exists(src) || readFile(t, dst) != "moved" || readFile(t, other) != "after" {
		t.Fatal("the staged operations did not happen")
	}

	if err := s.rollback(); err != nil {
		t.Fatal(err)
	}
	if exists(dst) || readFile(t, src) != "moved" || readFile(t, other) != "before" {
		t.Error("rollback did not restore the files")
	}
}

// A failure after a move must leave the row-and-file pair it never got to
// finalize alone: the source stays, and the half-published destination is
// the only thing rollback removes.
func TestFileStageRollbackAfterMoveLeavesSourceIntact(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
	writeFile(t, src, "moved")

	s := &fileStage{}
	if err := s.move(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.rollback(); err != nil {
		t.Fatal(err)
	}
	if !exists(src) {
		t.Error("rollback removed the move's source")
	}
	if exists(dst) {
		t.Error("rollback left the destination behind")
	}
	if readFile(t, src) != "moved" {
		t.Error("rollback changed the source's content")
	}
}

// finalize is the only thing allowed to remove a move's source, and only
// once the transaction that depended on the move has committed.
func TestFileStageFinalizeRemovesTheSourceOfAMove(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
	writeFile(t, src, "moved")

	s := &fileStage{}
	if err := s.move(src, dst); err != nil {
		t.Fatal(err)
	}
	if !exists(src) {
		t.Fatal("move must not remove its source before finalize")
	}

	if err := s.finalize(); err != nil {
		t.Fatal(err)
	}
	if exists(src) {
		t.Error("finalize did not remove the move's source")
	}
	if readFile(t, dst) != "moved" {
		t.Error("finalize must not touch the destination")
	}
}

func TestFileStageNeverOverwritesOnMove(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
	writeFile(t, src, "src")
	writeFile(t, dst, "dst")
	s := &fileStage{}
	if err := s.move(src, dst); err == nil {
		t.Fatal("a move onto an existing file succeeded")
	}
	if readFile(t, src) != "src" || readFile(t, dst) != "dst" {
		t.Error("a refused move changed a file")
	}
}

func TestCopyUnderKeepsThePathsBelowTheRoot(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "projects", "API", "knowledge", "a.md")
	b := filepath.Join(root, "global", "knowledge", "b.md")
	writeFile(t, a, "A")
	writeFile(t, b, "B")
	out := filepath.Join(t.TempDir(), "files")
	hashes, err := copyUnder(root, out, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := fileHash(a); hashes[a] != want || len(hashes) != 2 {
		t.Errorf("hashes = %v", hashes)
	}
	if readFile(t, filepath.Join(out, "projects", "API", "knowledge", "a.md")) != "A" ||
		readFile(t, filepath.Join(out, "global", "knowledge", "b.md")) != "B" {
		t.Error("the copies are not laid out as under the root")
	}
	if _, err := copyUnder(root, out, []string{filepath.Join(t.TempDir(), "x.md")}); err == nil {
		t.Error("a file outside the root was accepted")
	}
}
