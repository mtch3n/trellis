package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupWritesAReadableCopy(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "backed up"}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "copy.db")
	if err := c.Backup(t.Context(), dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
		t.Fatalf("Stat(%s) = %v, %v", dest, fi, err)
	}
}
