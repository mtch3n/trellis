package store

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// Before 0019 blocking a card twice stored the link twice. The migration
// keeps one and then ignores a repeat.
func TestMigrationCardLinkUniqueKeepsOneLink(t *testing.T) {
	db, err := connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := goose.UpTo(db.DB, "migrations", 18); err != nil {
		t.Fatalf("UpTo(18): %v", err)
	}
	insert := `INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
		VALUES ('card', 'a', 'card', 'b', 'X-2', NULL, 'blocked_by')`
	mustExec(t, db, insert)
	mustExec(t, db, insert)
	mustExec(t, db, `INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
		VALUES ('card', 'a', 'card', 'b', 'X-2', NULL, 'relates_to')`)

	count := func() int {
		t.Helper()
		var n int
		if err := db.Get(&n, `SELECT COUNT(*) FROM link`); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 3 {
		t.Fatalf("seeded %d links, want 3 (the bug stores the repeat)", n)
	}
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if n := count(); n != 2 {
		t.Fatalf("%d links after migration, want 2", n)
	}
	mustExec(t, db, insert)
	if n := count(); n != 2 {
		t.Fatalf("%d links after a repeat, want 2", n)
	}
}
