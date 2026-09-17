package store

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

// seedV15Events fills the database with events from various entity types, matching
// the schema before migration 0016 (no project_id in event table).
func seedV15Events(t *testing.T, db *sqlx.DB) {
	t.Helper()

	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES
		   ('p1', 'ALPHA', 'ALPHA', 1),
		   ('p2', 'BETA', 'BETA', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES
		   ('b1', 'p1', 'board1', 'board1', 1, 1),
		   ('b2', 'p2', 'board2', 'board2', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES
		   ('c1', 'b1', 'backlog', 0),
		   ('c2', 'b2', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at)
		   VALUES ('k1', 'p1', 'b1', 1, 'c1', 'a', 'Card1', 1, 1),
		        ('k2', 'p2', 'b2', 1, 'c2', 'a', 'Card2', 1, 1)`,
		`INSERT INTO label (id, project_id, name, description, created_at) VALUES
		   ('l1', 'p1', 'bug', 'A defect', 1),
		   ('l2', 'p2', 'feature', 'A feature', 1)`,
		`INSERT INTO knowledge (id, project_id, slug, title, path, content_hash, mtime, size, created_at, updated_at)
		   VALUES ('n1', 'p1', 'design', 'Design', '/kb/design.md', 'h', 1, 1, 1, 1),
		          ('n2', 'p2', 'guide', 'Guide', '/kb/guide.md', 'h', 1, 1, 1, 1)`,
		`INSERT INTO note (id, card_id, actor, body_md, created_at)
		   VALUES ('note1', 'k1', 'user1', 'Note text', 1),
		          ('note2', 'k2', 'user1', 'Note text', 1)`,
		// Events before migration (no project_id column exists yet)
		`INSERT INTO event (seq, ts, actor, entity_type, entity_id, action, field, old_value, new_value)
		   VALUES (1, 1, 'user1', 'card', 'k1', 'created', NULL, NULL, 'Card1'),
		          (2, 2, 'user1', 'card', 'k2', 'created', NULL, NULL, 'Card2'),
		          (3, 3, 'user1', 'knowledge', 'n1', 'created', NULL, NULL, 'Design'),
		          (4, 4, 'user1', 'knowledge', 'n2', 'created', NULL, NULL, 'Guide'),
		          (5, 5, 'user1', 'board', 'b1', 'created', NULL, NULL, 'board1'),
		          (6, 6, 'user1', 'board', 'b2', 'created', NULL, NULL, 'board2'),
		          (7, 7, 'user1', 'label', 'l1', 'created', NULL, NULL, 'bug'),
		          (8, 8, 'user1', 'label', 'l2', 'created', NULL, NULL, 'feature'),
		          (9, 9, 'user1', 'note', 'note1', 'created', NULL, NULL, ''),
		          (10, 10, 'user1', 'note', 'note2', 'created', NULL, NULL, '')`,
	} {
		mustExec(t, db, q)
	}
}

func TestMigrationEventProjectIDBackfillsAllEventTypes(t *testing.T) {
	db, err := connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := goose.UpTo(db.DB, "migrations", 15); err != nil {
		t.Fatalf("UpTo(15): %v", err)
	}
	seedV15Events(t, db)

	// Run the migration.
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Verify project_id is backfilled for all event types.
	type eventRow struct {
		EntityType string `db:"entity_type"`
		EntityID   string `db:"entity_id"`
		ProjectID  string `db:"project_id"`
	}
	var events []eventRow
	if err := db.Select(&events,
		`SELECT entity_type, entity_id, COALESCE(project_id, '') as project_id FROM event ORDER BY seq`); err != nil {
		t.Fatal(err)
	}

	want := []eventRow{
		{"card", "k1", "p1"},
		{"card", "k2", "p2"},
		{"knowledge", "n1", "p1"},
		{"knowledge", "n2", "p2"},
		{"board", "b1", "p1"},
		{"board", "b2", "p2"},
		{"label", "l1", "p1"},
		{"label", "l2", "p2"},
		{"comment", "note1", "p1"},
		{"comment", "note2", "p2"},
	}

	if !slices.Equal(events, want) {
		t.Errorf("events after backfill:\ngot  %+v\nwant %+v", events, want)
	}

	// Verify the index was created.
	var indexName string
	if err := db.Get(&indexName,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='event_project_seq'`); err != nil {
		t.Errorf("event_project_seq index not found: %v", err)
	}
}
