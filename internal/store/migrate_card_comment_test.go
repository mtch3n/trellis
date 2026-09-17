package store

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigrationCardCommentRenamesNoteTable verifies that the note table is renamed
// to comment and event records are updated to use 'comment' as entity_type.
func TestMigrationCardCommentRenamesNoteTable(t *testing.T) {
	db, err := connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Up to migration 19 to have all the tables and indices
	if err := goose.UpTo(db.DB, "migrations", 19); err != nil {
		t.Fatalf("UpTo(19): %v", err)
	}

	// Seed test data
	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'TEST', 'Test', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'board1', 'board1', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'col1', 1)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at)
		   VALUES ('card1', 'p1', 'b1', 1, 'c1', '1', 'Card 1', 1, 1)`,
		`INSERT INTO agent (id, handle, kind, cwd, host, pid, first_seen, last_seen)
		   VALUES ('agent1', 'h1', 'agent', '/tmp', 'host1', 1, 1, 1)`,
		`INSERT INTO note (id, card_id, actor, body_md, created_at)
		   VALUES ('note1', 'card1', 'agent1', 'This is a note', 1)`,
		`INSERT INTO event (ts, actor, entity_type, entity_id, action, field)
		   VALUES (1, 'agent1', 'note', 'note1', 'created', NULL)`,
	} {
		mustExec(t, db, q)
	}

	// Run the migration
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Verify the note table is renamed to comment
	var tableNames []string
	if err := db.Select(&tableNames, `SELECT name FROM sqlite_master WHERE type='table' AND name IN ('note', 'comment')`); err != nil {
		t.Fatal(err)
	}
	if len(tableNames) != 1 || tableNames[0] != "comment" {
		t.Fatalf("expected comment table to exist, got tables: %v", tableNames)
	}

	// Verify data was preserved
	var comments []struct {
		ID        string `db:"id"`
		CardID    string `db:"card_id"`
		Actor     string `db:"actor"`
		BodyMD    string `db:"body_md"`
		CreatedAt int64  `db:"created_at"`
	}
	if err := db.Select(&comments, `SELECT id, card_id, actor, body_md, created_at FROM comment`); err != nil {
		t.Fatalf("failed to query comment table: %v", err)
	}
	if len(comments) != 1 || comments[0].ID != "note1" || comments[0].BodyMD != "This is a note" {
		t.Fatalf("comment data not preserved correctly: %+v", comments)
	}

	// Verify the index was renamed
	var indexNames []string
	if err := db.Select(&indexNames, `SELECT name FROM sqlite_master WHERE type='index' AND name IN ('note_card', 'comment_card')`); err != nil {
		t.Fatal(err)
	}
	if len(indexNames) != 1 || indexNames[0] != "comment_card" {
		t.Fatalf("expected comment_card index to exist, got indices: %v", indexNames)
	}

	// Verify event entity_type was updated
	var events []struct {
		EntityType string `db:"entity_type"`
		EntityID   string `db:"entity_id"`
		Action     string `db:"action"`
	}
	if err := db.Select(&events, `SELECT entity_type, entity_id, action FROM event WHERE entity_id = 'note1'`); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EntityType != "comment" {
		t.Fatalf("event entity_type not updated: %+v", events)
	}
}
