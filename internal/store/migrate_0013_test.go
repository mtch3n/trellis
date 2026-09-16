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

// seedV12 fills every table a project owns, with the schema as it was before
// 0013, so a cascade anywhere would show up as a missing row.
func seedV12(t *testing.T, db *sqlx.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO project (id, key, identity_kind, identity_value, root_path, name, created_at) VALUES
		   ('p1', 'ALPHA', 'path', '/src/alpha', '/src/alpha', 'ALPHA', 1),
		   ('p2', 'BETA', 'remote', 'github.com/x/beta', '/src/beta', 'BETA', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'alpha', 'alpha', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at)
		   VALUES ('k1', 'p1', 'b1', 1, 'c1', 'a', 'First', 1, 1)`,
		`INSERT INTO label (id, project_id, name, description, created_at) VALUES ('l1', 'p1', 'bug', 'A defect', 1)`,
		`INSERT INTO card_label (card_id, label_id) VALUES ('k1', 'l1')`,
		`INSERT INTO knowledge (id, project_id, slug, title, path, content_hash, mtime, size, created_at, updated_at)
		   VALUES ('n1', 'p1', 'design', 'Design', '/kb/design.md', 'h', 1, 1, 1, 1)`,
		`INSERT INTO project_config (project_id, key, value, updated_at) VALUES ('p1', 'lease.ttl', '10m', 1)`,
	} {
		mustExec(t, db, q)
	}
}

func projectColumns(t *testing.T, db *sqlx.DB) []string {
	t.Helper()
	var cols []string
	if err := db.Select(&cols, `SELECT name FROM pragma_table_info('project') ORDER BY cid`); err != nil {
		t.Fatal(err)
	}
	return cols
}

func TestDropProjectIdentityKeepsEveryRow(t *testing.T) {
	db := openAtVersion(t, 12)
	seedV12(t, db)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}
	for table, want := range map[string]int{
		"project": 2, "board": 1, "column_": 1, "card": 1, "label": 1,
		"card_label": 1, "knowledge": 1, "project_config": 1,
	} {
		var n int
		if err := db.Get(&n, "SELECT count(*) FROM "+table); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s has %d rows, want %d", table, n, want)
		}
	}
	if got := projectColumns(t, db); !slices.Equal(got, []string{"id", "key", "name", "created_at"}) {
		t.Errorf("project columns = %v", got)
	}
	var fk int
	if err := db.Get(&fk, `PRAGMA foreign_keys`); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want enforcement back on", fk, err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("foreign_key_check reports a violation after the migration")
	}
}

func TestDropProjectIdentityRecordsTheOldBindings(t *testing.T) {
	db := openAtVersion(t, 12)
	seedV12(t, db)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}
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
		"p2 identity remote:github.com/x/beta",
		"p2 root_path /src/beta",
	}
	if !slices.Equal(got, want) {
		t.Errorf("unbound events = %v, want %v", got, want)
	}
}

// goose records the version separately; a crash in between reruns the
// migration against a schema it already rebuilt.
func TestDropProjectIdentityIsSafeToRerun(t *testing.T) {
	db := openAtVersion(t, 12)
	seedV12(t, db)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.Get(&before, `SELECT count(*) FROM event`); err != nil {
		t.Fatal(err)
	}
	if err := dropProjectIdentity(t.Context(), db.DB); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var after int
	if err := db.Get(&after, `SELECT count(*) FROM event`); err != nil {
		t.Fatal(err)
	}
	if after != before || !slices.Equal(projectColumns(t, db), []string{"id", "key", "name", "created_at"}) {
		t.Errorf("a rerun changed something: events %d -> %d, columns %v", before, after, projectColumns(t, db))
	}
}

func TestDropProjectIdentityRollsBackOnAForeignKeyViolation(t *testing.T) {
	db := openAtVersion(t, 12)
	seedV12(t, db)
	mustExec(t, db, `PRAGMA foreign_keys = OFF`)
	mustExec(t, db, `INSERT INTO board (id, project_id, name, slug, created_at) VALUES ('orphan', 'missing', 'x', 'x', 1)`)
	mustExec(t, db, `PRAGMA foreign_keys = ON`)

	err := goose.Up(db.DB, "migrations")
	if err == nil || !strings.Contains(err.Error(), "foreign key check failed") {
		t.Fatalf("Up = %v, want the foreign key check to fail it", err)
	}
	if got := projectColumns(t, db); !slices.Contains(got, "root_path") {
		t.Errorf("project columns = %v; the rebuild was not rolled back", got)
	}
	version, err := goose.GetDBVersion(db.DB)
	if err != nil || version != 12 {
		t.Errorf("version = %d, %v; want 12", version, err)
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
