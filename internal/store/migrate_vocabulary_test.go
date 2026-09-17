package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

const entryBefore = "---\ntitle: A\ntemplate: runbook\nsources:\n  - /TR/knowledge/b\n---\nSee [[/TR/knowledge/b]], [[/TR/knowledge/ops/rollback]] and /home/me/knowledge/notes.\n"

func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// rootBefore is a storage root holding trellis.db just before this migration:
// one project TR with one entry, one card and comment, one global entry, one
// template and one revision. The schema is as migration 0014 left it:
// knowledge.template, a comment table, project(id, key, name, created_at).
func rootBefore(t *testing.T) (string, *sqlx.DB) {
	t.Helper()
	return rootBeforeAt(t, t.TempDir())
}

// rootBeforeAt is rootBefore at a root the caller chose.
func rootBeforeAt(t *testing.T, root string) (string, *sqlx.DB) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := connect(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := goose.UpTo(db.DB, "migrations", vocabularyVersion-1); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "projects", "TR", "knowledge", "a.md")
	writeFile(t, entry, entryBefore)
	writeFile(t, filepath.Join(root, "projects", "TR", "knowledge", ".a.md", "1.md"), "---\ntitle: A\ntemplate: runbook\n---\nsee /TR/knowledge/b\n")
	writeFile(t, filepath.Join(root, "global", "knowledge", "g.md"), "---\ntitle: G\ntemplate: decision\n---\nSee /TR/knowledge/a\n")
	writeFile(t, filepath.Join(root, "templates", "cited.md"), "---\nenforce: reject\nverify: [sources]\n---\n# {{title}}\n")
	writeFile(t, filepath.Join(root, "config.yaml"), "lease:\n  ttl: 30m\n")
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'TR', 'TR', 1)`, nil},
		{`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'main', 'main', 1, 1)`, nil},
		{`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'backlog', 0)`, nil},
		{`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, body_md, owner, lease_until, created_at, updated_at)
		  VALUES ('k1', 'p1', 'b1', 1, 'c1', 'a', 'First', 'see /TR/knowledge/a', 'agent:x', 99, 1, 1)`, nil},
		{`INSERT INTO comment (id, card_id, actor, body_md, created_at) VALUES ('m1', 'k1', 'agent:x', 'read /TR/knowledge/a', 1)`, nil},
		{`INSERT INTO knowledge (id, project_id, slug, title, template, recap, recap_hash, content_hash, mtime, size, created_at, updated_at)
		  VALUES ('e1', 'p1', 'a', 'A', 'runbook', 'do A', ?, ?, 1, 1, 1, 1)`, []any{sha(entryBefore), sha(entryBefore)}},
		{`INSERT INTO knowledge (id, project_id, slug, title, template, content_hash, mtime, size, global, review_by, reviewed_at, created_at, updated_at)
		  VALUES ('e2', 'p1', 'g', 'G', 'decision', 'x', 1, 1, 1, 5, 4, 1, 1)`, nil},
		{`INSERT INTO pin (id, knowledge_id, created_at) VALUES ('pn1', 'e1', 1)`, nil},
		{`INSERT INTO nomination (id, knowledge_id, actor, reason, created_at) VALUES ('nm1', 'e1', 'agent:x', 'r', 1)`, nil},
		{`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('card', 'k1', 'doc', 'e1', '/TR/knowledge/a', 'documents')`, nil},
		{`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('doc', 'e1', 'doc', NULL, '/TR/knowledge/b', 'wikilink')`, nil},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action) VALUES (1, 'h', 'knowledge', 'e2', 'escalated')`, nil},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action) VALUES (1, 'h', 'card', 'k1', 'unarchived')`, nil},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value) VALUES (1, 'h', 'card', 'k1', 'stolen', 'owner', 'agent:y')`, nil},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action, field, new_value) VALUES (1, 'h', 'card', 'k1', 'linked', 'documents', '/TR/knowledge/a')`, nil},
		{`INSERT INTO project_config (project_id, key, value, updated_at) VALUES ('p1', 'lease.ttl', '10m', 1)`, nil},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	return root, db
}

var retiredSchema = regexp.MustCompile(`(?i)knowledge|\bdoc_(type|id)\b|\bowner\b|lease_until|review_by|reviewed_at`)

func TestVocabularyMigration(t *testing.T) {
	root, db := rootBefore(t)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(root, "projects", "TR", "vault", "a.md")
	got := readFile(t, moved)
	want := strings.ReplaceAll(entryBefore, "/TR/knowledge/", "/TR/vault/")
	if got != want {
		t.Errorf("entry file:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(got, "/home/me/knowledge/notes") {
		t.Error("a filesystem path inside a body was rewritten")
	}
	if r := readFile(t, filepath.Join(root, "projects", "TR", "vault", ".a.md", "1.md")); !strings.Contains(r, "/TR/knowledge/b") {
		t.Errorf("a revision was rewritten: %q", r)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "TR", "knowledge")); !os.IsNotExist(err) {
		t.Errorf("old project vault still there: %v", err)
	}
	if g := readFile(t, filepath.Join(root, "global", "vault", "g.md")); !strings.Contains(g, "See /TR/vault/a") {
		t.Errorf("global entry: %q", g)
	}
	if tpl := readFile(t, filepath.Join(root, "templates", "cited.md")); !strings.Contains(tpl, "\nresolve: [sources]\n") {
		t.Errorf("template: %q", tpl)
	}
	if cfg := readFile(t, filepath.Join(root, "config.yaml")); cfg != "claim:\n  ttl: 30m\n" {
		t.Errorf("config.yaml: %q", cfg)
	}

	var objects []struct {
		Name string `db:"name"`
		SQL  string `db:"sql"`
	}
	if err := db.Select(&objects, `SELECT name, COALESCE(sql, '') AS sql FROM sqlite_master`); err != nil {
		t.Fatal(err)
	}
	for _, o := range objects {
		if retiredSchema.MatchString(o.Name) || retiredSchema.MatchString(o.SQL) {
			t.Errorf("schema object %s still uses a retired word:\n%s", o.Name, o.SQL)
		}
	}

	var row struct {
		Template    string `db:"template"`
		ContentHash string `db:"content_hash"`
		RecapHash   string `db:"recap_hash"`
	}
	if err := db.Get(&row, `SELECT template, content_hash, recap_hash FROM entry WHERE id = 'e1'`); err != nil {
		t.Fatal(err)
	}
	if row.Template != "runbook" || row.ContentHash != sha(want) || row.RecapHash != sha(want) {
		t.Errorf("entry row = %+v; want template runbook and both hashes %s", row, sha(want))
	}
	var verifyBy int
	if err := db.Get(&verifyBy, `SELECT verify_by FROM entry WHERE id = 'e2'`); err != nil || verifyBy != 5 {
		t.Errorf("verify_by = %d, %v", verifyBy, err)
	}

	checks := []struct{ q, want string }{
		{`SELECT claimed_by || '/' || claim_until FROM card WHERE id = 'k1'`, "agent:x/99"},
		{`SELECT body_md FROM card WHERE id = 'k1'`, "see /TR/vault/a"},
		{`SELECT body_md FROM comment WHERE id = 'm1'`, "read /TR/vault/a"},
		{`SELECT group_concat(x, ' ') FROM (SELECT from_type || '>' || to_type || ':' || rel || ':' || to_raw AS x
		   FROM link ORDER BY from_type)`,
			"card>entry:cites:/TR/vault/a entry>entry:wikilink:/TR/vault/b"},
		{`SELECT group_concat(x, ' ') FROM (SELECT entity_type || ':' || action || ':' || COALESCE(field, '') AS x
		   FROM event ORDER BY seq)`,
			"entry:promoted: card:restored: card:stolen:claimed_by card:linked:cites"},
		{`SELECT entry_id FROM pin`, "e1"},
		{`SELECT entry_id FROM nomination`, "e1"},
		{`SELECT key FROM project_config`, "claim.ttl"},
		// History keeps its words: an event's values are not rewritten.
		{`SELECT new_value FROM event WHERE action = 'linked'`, "/TR/knowledge/a"},
	}
	for _, c := range checks {
		var got string
		if err := db.Get(&got, c.q); err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
		if got != c.want {
			t.Errorf("%s\n got %q\nwant %q", c.q, got, c.want)
		}
	}
}

func TestVocabularyMigrationRefusesTwoVaults(t *testing.T) {
	root, db := rootBefore(t)
	writeFile(t, filepath.Join(root, "projects", "TR", "vault", "stray.md"), "x")
	err := goose.Up(db.DB, "migrations")
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("want a 'both … exist' error, got %v", err)
	}
	if readFile(t, filepath.Join(root, "projects", "TR", "knowledge", "a.md")) != entryBefore {
		t.Error("the entry changed although the migration refused")
	}
}

func TestVocabularyMigrationPutsFilesBackWhenTheSchemaFails(t *testing.T) {
	root, db := rootBefore(t)
	mustExec(t, db, `CREATE TABLE entry (x INTEGER)`) // makes the table rename fail
	if err := goose.Up(db.DB, "migrations"); err == nil {
		t.Fatal("want the schema step to fail")
	}
	got := readFile(t, filepath.Join(root, "projects", "TR", "knowledge", "a.md"))
	if got != entryBefore {
		t.Errorf("entry not restored:\n%s", got)
	}
	if tpl := readFile(t, filepath.Join(root, "templates", "cited.md")); !strings.Contains(tpl, "\nverify: [sources]\n") {
		t.Errorf("template not restored: %q", tpl)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "TR", "vault")); !os.IsNotExist(err) {
		t.Errorf("new vault directory left behind: %v", err)
	}
}

// Keys may carry hyphens (vpath's key grammar), as trellis init makes them.
// Inside a wikilink, or in a link target, a lower-case key is still a
// reference; anywhere else only an upper-case one is.
const hyphenEntryBefore = "---\ntitle: H\nsources:\n  - /KIOSK-ANALYSE/knowledge/x\n  - \"[[/api/knowledge/x]]\"\n---\n" +
	"See [[/MY-APP/knowledge/b]], [[ /api/knowledge/x#why|x]], [[/home/me/knowledge/notes]] and /home/me/knowledge/notes.\n" +
	"A bare /api/knowledge/x is prose.\n"

const hyphenEntryAfter = "---\ntitle: H\nsources:\n  - /KIOSK-ANALYSE/vault/x\n  - \"[[/api/vault/x]]\"\n---\n" +
	"See [[/MY-APP/vault/b]], [[ /api/vault/x#why|x]], [[/home/me/knowledge/notes]] and /home/me/knowledge/notes.\n" +
	"A bare /api/knowledge/x is prose.\n"

func TestVocabularyMigrationRewritesHyphenatedAndLowerCaseKeys(t *testing.T) {
	root, db := rootBefore(t)
	writeFile(t, filepath.Join(root, "projects", "MY-APP", "knowledge", "h.md"), hyphenEntryBefore)
	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES ('p2', 'MY-APP', 'My app', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b2', 'p2', 'main', 'main', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c2', 'b2', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, body_md, ref, created_at, updated_at)
		 VALUES ('k2', 'p2', 'b2', 1, 'c2', 'a', 'Second', 'see /MY-APP/knowledge/b', 'MY-APP-1', 1, 1)`,
		`INSERT INTO comment (id, card_id, actor, body_md, created_at) VALUES ('m2', 'k2', 'agent:x', 'read /MY-APP/knowledge/b', 1)`,
		`INSERT INTO knowledge (id, project_id, slug, title, content_hash, mtime, size, created_at, updated_at)
		 VALUES ('e3', 'p2', 'h', 'H', '` + sha(hyphenEntryBefore) + `', 1, 1, 1, 1)`,
		`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('card', 'k2', 'doc', 'e3', '/MY-APP/knowledge/h', 'documents')`,
		`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('doc', 'e3', 'doc', NULL, '/api/knowledge/x#why', 'wikilink')`,
		`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('doc', 'e3', 'doc', NULL, '/home/me/knowledge/notes', 'wikilink')`,
	} {
		mustExec(t, db, q)
	}
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(root, "projects", "MY-APP", "vault", "h.md")); got != hyphenEntryAfter {
		t.Errorf("entry file:\n%s\nwant:\n%s", got, hyphenEntryAfter)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "MY-APP", "knowledge")); !os.IsNotExist(err) {
		t.Errorf("old MY-APP vault still there: %v", err)
	}
	checks := []struct{ q, want string }{
		{`SELECT body_md FROM card WHERE id = 'k2'`, "see /MY-APP/vault/b"},
		{`SELECT body_md FROM comment WHERE id = 'm2'`, "read /MY-APP/vault/b"},
		{`SELECT group_concat(to_raw, ' ') FROM (SELECT to_raw FROM link WHERE from_id IN ('k2', 'e3') ORDER BY to_raw)`,
			"/MY-APP/vault/h /api/vault/x#why /home/me/knowledge/notes"},
		{`SELECT content_hash FROM entry WHERE id = 'e3'`, sha(hyphenEntryAfter)},
	}
	for _, c := range checks {
		var got string
		if err := db.Get(&got, c.q); err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
		if got != c.want {
			t.Errorf("%s\n got %q\nwant %q", c.q, got, c.want)
		}
	}
}

// linkVault moves TR's vault directory out of root and leaves a symlink in
// its place, returning where the directory went. That name starts with a
// dot, as a hidden directory's does: it is still the vault, not a revision
// directory. The test is skipped where symlinks need a privilege it lacks.
func linkVault(t *testing.T, root string) string {
	t.Helper()
	link := filepath.Join(root, "projects", "TR", "knowledge")
	target := filepath.Join(t.TempDir(), ".entries")
	if err := os.Rename(link, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	return target
}

func isSymlink(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode()&fs.ModeSymlink != 0
}

// A vault directory may be a symlink to one kept elsewhere. The move renames
// the link; the rewrite must still reach the entries behind it and carry
// their hashes, or every rewritten address would read as a diagnostic.
func TestVocabularyMigrationFollowsASymlinkedVault(t *testing.T) {
	root, db := rootBefore(t)
	target := linkVault(t, root)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}

	if !isSymlink(filepath.Join(root, "projects", "TR", "vault")) {
		t.Fatal("the vault link was not moved")
	}
	want := strings.ReplaceAll(entryBefore, "/TR/knowledge/", "/TR/vault/")
	if got := readFile(t, filepath.Join(target, "a.md")); got != want {
		t.Errorf("entry behind the link:\n%s\nwant:\n%s", got, want)
	}
	if r := readFile(t, filepath.Join(target, ".a.md", "1.md")); !strings.Contains(r, "/TR/knowledge/b") {
		t.Errorf("a revision behind the link was rewritten: %q", r)
	}
	var row struct {
		ContentHash string `db:"content_hash"`
		RecapHash   string `db:"recap_hash"`
	}
	if err := db.Get(&row, `SELECT content_hash, recap_hash FROM entry WHERE id = 'e1'`); err != nil {
		t.Fatal(err)
	}
	if row.ContentHash != sha(want) || row.RecapHash != sha(want) {
		t.Errorf("entry row = %+v; want both hashes %s", row, sha(want))
	}

	// The file work, repeated, finds nothing left to do behind the link.
	var fw fileWork
	if err := fw.moveVaults(root); err != nil {
		t.Fatalf("moveVaults again: %v", err)
	}
	if err := fw.rewrite(root); err != nil {
		t.Fatalf("rewrite again: %v", err)
	}
	if len(fw.moved) != 0 || len(fw.rewritten) != 0 {
		t.Errorf("second file pass moved %v and rewrote %d files", fw.moved, len(fw.rewritten))
	}
}

func TestVocabularyMigrationPutsASymlinkedVaultBack(t *testing.T) {
	root, db := rootBefore(t)
	target := linkVault(t, root)
	mustExec(t, db, `CREATE TABLE entry (x INTEGER)`) // makes the table rename fail
	if err := goose.Up(db.DB, "migrations"); err == nil {
		t.Fatal("want the schema step to fail")
	}
	if got := readFile(t, filepath.Join(target, "a.md")); got != entryBefore {
		t.Errorf("entry behind the link not restored:\n%s", got)
	}
	if !isSymlink(filepath.Join(root, "projects", "TR", "knowledge")) {
		t.Error("the vault link was not put back")
	}
	if _, err := os.Lstat(filepath.Join(root, "projects", "TR", "vault")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("new vault link left behind: %v", err)
	}
}

// A root is a path, not a pattern: '[' in it must not hide the vaults.
func TestVocabularyMigrationTakesARootLiterally(t *testing.T) {
	root, db := rootBeforeAt(t, filepath.Join(t.TempDir(), "home[1]"))
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, "projects", "TR", "vault", "a.md")); !strings.Contains(got, "[[/TR/vault/b]]") {
		t.Errorf("entry file not moved and rewritten: %q", got)
	}
	if tpl := readFile(t, filepath.Join(root, "templates", "cited.md")); !strings.Contains(tpl, "\nresolve: [sources]\n") {
		t.Errorf("template: %q", tpl)
	}
}

// goose records the version after the migration returns, so a start can run
// it again over work already done. That must change nothing.
func TestVocabularyMigrationIsSafeToRerun(t *testing.T) {
	root, db := rootBefore(t)
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatal(err)
	}
	files, rows := filesUnder(t, root), tableRows(t, db)

	if err := renameVocabulary(t.Context(), db.DB); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up again: %v", err)
	}
	// The file work alone, as a start that died before the schema step
	// repeats it, finds nothing left to do.
	var fw fileWork
	if err := fw.moveVaults(root); err != nil {
		t.Fatalf("moveVaults again: %v", err)
	}
	if err := fw.rewrite(root); err != nil {
		t.Fatalf("rewrite again: %v", err)
	}
	if len(fw.moved) != 0 || len(fw.rewritten) != 0 {
		t.Errorf("second file pass moved %v and rewrote %d files", fw.moved, len(fw.rewritten))
	}

	if got := filesUnder(t, root); !maps.Equal(got, files) {
		t.Errorf("files changed on rerun:\n%v\nwant:\n%v", got, files)
	}
	if got := tableRows(t, db); got != rows {
		t.Errorf("database changed on rerun:\n%s\nwant:\n%s", got, rows)
	}
}

// filesUnder maps every file below root, the database aside, to its contents.
func filesUnder(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), "trellis.db") {
			return err
		}
		out[path] = readFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// tableRows prints the schema and every row the migration touches.
func tableRows(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	var b strings.Builder
	for _, q := range []string{
		`SELECT type, name, COALESCE(sql, '') FROM sqlite_master ORDER BY name`,
		`SELECT * FROM entry ORDER BY rowid`, `SELECT * FROM card ORDER BY rowid`,
		`SELECT * FROM comment ORDER BY rowid`, `SELECT * FROM link ORDER BY rowid`,
		`SELECT * FROM event ORDER BY rowid`, `SELECT * FROM pin ORDER BY rowid`,
		`SELECT * FROM nomination ORDER BY rowid`, `SELECT * FROM project_config ORDER BY rowid`,
	} {
		rows, err := db.Queryx(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		for rows.Next() {
			row, err := rows.SliceScan()
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(&b, row...)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}
