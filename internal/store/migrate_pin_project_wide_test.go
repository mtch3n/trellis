package store

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// Before 0018 a project-wide pin could be stored twice. The migration keeps
// the newest and then refuses a second one.
func TestMigrationPinProjectWideKeepsOnePin(t *testing.T) {
	db, err := connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := goose.UpTo(db.DB, "migrations", 17); err != nil {
		t.Fatalf("UpTo(17): %v", err)
	}
	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'ALPHA', 'ALPHA', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'board1', 'board1', 1, 1)`,
		`INSERT INTO knowledge (id, project_id, slug, title, path, content_hash, mtime, size, created_at, updated_at)
		   VALUES ('n1', 'p1', 'design', 'Design', '/kb/design.md', 'h', 1, 1, 1, 1)`,
		`INSERT INTO pin (id, knowledge_id, board_id, created_at) VALUES
		   ('old', 'n1', NULL, 1), ('new', 'n1', NULL, 2), ('board', 'n1', 'b1', 3)`,
	} {
		mustExec(t, db, q)
	}

	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var ids []string
	if err := db.Select(&ids, `SELECT id FROM pin ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "board" || ids[1] != "new" {
		t.Fatalf("pins after migration = %v, want [board new]", ids)
	}
	if _, err := db.Exec(`INSERT INTO pin (id, knowledge_id, board_id, created_at) VALUES ('again', 'n1', NULL, 4)`); err == nil {
		t.Fatal("a second project-wide pin was accepted")
	}
}
