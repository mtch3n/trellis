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
	// A binary from before 0014 finds its projects by these columns, and
	// creates a new project when none matches: they must come back as they were.
	var bindings []string
	if err := db.Select(&bindings,
		`SELECT id || ' ' || identity_kind || ' ' || identity_value || ' ' || root_path FROM project ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"p1 path /src/alpha /src/alpha", "p2 remote github.com/x/app /src/app"}; !slices.Equal(bindings, want) {
		t.Errorf("bindings after down = %v, want %v", bindings, want)
	}
	var unbound int
	if err := db.Get(&unbound, `SELECT count(*) FROM event WHERE action = 'unbound'`); err != nil || unbound != 0 {
		t.Errorf("unbound events after down = %d, %v", unbound, err)
	}
	migrateUp(t, db)
	if err := db.Get(&unbound, `SELECT count(*) FROM event WHERE action = 'unbound'`); err != nil || unbound != 4 {
		t.Errorf("unbound events after up again = %d, %v", unbound, err)
	}
	var comments int
	if err := db.Get(&comments, `SELECT count(*) FROM comment`); err != nil || comments != 2 {
		t.Errorf("comments after up again = %d, %v", comments, err)
	}
}

// seedArtifact adds one v13 artifact row, keyed to p1 ('ALPHA') from seedV13.
// v13's artifact table still has path; seedV13 itself has no artifact rows,
// since row-count checks elsewhere enumerate every table it touches.
func seedArtifact(t *testing.T, db *sqlx.DB, id, projectID, name string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO artifact (id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at) VALUES
	   ('`+id+`', '`+projectID+`', '`+name+`', '/kb/artifacts/`+name+`', 'image', 'image/png', 100, 'ah-`+id+`', 1, 1)`)
}

// TestMigration0014DropsStoredPathsButKeepsEverythingElse is TRELLIS-36: the
// file location is derived from the storage root, the project key and the
// slug or name, never stored, so up must drop path from both tables while
// leaving every other column exactly as it was.
func TestMigration0014DropsStoredPathsButKeepsEverythingElse(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	seedArtifact(t, db, "a1", "p1", "shot.png")
	migrateUp(t, db)

	if got := columns(t, db, "knowledge"); slices.Contains(got, "path") {
		t.Errorf("knowledge columns = %v, want no path", got)
	}
	if got := columns(t, db, "artifact"); slices.Contains(got, "path") {
		t.Errorf("artifact columns = %v, want no path", got)
	}

	var doc struct {
		Slug        string `db:"slug"`
		Title       string `db:"title"`
		ContentHash string `db:"content_hash"`
		Mtime       int64  `db:"mtime"`
		Size        int64  `db:"size"`
		CreatedAt   int64  `db:"created_at"`
		UpdatedAt   int64  `db:"updated_at"`
	}
	if err := db.Get(&doc, `SELECT slug, title, content_hash, mtime, size, created_at, updated_at FROM knowledge WHERE id = 'n1'`); err != nil {
		t.Fatal(err)
	}
	if doc.Slug != "design" || doc.Title != "Design" || doc.ContentHash != "h1" || doc.Mtime != 1 || doc.Size != 1 {
		t.Errorf("knowledge row lost a value: %+v", doc)
	}

	var art struct {
		Name        string `db:"name"`
		Kind        string `db:"kind"`
		MIME        string `db:"mime"`
		ContentHash string `db:"content_hash"`
		Size        int64  `db:"size"`
	}
	if err := db.Get(&art, `SELECT name, kind, mime, content_hash, size FROM artifact WHERE id = 'a1'`); err != nil {
		t.Fatal(err)
	}
	if art.Name != "shot.png" || art.Kind != "image" || art.MIME != "image/png" || art.ContentHash != "ah-a1" || art.Size != 100 {
		t.Errorf("artifact row lost a value: %+v", art)
	}
}

// TestMigration0014ArtifactNameUniqueWithinProject is TRELLIS-36's
// UNIQUE(project_id, name), replacing UNIQUE(project_id, path). The two never
// differed in practice -- path was always derived 1:1 from (project, name) --
// so this changes nothing a real caller could already do; it now holds
// directly on the column Trellis actually addresses artifacts by.
func TestMigration0014ArtifactNameUniqueWithinProject(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	seedArtifact(t, db, "a1", "p1", "shot.png")
	migrateUp(t, db)

	if _, err := db.Exec(`INSERT INTO artifact (id, project_id, name, kind, mime, size, content_hash, created_at, updated_at) VALUES
		('a2', 'p1', 'shot.png', 'image', 'image/png', 200, 'ah-a2', 2, 2)`); err == nil {
		t.Error("a second artifact named the same in the same project was accepted")
	}
	// A different project may reuse the name.
	if _, err := db.Exec(`INSERT INTO artifact (id, project_id, name, kind, mime, size, content_hash, created_at, updated_at) VALUES
		('a3', 'p2', 'shot.png', 'image', 'image/png', 300, 'ah-a3', 3, 3)`); err != nil {
		t.Errorf("a different project could not reuse the name: %v", err)
	}
}

// TestMigration0014DownRestoresDerivedPaths checks down's re-added path
// against exactly what Core.docPath/Core.artifactPath would derive: root is
// the directory holding the database file itself (PRAGMA database_list,
// row main), never a guess. Then up again must drop path again: the two
// migrations round-trip.
func TestMigration0014DownRestoresDerivedPaths(t *testing.T) {
	db := openAtVersion(t, 13)
	seedV13(t, db)
	seedArtifact(t, db, "a1", "p1", "shot.png")
	// A global entry, to exercise the other half of docPath's branch.
	mustExec(t, db, `INSERT INTO knowledge (id, project_id, slug, title, path, doc_type, content_hash, mtime, size, global, created_at, updated_at) VALUES
	   ('n3', 'p1', 'shared/glossary', 'Glossary', '/kb/glossary.md', 'note', 'h3', 1, 1, 1, 1, 1)`)
	migrateUp(t, db)
	if err := goose.DownTo(db.DB, "migrations", 13); err != nil {
		t.Fatalf("DownTo(13): %v", err)
	}

	var dbFile string
	if err := db.Get(&dbFile, `SELECT file FROM pragma_database_list WHERE name = 'main'`); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(dbFile)

	var docPath string
	if err := db.Get(&docPath, `SELECT path FROM knowledge WHERE id = 'n1'`); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "projects", "ALPHA", "knowledge", "design.md"); docPath != want {
		t.Errorf("path = %q, want %q", docPath, want)
	}

	var globalPath string
	if err := db.Get(&globalPath, `SELECT path FROM knowledge WHERE id = 'n3'`); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "global", "knowledge", "shared", "glossary.md"); globalPath != want {
		t.Errorf("global path = %q, want %q", globalPath, want)
	}

	var artPath string
	if err := db.Get(&artPath, `SELECT path FROM artifact WHERE id = 'a1'`); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "projects", "ALPHA", "artifacts", "shot.png"); artPath != want {
		t.Errorf("artifact path = %q, want %q", artPath, want)
	}

	// Round-trip: up again drops path once more.
	migrateUp(t, db)
	if got := columns(t, db, "knowledge"); slices.Contains(got, "path") {
		t.Errorf("knowledge columns after up again = %v, want no path", got)
	}
	if got := columns(t, db, "artifact"); slices.Contains(got, "path") {
		t.Errorf("artifact columns after up again = %v, want no path", got)
	}
	var artifacts int
	if err := db.Get(&artifacts, `SELECT count(*) FROM artifact`); err != nil || artifacts != 1 {
		t.Errorf("artifacts after round-trip = %d, %v, want 1", artifacts, err)
	}
}
