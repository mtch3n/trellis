package importer

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
	"github.com/mtch3n/trellis/internal/platform"
	_ "modernc.org/sqlite"
)

type clientHome struct {
	t   *testing.T
	dir string
	db  *sqlx.DB
}

func newClientHome(t *testing.T) *clientHome {
	t.Helper()
	dir := t.TempDir()
	schema, err := os.ReadFile(filepath.Join("testdata", "local.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlx.Open("sqlite", filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	return &clientHome{t: t, dir: dir, db: db}
}

func (h *clientHome) exec(q string, args ...any) {
	h.t.Helper()
	if _, err := h.db.Exec(q, args...); err != nil {
		h.t.Fatalf("%s: %v", q, err)
	}
}

func (h *clientHome) file(rel, content string) {
	h.t.Helper()
	path := filepath.Join(h.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *clientHome) project(id, key string) {
	h.exec(`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, 1000)`, id, key, key+" name")
}

func (h *clientHome) board(id, projectID, name string) {
	h.exec(`INSERT INTO board (id, project_id, name, slug, created_at) VALUES (?, ?, ?, ?, 1000)`, id, projectID, name, name)
	h.exec(`INSERT INTO column_ (id, board_id, name, position, is_done) VALUES (?, ?, 'backlog', 0, 0), (?, ?, 'done', 1, 1)`,
		id+"-backlog", id, id+"-done", id)
}

func (h *clientHome) card(id, projectID, boardID, column, ref string, seq int64) {
	h.exec(`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, body_md, priority, version, created_at, updated_at, ref)
		VALUES (?, ?, ?, ?, ?, 'm', ?, ?, 1, 3, 1000, 2000, ?)`,
		id, projectID, boardID, seq, boardID+"-"+column, "title "+ref, "body "+ref, ref)
}

func entryFile(title, summary, body string) string {
	return "---\ntitle: " + title + "\nsummary: " + summary + "\nprovenance: authored\n---\n\n" + body
}

func (h *clientHome) entry(id, projectID, key, slug string, version int64, body string) {
	h.exec(`INSERT INTO entry (id, project_id, slug, title, template, summary, content_hash, mtime, size, version, created_at, updated_at, provenance)
		VALUES (?, ?, ?, ?, 'note', ?, 'h', 1, 1, ?, 1000, 2000, 'authored')`, id, projectID, slug, "Title "+slug, "sum "+slug, version)
	h.file("projects/"+key+"/vault/"+slug+".md", entryFile("Title "+slug, "sum "+slug, body))
}

func clientServer(t *testing.T) *sqlx.DB {
	t.Helper()
	db := pgtest.Open(t)
	if err := pgstore.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	p := platform.New(db)
	if _, err := p.CreateOrg(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddMember(t.Context(), "acme", "ming", "Ming"); err != nil {
		t.Fatal(err)
	}
	return db
}

func clientRun(t *testing.T, db *sqlx.DB, home string) Report {
	t.Helper()
	rep, err := Run(t.Context(), db, Options{Home: home, Org: "acme", Member: "ming"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return rep
}

func clientStrings(t *testing.T, db *sqlx.DB, q string, args ...any) []string {
	t.Helper()
	var out []string
	if err := db.Select(&out, q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return out
}

func clientCount(t *testing.T, db *sqlx.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Get(&n, q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func clientWant(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

func TestClient_CLIENT_C1_ImportsAProjectWithBoardsCardsEntriesAndLinks(t *testing.T) {
	db := clientServer(t)
	h := newClientHome(t)
	h.project("p1", "DEMO")
	h.board("b1", "p1", "main")
	h.board("b2", "p1", "side")
	h.card("c1", "p1", "b1", "backlog", "DEMO-1", 1)
	h.card("c2", "p1", "b2", "done", "DEMO-2", 2)
	h.exec(`UPDATE card SET claimed_by = 'agent:x', claim_until = 9999999999999 WHERE id = 'c1'`)
	h.exec(`INSERT INTO comment (id, card_id, actor, body_md, created_at) VALUES ('n1', 'c1', 'agent:x', 'first', 1100), ('n2', 'c1', 'agent:y', 'second', 1200)`)
	h.entry("e1", "p1", "DEMO", "alpha", 2, "see [[beta]] again")
	h.file("projects/DEMO/vault/.alpha.md/1.md", entryFile("Title alpha", "sum alpha", "see [[beta]]"))
	h.file("projects/DEMO/vault/.alpha.md/2.md", entryFile("Title alpha", "sum alpha", "see [[beta]] again"))
	h.entry("e2", "p1", "DEMO", "beta", 1, "beta body")
	h.file("projects/DEMO/vault/.beta.md/1.md", entryFile("Title beta", "sum beta", "beta body"))
	h.exec(`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES ('entry', 'e1', 'entry', 'e2', 'beta', 'wikilink')`)
	h.exec(`UPDATE entry SET recap = 'alpha recap' WHERE id = 'e1'`)
	h.exec(`INSERT INTO pin (id, entry_id, board_id, created_at) VALUES ('pin1', 'e1', NULL, 1300)`)
	h.exec(`INSERT INTO nomination (id, entry_id, actor, reason, created_at) VALUES ('nom1', 'e2', 'agent:x', 'worth sharing', 1400)`)
	h.exec(`INSERT INTO artifact (id, project_id, name, kind, mime, size, content_hash, created_at, updated_at)
		VALUES ('a1', 'p1', 'notes.txt', 'text', 'text/plain', 5, 'x', 1000, 1000)`)
	h.file("projects/DEMO/artifacts/notes.txt", "hello")

	rep := clientRun(t, db, h.dir)
	clientWant(t, "imported", rep.Imported, []string{"DEMO"})

	clientWant(t, "boards", clientStrings(t, db, `SELECT b.name || ':' || b.scope || ':' || m.handle FROM board b
		JOIN project p ON p.id = b.project_id JOIN member m ON m.id = b.owner_id
		WHERE p.key = 'DEMO' AND b.name IN ('main', 'side') ORDER BY b.name`),
		[]string{"main:project:ming", "side:project:ming"})
	clientWant(t, "cards", clientStrings(t, db, `SELECT c.ref || ':' || b.name || ':' || col.name || ':' || c.title || ':' || c.body || ':' || c.version
		FROM card c JOIN board b ON b.id = c.board_id JOIN board_column col ON col.id = c.column_id ORDER BY c.ref`),
		[]string{"DEMO-1:main:backlog:title DEMO-1:body DEMO-1:3", "DEMO-2:side:done:title DEMO-2:body DEMO-2:3"})
	if n := clientCount(t, db, `SELECT count(*) FROM card WHERE claimed_by IS NOT NULL OR claim_until IS NOT NULL`); n != 0 {
		t.Fatalf("claimed cards = %d, want 0", n)
	}
	clientWant(t, "comments", clientStrings(t, db, `SELECT c.ref || ':' || cm.body || ':' || m.handle FROM comment cm
		JOIN card c ON c.id = cm.card_id JOIN member m ON m.id = cm.member_id ORDER BY cm.created_at`),
		[]string{"DEMO-1:first:ming", "DEMO-1:second:ming"})
	clientWant(t, "entries", clientStrings(t, db, `SELECT slug || ':' || scope || ':' || version || ':' || body || ':' || summary || ':' || template
		FROM entry ORDER BY slug`),
		[]string{"alpha:project:2:see [[beta]] again:sum alpha:note", "beta:project:1:beta body:sum beta:note"})
	clientWant(t, "revisions", clientStrings(t, db, `SELECT e.slug || ':' || r.version || ':' || r.body FROM entry_revision r
		JOIN entry e ON e.id = r.entry_id ORDER BY e.slug, r.version`),
		[]string{"alpha:1:see [[beta]]", "alpha:2:see [[beta]] again", "beta:1:beta body"})
	clientWant(t, "links", clientStrings(t, db, `SELECT f.slug || ':' || l.rel || ':' || l.target || ':' || t.slug FROM entry_link l
		JOIN entry f ON f.id = l.from_entry_id JOIN entry t ON t.id = l.to_entry_id`),
		[]string{"alpha:wikilink:beta:beta"})
	clientWant(t, "pins", clientStrings(t, db, `SELECT e.slug || ':' || coalesce(p.recap, '') || ':' || (p.board_id IS NULL) FROM pin p JOIN entry e ON e.id = p.entry_id`),
		[]string{"alpha:alpha recap:true"})
	clientWant(t, "nominations", clientStrings(t, db, `SELECT e.slug || ':' || n.reason FROM nomination n JOIN entry e ON e.id = n.entry_id`),
		[]string{"beta:worth sharing"})
	clientWant(t, "artifacts", clientStrings(t, db, `SELECT name || ':' || scope || ':' || convert_from(content, 'UTF8') FROM artifact`),
		[]string{"notes.txt:project:hello"})
}

func TestClient_CLIENT_C1_NewCardsContinueAfterTheImportedRefs(t *testing.T) {
	db := clientServer(t)
	h := newClientHome(t)
	h.project("p1", "DEMO")
	h.board("b1", "p1", "main")
	h.card("c7", "p1", "b1", "backlog", "DEMO-7", 7)
	clientRun(t, db, h.dir)
	if n := clientCount(t, db, `SELECT card_counter FROM project WHERE key = 'DEMO'`); n != 7 {
		t.Fatalf("card_counter = %d, want 7", n)
	}
	if !strings.Contains(strings.Join(clientStrings(t, db, `SELECT m.handle FROM project_member pm JOIN member m ON m.id = pm.member_id`), ","), "ming") {
		t.Fatal("ming is not a member of DEMO")
	}
}

func TestClient_CLIENT_C1_CarriesBlockersCitesAndArtifactLinks(t *testing.T) {
	db := clientServer(t)
	h := newClientHome(t)
	h.project("p1", "DEMO")
	h.board("b1", "p1", "main")
	h.card("c1", "p1", "b1", "backlog", "DEMO-1", 1)
	h.card("c2", "p1", "b1", "backlog", "DEMO-2", 2)
	h.entry("e1", "p1", "DEMO", "alpha", 1, "alpha body")
	h.exec(`INSERT INTO artifact (id, project_id, name, kind, mime, size, content_hash, created_at, updated_at)
		VALUES ('a1', 'p1', 'notes.txt', 'text', 'text/plain', 5, 'x', 1000, 1000)`)
	h.file("projects/DEMO/artifacts/notes.txt", "hello")
	h.exec(`INSERT INTO link (from_type, from_id, to_type, to_id, to_raw, rel) VALUES
		('card', 'c2', 'card', 'c1', 'DEMO-1', 'blocked_by'),
		('card', 'c1', 'entry', 'e1', 'alpha', 'cites'),
		('card', 'c1', 'artifact', 'a1', 'notes.txt', 'artifact'),
		('entry', 'e1', 'artifact', 'a1', 'notes.txt', 'artifact')`)
	clientRun(t, db, h.dir)
	clientWant(t, "blockers", clientStrings(t, db, `SELECT c.ref || '<' || b.ref FROM blocker bl JOIN card c ON c.id = bl.card_id JOIN card b ON b.id = bl.blocker_id`),
		[]string{"DEMO-2<DEMO-1"})
	clientWant(t, "cites", clientStrings(t, db, `SELECT c.ref || ':' || e.slug FROM card_entry ce JOIN card c ON c.id = ce.card_id JOIN entry e ON e.id = ce.entry_id`),
		[]string{"DEMO-1:alpha"})
	clientWant(t, "artifact links", clientStrings(t, db, `SELECT coalesce(c.ref, e.slug) FROM artifact_link al
		LEFT JOIN card c ON c.id = al.card_id LEFT JOIN entry e ON e.id = al.entry_id ORDER BY al.card_id IS NULL`),
		[]string{"DEMO-1", "alpha"})
}

func TestClient_CLIENT_C2_KeepsGlobalProjectAndPrivateScopes(t *testing.T) {
	db := clientServer(t)
	h := newClientHome(t)
	h.project("p1", "DEMO")
	h.entry("e1", "p1", "DEMO", "shared", 1, "project body")
	h.entry("e2", "p1", "DEMO", "secret", 1, "private body")
	h.exec(`UPDATE entry SET private = 1 WHERE id = 'e2'`)
	h.exec(`INSERT INTO entry (id, project_id, slug, title, summary, content_hash, mtime, size, global, verify_by, version, created_at, updated_at)
		VALUES ('g1', 'p1', 'wide', 'Wide', 'sum wide', 'h', 1, 1, 1, 1893456000000, 1, 1000, 2000)`)
	h.file("global/vault/wide.md", entryFile("Wide", "sum wide", "global body"))

	clientRun(t, db, h.dir)
	clientWant(t, "entries", clientStrings(t, db, `SELECT slug || ':' || scope || ':' || private || ':' || array_to_string(labels, ',') || ':' ||
		coalesce(to_char(verify_by AT TIME ZONE 'UTC', 'YYYY-MM-DD'), '-') || ':' || body FROM entry ORDER BY slug`),
		[]string{"secret:personal:true::-:private body", "shared:project:false:import:-:project body", "wide:global:false::2030-01-01:global body"})
}

func TestClient_CLIENT_C3_LeavesOutEventsLabelsTagsAndTrashAndCountsThem(t *testing.T) {
	db := clientServer(t)
	h := newClientHome(t)
	h.project("p1", "DEMO")
	h.board("b1", "p1", "main")
	h.card("c1", "p1", "b1", "backlog", "DEMO-1", 1)
	h.entry("e1", "p1", "DEMO", "alpha", 1, "alpha body")
	h.exec(`INSERT INTO label (id, project_id, name, description, created_at) VALUES ('l1', 'p1', 'bug', 'a bug', 1000), ('l2', 'p1', 'chore', '', 1000)`)
	h.exec(`INSERT INTO card_label (card_id, label_id) VALUES ('c1', 'l1')`)
	h.exec(`INSERT INTO entry_label (entry_id, label_id) VALUES ('e1', 'l1')`)
	h.exec(`INSERT INTO tag (id, project_id, name, created_at) VALUES ('t1', 'p1', 'red', 1000)`)
	h.exec(`INSERT INTO entry_tag (entry_id, tag_id) VALUES ('e1', 't1')`)
	h.exec(`INSERT INTO event (ts, actor, entity_type, entity_id, action, project_id) VALUES (1, 'agent:x', 'card', 'c1', 'created', 'p1'),
		(2, 'agent:x', 'card', 'c1', 'moved', 'p1'), (3, 'agent:x', 'entry', 'e1', 'created', 'p1')`)
	h.exec(`INSERT INTO trash (id, project_id, project_key, kind, item_id, name, title, seq, trashed_at, trashed_by, rows)
		VALUES ('tr1', 'p1', 'DEMO', 'card', 'c9', 'DEMO-9', 'gone card', 9, 1000, 'agent:x', '[]')`)
	before := clientCount(t, db, `SELECT count(*) FROM event`)

	rep := clientRun(t, db, h.dir)
	if after := clientCount(t, db, `SELECT count(*) FROM event`); after != before {
		t.Fatalf("events = %d, want %d", after, before)
	}
	clientWant(t, "labels and tags", clientStrings(t, db, `SELECT slug || ':' || array_to_string(labels, ',') || ':' || array_to_string(tags, ',') FROM entry`),
		[]string{"alpha:import:"})
	if n := clientCount(t, db, `SELECT count(*) FROM card WHERE ref = 'DEMO-9' OR title = 'gone card'`); n != 0 {
		t.Fatalf("trashed cards = %d, want 0", n)
	}
	for kind, want := range map[string]int64{"events": 3, "labels": 2, "tags": 1, "trash": 1, "vectors": 0} {
		if rep.LeftOut[kind] != want {
			t.Errorf("left out %s = %d, want %d", kind, rep.LeftOut[kind], want)
		}
	}
	text := rep.Text()
	for _, line := range []string{"3 events", "2 labels", "1 tags", "1 trash"} {
		if !strings.Contains(text, line) {
			t.Errorf("output %q does not say %q", text, line)
		}
	}
}

func clientSnapshot(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	var s string
	if err := db.Get(&s, `SELECT concat_ws(',', (SELECT count(*) FROM project), (SELECT count(*) FROM project_member), (SELECT count(*) FROM board),
		(SELECT count(*) FROM card), (SELECT count(*) FROM comment), (SELECT count(*) FROM entry), (SELECT count(*) FROM entry_revision),
		(SELECT count(*) FROM artifact), (SELECT count(*) FROM pin), (SELECT count(*) FROM nomination), (SELECT count(*) FROM entry_link))`); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestClient_CLIENT_C4_RefusesADirectoryWithNoTrellisDB(t *testing.T) {
	db := clientServer(t)
	dir := t.TempDir()
	before := clientSnapshot(t, db)
	_, err := Run(t.Context(), db, Options{Home: dir, Org: "acme", Member: "ming"})
	e, ok := errors.AsType[*platform.Error](err)
	if !ok || e.Kind != platform.KindUsage || !strings.Contains(e.Msg, dir) {
		t.Fatalf("err = %v, want a usage error naming %s", err, dir)
	}
	if after := clientSnapshot(t, db); after != before {
		t.Fatalf("server rows %s, want %s", after, before)
	}
}
