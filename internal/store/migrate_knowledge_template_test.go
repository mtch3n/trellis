package store

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// Migration 0020 renames doc_type to template and clears the default value.
func TestMigrationKnowledgeTemplate(t *testing.T) {
	db, err := connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := goose.UpTo(db.DB, "migrations", 19); err != nil {
		t.Fatalf("UpTo(19): %v", err)
	}
	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'ALPHA', 'ALPHA', 1)`,
		`INSERT INTO knowledge (id, project_id, slug, title, path, doc_type, content_hash, mtime, size, created_at, updated_at)
		   VALUES ('n1', 'p1', 'design', 'Design', '/kb/design.md', 'note', 'h1', 1, 1, 1, 1)`,
		`INSERT INTO knowledge (id, project_id, slug, title, path, doc_type, content_hash, mtime, size, created_at, updated_at)
		   VALUES ('n2', 'p1', 'decision', 'Decision', '/kb/decision.md', 'decision', 'h2', 2, 2, 2, 2)`,
	} {
		mustExec(t, db, q)
	}

	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	type row struct {
		ID       string
		Template string
	}
	var rows []row
	if err := db.Select(&rows, `SELECT id, template FROM knowledge ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "n1" || rows[0].Template != "" {
		t.Fatalf("n1: expected template='', got template=%q", rows[0].Template)
	}
	if rows[1].ID != "n2" || rows[1].Template != "decision" {
		t.Fatalf("n2: expected template='decision', got template=%q", rows[1].Template)
	}
}
