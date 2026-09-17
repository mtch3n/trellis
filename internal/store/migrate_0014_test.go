package store

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

func openAtVersion(t *testing.T, version int64) *sqlx.DB {
	t.Helper()
	db, err := connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := goose.UpTo(db.DB, "migrations", version); err != nil {
		t.Fatalf("UpTo(%d): %v", version, err)
	}
	return db
}

func mustExec(t *testing.T, db *sqlx.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func migrateUp(t *testing.T, db *sqlx.DB) {
	t.Helper()
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}
}

// seedV13 fills every table 0014 touches, with the schema a database has at
// version 13 -- the state of a real database that ran this release's earlier
// migrations -- so a cascade or a lost row anywhere shows up.
func seedV13(t *testing.T, db *sqlx.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO project (id, key, identity_kind, identity_value, root_path, name, created_at) VALUES
		   ('p1', 'ALPHA', 'path', '/src/alpha', '/src/alpha', 'ALPHA', 1),
		   ('p2', 'MY-APP', 'remote', 'github.com/x/app', '/src/app', 'MY-APP', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES
		   ('b1', 'p1', 'alpha', 'alpha', 1, 1), ('b2', 'p2', 'app', 'app', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'backlog', 0), ('c2', 'b2', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at) VALUES
		   ('k1', 'p1', 'b1', 1, 'c1', 'a', 'one', 1, 1),
		   ('k2', 'p1', 'b1', 12, 'c1', 'b', 'twelve', 1, 1),
		   ('k3', 'p2', 'b2', 1, 'c2', 'a', 'other', 1, 1)`,
		`INSERT INTO card_revision (card_id, version, title, body_md, actor, created_at) VALUES ('k1', 1, 'one', '', 'a', 1)`,
		`INSERT INTO label (id, project_id, name, description, created_at) VALUES ('l1', 'p1', 'bug', 'A defect', 1)`,
		`INSERT INTO card_label (card_id, label_id) VALUES ('k1', 'l1')`,
		`INSERT INTO knowledge (id, project_id, slug, title, path, doc_type, content_hash, mtime, size, created_at, updated_at) VALUES
		   ('n1', 'p1', 'design', 'Design', '/kb/design.md', 'note', 'h1', 1, 1, 1, 1),
		   ('n2', 'p2', 'choice', 'Choice', '/kb/choice.md', 'decision', 'h2', 1, 1, 1, 1)`,
		`INSERT INTO project_config (project_id, key, value, updated_at) VALUES ('p1', 'lease.ttl', '10m', 1)`,
		`INSERT INTO pin (id, knowledge_id, board_id, created_at) VALUES
		   ('pin-old', 'n1', NULL, 1), ('pin-new', 'n1', NULL, 2), ('pin-board', 'n1', 'b1', 3)`,
		`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel) VALUES
		   ('card', 'k1', 'card', 'k2', 'ALPHA-12', NULL, 'blocked_by')`,
		`INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel) VALUES
		   ('card', 'k1', 'card', 'k2', 'ALPHA-12', NULL, 'blocked_by')`,
		`INSERT INTO agent (id, handle, kind, cwd, host, pid, first_seen, last_seen) VALUES ('a1', 'h1', 'agent', '/tmp', 'host', 1, 1, 1)`,
		`INSERT INTO note (id, card_id, actor, body_md, created_at) VALUES ('m1', 'k1', 'a1', 'handed off', 1), ('m2', 'k3', 'a1', 'later', 2)`,
		`INSERT INTO event (seq, ts, actor, entity_type, entity_id, action) VALUES
		   (1, 1, 'a', 'card', 'k1', 'created'),
		   (2, 1, 'a', 'knowledge', 'n2', 'created'),
		   (3, 1, 'a', 'board', 'b1', 'created'),
		   (4, 1, 'a', 'label', 'l1', 'created'),
		   (5, 1, 'a', 'note', 'm1', 'created'),
		   (6, 1, 'a', 'note', 'm2', 'created'),
		   (7, 1, 'a', 'card', 'gone', 'deleted')`,
	} {
		mustExec(t, db, q)
	}
}

func columns(t *testing.T, db *sqlx.DB, table string) []string {
	t.Helper()
	var cols []string
	if err := db.Select(&cols, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table); err != nil {
		t.Fatal(err)
	}
	return cols
}

func TestMigration0014KeepsEveryRow(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	migrateUp(t, db)

	for table, want := range map[string]int{
		"project": 2, "board": 2, "column_": 2, "card": 3, "card_revision": 1, "label": 1,
		"card_label": 1, "knowledge": 2, "project_config": 1, "comment": 2,
		"pin":  2, // the older of two project-wide pins is dropped
		"link": 1, // the repeated card link is dropped
	} {
		var n int
		if err := db.Get(&n, "SELECT count(*) FROM "+table); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s has %d rows, want %d", table, n, want)
		}
	}
	if got := columns(t, db, "project"); !slices.Equal(got, []string{"id", "key", "name", "created_at"}) {
		t.Errorf("project columns = %v", got)
	}
	var fk int
	if err := db.Get(&fk, `PRAGMA foreign_keys`); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want enforcement back on", fk, err)
	}
	var violations int
	if err := db.Get(&violations, `SELECT count(*) FROM pragma_foreign_key_check`); err != nil || violations != 0 {
		t.Errorf("foreign_key_check: %d, %v", violations, err)
	}
	if version, err := goose.GetDBVersion(db.DB); err != nil || version != 14 {
		t.Errorf("version = %d, %v", version, err)
	}
}

func TestMigration0014RecordsTheOldBindings(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	migrateUp(t, db)
	var got []string
	if err := db.Select(&got,
		`SELECT entity_id || ' ' || field || ' ' || old_value FROM event
		 WHERE entity_type = 'project' AND action = 'unbound' AND actor = 'migration'
		 ORDER BY entity_id, field`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"p1 identity path:/src/alpha",
		"p1 root_path /src/alpha",
		"p2 identity remote:github.com/x/app",
		"p2 root_path /src/app",
	}
	if !slices.Equal(got, want) {
		t.Errorf("unbound events = %v, want %v", got, want)
	}
}

func TestMigration0014ConvertsTheData(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	migrateUp(t, db)

	var refs []string
	if err := db.Select(&refs, `SELECT ref FROM card ORDER BY ref`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ALPHA-1", "ALPHA-12", "MY-APP-1"}; !slices.Equal(refs, want) {
		t.Errorf("refs = %v, want %v", refs, want)
	}
	if _, err := db.Exec(`UPDATE card SET ref = 'ALPHA-1' WHERE id = 'k3'`); err == nil {
		t.Error("two cards can share a ref")
	}

	var templates []string
	if err := db.Select(&templates, `SELECT id || '=' || template FROM knowledge ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"n1=", "n2=decision"}; !slices.Equal(templates, want) {
		t.Errorf("templates = %v, want %v", templates, want)
	}

	var events []string
	if err := db.Select(&events,
		`SELECT entity_type || ' ' || entity_id || ' ' || COALESCE(project_id, '-') FROM event
		 WHERE actor = 'a' ORDER BY seq`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"card k1 p1", "knowledge n2 p2", "board b1 p1", "label l1 p1",
		"comment m1 p1", "comment m2 p2", "card gone -",
	}
	if !slices.Equal(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}

	var tables []string
	if err := db.Select(&tables,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('note', 'comment', 'event_consumer', 'merged_project') ORDER BY name`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"comment", "event_consumer", "merged_project"}; !slices.Equal(tables, want) {
		t.Errorf("tables = %v, want %v", tables, want)
	}

	// The new uniqueness holds.
	if _, err := db.Exec(`INSERT INTO pin (id, knowledge_id, board_id, created_at) VALUES ('again', 'n1', NULL, 4)`); err == nil {
		t.Error("a second project-wide pin was accepted")
	}
	mustExec(t, db, `INSERT OR IGNORE INTO link (from_type, from_id, to_type, to_id, to_raw, anchor, rel)
		VALUES ('card', 'k1', 'card', 'k2', 'ALPHA-12', NULL, 'blocked_by')`)
	var links int
	if err := db.Get(&links, `SELECT count(*) FROM link`); err != nil || links != 1 {
		t.Errorf("links after a repeat = %d, %v", links, err)
	}

	// A merged key is reserved until its survivor goes.
	mustExec(t, db, `INSERT INTO merged_project (key, into_id, merged_at) VALUES ('OLD', 'p2', 1)`)
	mustExec(t, db, `DELETE FROM project WHERE id = 'p2'`)
	var reserved int
	if err := db.Get(&reserved, `SELECT count(*) FROM merged_project`); err != nil || reserved != 0 {
		t.Errorf("merged_project rows = %d, %v; deleting the survivor must free the key", reserved, err)
	}
}

// goose records the version separately; a crash in between reruns the
// migration against a schema it already changed.
func TestMigration0014IsSafeToRerun(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	migrateUp(t, db)
	var before int
	if err := db.Get(&before, `SELECT count(*) FROM event`); err != nil {
		t.Fatal(err)
	}
	if err := upMemoryGroundwork(t.Context(), db.DB); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var after int
	if err := db.Get(&after, `SELECT count(*) FROM event`); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("a rerun changed the event log: %d -> %d", before, after)
	}
}

// A failure anywhere leaves the database exactly at version 13.
func TestMigration0014RollsBackWhole(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	mustExec(t, db, `PRAGMA foreign_keys = OFF`)
	mustExec(t, db, `INSERT INTO board (id, project_id, name, slug, created_at) VALUES ('orphan', 'missing', 'x', 'x', 1)`)
	mustExec(t, db, `PRAGMA foreign_keys = ON`)

	err := goose.Up(db.DB, "migrations")
	if err == nil || !strings.Contains(err.Error(), "foreign key check failed") {
		t.Fatalf("Up = %v, want the foreign key check to fail it", err)
	}
	if got := columns(t, db, "project"); !slices.Contains(got, "root_path") {
		t.Errorf("project columns = %v; the rebuild was not rolled back", got)
	}
	if got := columns(t, db, "knowledge"); !slices.Contains(got, "doc_type") {
		t.Errorf("knowledge columns = %v; the rename was not rolled back", got)
	}
	var tables int
	if err := db.Get(&tables, `SELECT count(*) FROM sqlite_master WHERE name IN ('event_consumer', 'comment', 'merged_project')`); err != nil || tables != 0 {
		t.Errorf("%d new tables survived the rollback, %v", tables, err)
	}
	if version, err := goose.GetDBVersion(db.DB); err != nil || version != 13 {
		t.Errorf("version = %d, %v; want 13", version, err)
	}
	var events int
	if err := db.Get(&events, `SELECT count(*) FROM event WHERE action = 'unbound'`); err != nil || events != 0 {
		t.Errorf("unbound events = %d, %v; want none after a rollback", events, err)
	}
	var fk int
	if err := db.Get(&fk, `PRAGMA foreign_keys`); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want enforcement back on", fk, err)
	}
}

func TestMigration0014GoesDownAndUpAgain(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	migrateUp(t, db)
	if err := goose.DownTo(db.DB, "migrations", 13); err != nil {
		t.Fatalf("DownTo(13): %v", err)
	}
	if got := columns(t, db, "knowledge"); !slices.Contains(got, "doc_type") {
		t.Errorf("knowledge columns after down = %v", got)
	}
	var notes int
	if err := db.Get(&notes, `SELECT count(*) FROM note`); err != nil || notes != 2 {
		t.Errorf("notes after down = %d, %v", notes, err)
	}
	migrateUp(t, db)
	var comments int
	if err := db.Get(&comments, `SELECT count(*) FROM comment`); err != nil || comments != 2 {
		t.Errorf("comments after up again = %d, %v", comments, err)
	}
}
