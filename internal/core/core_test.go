package core

import (
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/store"
)

// TestNewPanicsOnEmptyRoot guards the TRELLIS-48 invariant: an empty root is
// a programming error, not user input, and must never fall back to the
// user's real home the way the old kbRoot/home.Root() fallback did.
func TestNewPanicsOnEmptyRoot(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	defer func() {
		if recover() == nil {
			t.Fatal("New with an empty root did not panic")
		}
	}()
	New(db, FixedClock{MS: 1}, "test:1", "")
}
