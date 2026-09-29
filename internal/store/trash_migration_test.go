package store

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestTrash_TRASH_C28_upgrade_keeps_every_row_live(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	old, err := connect(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(old.DB, "migrations", 16); err != nil {
		t.Fatalf("UpTo(16): %v", err)
	}
	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES ('p', 'KEY', 'KEY', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b', 'p', 'main', 'main', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c', 'b', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at, ref)
		 VALUES ('k', 'p', 'b', 1, 'c', 'a', 'kept', 1, 1, 'KEY-1')`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	old.Close()

	for range 2 {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		var cards, trashed int
		if err := db.Get(&cards, `SELECT count(*) FROM card WHERE title = 'kept'`); err != nil {
			t.Fatal(err)
		}
		if err := db.Get(&trashed, `SELECT count(*) FROM trash`); err != nil {
			t.Fatalf("trash table: %v", err)
		}
		if cards != 1 || trashed != 0 {
			t.Errorf("cards = %d, trashed = %d; want 1 live card and an empty trash", cards, trashed)
		}
		db.Close()
	}
}
