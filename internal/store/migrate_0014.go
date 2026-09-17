package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationNoTxContext("0014_memory_groundwork.go", upMemoryGroundwork, downMemoryGroundwork)
}

// rawStep is one statement a migration runs, in order.
type rawStep struct {
	q    string
	args []any
}

// execSteps runs every step against tx, in order, naming the failing
// statement's first line in the error so a broken migration is easy to place.
func execSteps(ctx context.Context, tx *sql.Tx, steps []rawStep) error {
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return fmt.Errorf("%s: %w", firstLine(s.q), err)
		}
	}
	return nil
}

// tableAuxiliaries returns the CREATE statements sqlite_master records for
// every index, trigger and view attached to table, in catalog order, so a
// rebuild that drops and recreates the table (SQLite has no ALTER TABLE DROP
// COLUMN when a UNIQUE constraint or a trigger is involved) can put them back
// afterward. A constraint declared inline in CREATE TABLE -- an automatic
// index -- has no sql text of its own here; the new table's own definition
// recreates it.
func tableAuxiliaries(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type IN ('index', 'trigger', 'view') AND sql IS NOT NULL`,
		table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stmts []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
	}
	return stmts, rows.Err()
}

// auxSteps wraps recreate statements as rawSteps with no arguments.
func auxSteps(stmts []string) []rawStep {
	out := make([]rawStep, len(stmts))
	for i, s := range stmts {
		out[i] = rawStep{q: s}
	}
	return out
}

// mainDatabasePath returns the file backing the connection's "main" schema --
// always <root>/trellis.db -- so the storage root a path is derived against
// is read from the database itself, never guessed.
func mainDatabasePath(ctx context.Context, tx *sql.Tx) (string, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name == "main" {
			return file, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no main database attached")
}

// upMemoryGroundwork is everything the memory-groundwork release changes after
// card revisions, as one step that lands whole or not at all:
//
//   - project loses identity_kind, identity_value and root_path: a .trellis
//     pin is now the only link from a directory to a project;
//   - event_consumer, a named durable cursor into the event feed;
//   - event.project_id, so a project's feed reaches deleted entities;
//   - card.ref as stored data, and merged_project for retired keys;
//   - one project-wide pin per entry, and one card-to-card link per relation;
//   - knowledge.doc_type becomes template, with no "note" default;
//   - a card's notes become comments;
//   - knowledge and artifact lose path (TRELLIS-36): the file location is
//     derived from the storage root, the project key and the slug or name,
//     never stored, so a copied or moved TRELLIS_HOME reads its own files
//     instead of the original's.
//
// root_path is UNIQUE, which SQLite's DROP COLUMN refuses, so project is
// rebuilt -- and a rebuild is where this migration could destroy the
// database. Every connection runs with foreign_keys on, and dropping a parent
// table under enforcement is an implicit DELETE that cascades into board,
// card, knowledge and everything else a project owns. PRAGMA foreign_keys is
// a no-op inside a transaction, so this migration runs without goose's
// transaction: it turns enforcement off on one pinned connection, makes every
// change inside its own transaction, and commits only once foreign_key_check
// is empty. goose's SQL runner cannot do that last step; it never reads the
// rows a PRAGMA returns.
//
// The old project bindings go to the event log first. Migrations run on
// whatever command opens the database, so nobody gets to copy them down
// beforehand.
func upMemoryGroundwork(ctx context.Context, db *sql.DB) (err error) {
	// goose records the version after this returns, outside the transaction
	// below. A process that dies in between leaves everything committed and
	// the version unrecorded, so the next start runs this again: succeed.
	var applied int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'event_consumer'`).Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		if _, onErr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`); onErr != nil {
			err = errors.Join(err, onErr)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // a no-op once committed

	// Captured before either table is dropped, so the rebuilds below can put
	// every named index and trigger back afterward.
	knowledgeAux, err := tableAuxiliaries(ctx, tx, "knowledge")
	if err != nil {
		return err
	}
	artifactAux, err := tableAuxiliaries(ctx, tx, "artifact")
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	steps := []rawStep{
		// Project identity: record, then rebuild without it.
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value)
		  SELECT ?, 'migration', 'project', id, 'unbound', 'root_path', root_path
		  FROM project WHERE root_path IS NOT NULL AND root_path <> ''`, []any{now}},
		{`INSERT INTO event (ts, actor, entity_type, entity_id, action, field, old_value)
		  SELECT ?, 'migration', 'project', id, 'unbound', 'identity', identity_kind || ':' || identity_value
		  FROM project WHERE identity_value IS NOT NULL AND identity_value <> ''`, []any{now}},
		{`CREATE TABLE project_new (
		      id         TEXT PRIMARY KEY,
		      key        TEXT NOT NULL UNIQUE,
		      name       TEXT NOT NULL,
		      created_at INTEGER NOT NULL
		  )`, nil},
		{`INSERT INTO project_new (id, key, name, created_at) SELECT id, key, name, created_at FROM project`, nil},
		{`DROP TABLE project`, nil},
		{`ALTER TABLE project_new RENAME TO project`, nil},

		// Event consumers. Reading never advances a cursor; only an ack does,
		// and an ack never moves it backwards: at-least-once delivery.
		{`CREATE TABLE event_consumer (
		      name       TEXT PRIMARY KEY,
		      cursor     INTEGER NOT NULL DEFAULT 0,
		      created_at INTEGER NOT NULL,
		      updated_at INTEGER NOT NULL
		  )`, nil},

		// event.project_id, backfilled from each entity's live row.
		{`ALTER TABLE event ADD COLUMN project_id TEXT`, nil},
		{`CREATE INDEX event_project_seq ON event(project_id, seq)`, nil},
		{`UPDATE event SET project_id = (SELECT project_id FROM card WHERE card.id = event.entity_id)
		  WHERE entity_type = 'card'`, nil},
		{`UPDATE event SET project_id = (SELECT project_id FROM knowledge WHERE knowledge.id = event.entity_id)
		  WHERE entity_type = 'knowledge'`, nil},
		{`UPDATE event SET project_id = (SELECT project_id FROM board WHERE board.id = event.entity_id)
		  WHERE entity_type = 'board'`, nil},
		{`UPDATE event SET project_id = (SELECT project_id FROM label WHERE label.id = event.entity_id)
		  WHERE entity_type = 'label'`, nil},
		{`UPDATE event SET project_id = (SELECT c.project_id FROM note n JOIN card c ON c.id = n.card_id
		                                 WHERE n.id = event.entity_id)
		  WHERE entity_type = 'note'`, nil},
		{`UPDATE event SET project_id = entity_id
		  WHERE entity_type = 'project' AND entity_id IN (SELECT id FROM project)`, nil},

		// A card's ref is data: after a merge, API-12 lives in MONO and is
		// still API-12. The index is global because a ref's prefix is a key,
		// keys are unique, and a merged key stays reserved.
		{`ALTER TABLE card ADD COLUMN ref TEXT NOT NULL DEFAULT ''`, nil},
		{`UPDATE card SET ref = (SELECT key FROM project WHERE project.id = card.project_id) || '-' || seq`, nil},
		{`CREATE UNIQUE INDEX card_ref ON card(ref)`, nil},
		{`CREATE TABLE merged_project (
		      key       TEXT PRIMARY KEY,
		      into_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
		      merged_at INTEGER NOT NULL
		  )`, nil},

		// link's and pin's UNIQUE constraints include a NULL column, which
		// SQLite never compares equal, so duplicates were stored. Keep one.
		{`DELETE FROM pin WHERE board_id IS NULL AND rowid NOT IN (
		      SELECT MAX(rowid) FROM pin WHERE board_id IS NULL GROUP BY knowledge_id)`, nil},
		{`CREATE UNIQUE INDEX pin_project_wide ON pin (knowledge_id) WHERE board_id IS NULL`, nil},
		{`DELETE FROM link WHERE from_type = 'card' AND to_type = 'card' AND rowid NOT IN (
		      SELECT MIN(rowid) FROM link WHERE from_type = 'card' AND to_type = 'card'
		      GROUP BY from_id, to_id, rel)`, nil},
		{`CREATE UNIQUE INDEX link_card_card ON link (from_id, to_id, rel)
		      WHERE from_type = 'card' AND to_type = 'card'`, nil},

		// knowledge (TRELLIS-36): drop path -- derived from the storage root,
		// the project key and the slug, never stored -- and, since a rebuild
		// is already required, fix template's default. "note" was doc_type's
		// default; a template is the only classification now, and "" means
		// none, the value every write already produces.
		{`CREATE TABLE knowledge_new (
		      id           TEXT PRIMARY KEY,
		      project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
		      board_id     TEXT REFERENCES board(id) ON DELETE SET NULL,
		      slug         TEXT NOT NULL,
		      title        TEXT NOT NULL,
		      template     TEXT NOT NULL DEFAULT '',
		      summary      TEXT NOT NULL DEFAULT '',
		      recap        TEXT,
		      recap_hash   TEXT,
		      content_hash TEXT NOT NULL,
		      mtime        INTEGER NOT NULL,
		      size         INTEGER NOT NULL,
		      global       INTEGER NOT NULL DEFAULT 0,
		      review_by    INTEGER,
		      reviewed_at  INTEGER,
		      version      INTEGER NOT NULL DEFAULT 1,
		      created_at   INTEGER NOT NULL,
		      updated_at   INTEGER NOT NULL,
		      provenance   TEXT NOT NULL DEFAULT '',
		      private      INTEGER NOT NULL DEFAULT 0,
		      UNIQUE (project_id, slug)
		  )`, nil},
		{`INSERT INTO knowledge_new (
		      rowid, id, project_id, board_id, slug, title, template, summary,
		      recap, recap_hash, content_hash, mtime, size, global, review_by,
		      reviewed_at, version, created_at, updated_at, provenance, private
		  )
		  SELECT rowid, id, project_id, board_id, slug, title,
		         CASE WHEN doc_type = 'note' THEN '' ELSE doc_type END,
		         summary, recap, recap_hash, content_hash, mtime, size, global,
		         review_by, reviewed_at, version, created_at, updated_at,
		         provenance, private
		  FROM knowledge`, nil},
		{`DROP TABLE knowledge`, nil},
		{`ALTER TABLE knowledge_new RENAME TO knowledge`, nil},
	}
	steps = append(steps, auxSteps(knowledgeAux)...)
	steps = append(steps,
		// artifact (TRELLIS-36): drop path the same way, and replace
		// UNIQUE(project_id, path) with UNIQUE(project_id, name) -- the two
		// were never different in practice, since path was always derived
		// 1:1 from (project, name).
		rawStep{q: `CREATE TABLE artifact_new (
		      id           TEXT PRIMARY KEY,
		      project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
		      name         TEXT NOT NULL,
		      kind         TEXT NOT NULL,
		      mime         TEXT NOT NULL,
		      size         INTEGER NOT NULL,
		      content_hash TEXT NOT NULL,
		      created_at   INTEGER NOT NULL,
		      updated_at   INTEGER NOT NULL,
		      UNIQUE (project_id, name)
		  )`},
		rawStep{q: `INSERT INTO artifact_new (
		      rowid, id, project_id, name, kind, mime, size, content_hash, created_at, updated_at
		  )
		  SELECT rowid, id, project_id, name, kind, mime, size, content_hash, created_at, updated_at
		  FROM artifact`},
		rawStep{q: `DROP TABLE artifact`},
		rawStep{q: `ALTER TABLE artifact_new RENAME TO artifact`},
	)
	steps = append(steps, auxSteps(artifactAux)...)
	steps = append(steps,
		// A card's notes are comments.
		rawStep{q: `ALTER TABLE note RENAME TO comment`},
		rawStep{q: `DROP INDEX note_card`},
		rawStep{q: `CREATE INDEX comment_card ON comment(card_id, created_at)`},
		rawStep{q: `UPDATE event SET entity_type = 'comment' WHERE entity_type = 'note'`},
	)

	if err := execSteps(ctx, tx, steps); err != nil {
		return err
	}
	if err := foreignKeyCheck(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func firstLine(q string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(q), "\n")
	return line
}

// foreignKeyCheck fails with every violating row named.
func foreignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var broken []string
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		broken = append(broken, fmt.Sprintf("%s row %d references a missing %s", table, rowid.Int64, parent))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(broken) > 0 {
		return fmt.Errorf("foreign key check failed during migration 0014, rolled back: %s",
			strings.Join(broken, "; "))
	}
	return nil
}

// downMemoryGroundwork undoes the schema. The project bindings come back as
// empty columns; their old values are in the event log, not restored.
//
// knowledge and artifact are rebuilt the same way up rebuilt them, so
// dropping them needs the same foreign-key-off treatment: knowledge_label,
// knowledge_tag, pin and nomination all reference knowledge(id) ON DELETE
// CASCADE, and a DROP TABLE under enforcement is an implicit cascading
// DELETE.
func downMemoryGroundwork(ctx context.Context, db *sql.DB) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		if _, onErr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`); onErr != nil {
			err = errors.Join(err, onErr)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	dbFile, err := mainDatabasePath(ctx, tx)
	if err != nil {
		return err
	}
	root := filepath.Dir(dbFile)

	knowledgeAux, err := tableAuxiliaries(ctx, tx, "knowledge")
	if err != nil {
		return err
	}
	artifactAux, err := tableAuxiliaries(ctx, tx, "artifact")
	if err != nil {
		return err
	}

	steps := []rawStep{
		{q: `UPDATE event SET entity_type = 'note' WHERE entity_type = 'comment'`},
		{q: `DROP INDEX comment_card`},
		{q: `ALTER TABLE comment RENAME TO note`},
		{q: `CREATE INDEX note_card ON note(card_id, created_at)`},

		// knowledge: restore doc_type (and its "note" default) and re-add
		// path. path is filled with a per-row placeholder here -- knowledge
		// carries no uniqueness on it, but the value must be non-null before
		// the UPDATE loop below computes the real one -- and every other
		// column round-trips unchanged.
		{q: `CREATE TABLE knowledge_old (
		      id           TEXT PRIMARY KEY,
		      project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
		      board_id     TEXT REFERENCES board(id) ON DELETE SET NULL,
		      slug         TEXT NOT NULL,
		      title        TEXT NOT NULL,
		      path         TEXT NOT NULL,
		      doc_type     TEXT NOT NULL DEFAULT 'note',
		      summary      TEXT NOT NULL DEFAULT '',
		      recap        TEXT,
		      recap_hash   TEXT,
		      content_hash TEXT NOT NULL,
		      mtime        INTEGER NOT NULL,
		      size         INTEGER NOT NULL,
		      global       INTEGER NOT NULL DEFAULT 0,
		      review_by    INTEGER,
		      reviewed_at  INTEGER,
		      version      INTEGER NOT NULL DEFAULT 1,
		      created_at   INTEGER NOT NULL,
		      updated_at   INTEGER NOT NULL,
		      provenance   TEXT NOT NULL DEFAULT '',
		      private      INTEGER NOT NULL DEFAULT 0,
		      UNIQUE (project_id, slug)
		  )`},
		{q: `INSERT INTO knowledge_old (
		      rowid, id, project_id, board_id, slug, title, path, doc_type, summary,
		      recap, recap_hash, content_hash, mtime, size, global, review_by,
		      reviewed_at, version, created_at, updated_at, provenance, private
		  )
		  SELECT rowid, id, project_id, board_id, slug, title, id,
		         CASE WHEN template = '' THEN 'note' ELSE template END,
		         summary, recap, recap_hash, content_hash, mtime, size, global,
		         review_by, reviewed_at, version, created_at, updated_at,
		         provenance, private
		  FROM knowledge`},
		{q: `DROP TABLE knowledge`},
		{q: `ALTER TABLE knowledge_old RENAME TO knowledge`},
	}
	steps = append(steps, auxSteps(knowledgeAux)...)
	steps = append(steps,
		// artifact: restore UNIQUE(project_id, path), re-adding path with the
		// same globally-unique placeholder (its id) so the constraint never
		// sees a collision before the real values land.
		rawStep{q: `CREATE TABLE artifact_old (
		      id           TEXT PRIMARY KEY,
		      project_id   TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
		      name         TEXT NOT NULL,
		      path         TEXT NOT NULL,
		      kind         TEXT NOT NULL,
		      mime         TEXT NOT NULL,
		      size         INTEGER NOT NULL,
		      content_hash TEXT NOT NULL,
		      created_at   INTEGER NOT NULL,
		      updated_at   INTEGER NOT NULL,
		      UNIQUE (project_id, path)
		  )`},
		rawStep{q: `INSERT INTO artifact_old (
		      rowid, id, project_id, name, path, kind, mime, size, content_hash, created_at, updated_at
		  )
		  SELECT rowid, id, project_id, name, id, kind, mime, size, content_hash, created_at, updated_at
		  FROM artifact`},
		rawStep{q: `DROP TABLE artifact`},
		rawStep{q: `ALTER TABLE artifact_old RENAME TO artifact`},
	)
	steps = append(steps, auxSteps(artifactAux)...)
	steps = append(steps,
		rawStep{q: `DROP INDEX link_card_card`},
		rawStep{q: `DROP INDEX pin_project_wide`},
		rawStep{q: `DROP TABLE merged_project`},
		rawStep{q: `DROP INDEX card_ref`},
		rawStep{q: `ALTER TABLE card DROP COLUMN ref`},
		rawStep{q: `DROP INDEX event_project_seq`},
		rawStep{q: `ALTER TABLE event DROP COLUMN project_id`},
		rawStep{q: `DROP TABLE event_consumer`},
		rawStep{q: `ALTER TABLE project ADD COLUMN identity_kind TEXT NOT NULL DEFAULT 'pin'`},
		rawStep{q: `ALTER TABLE project ADD COLUMN identity_value TEXT`},
		rawStep{q: `ALTER TABLE project ADD COLUMN root_path TEXT`},
		// Up recorded each project's binding as an unbound event. A binary
		// from before 0014 looks projects up by these columns and creates a
		// new project when none matches, so put them back, then drop the
		// events: up records them again.
		rawStep{q: `UPDATE project SET
		   identity_kind = COALESCE((SELECT substr(e.old_value, 1, instr(e.old_value, ':') - 1) FROM event e
		       WHERE e.entity_type = 'project' AND e.entity_id = project.id AND e.action = 'unbound'
		         AND e.actor = 'migration' AND e.field = 'identity' ORDER BY e.seq DESC LIMIT 1), identity_kind),
		   identity_value = (SELECT substr(e.old_value, instr(e.old_value, ':') + 1) FROM event e
		       WHERE e.entity_type = 'project' AND e.entity_id = project.id AND e.action = 'unbound'
		         AND e.actor = 'migration' AND e.field = 'identity' ORDER BY e.seq DESC LIMIT 1),
		   root_path = (SELECT e.old_value FROM event e
		       WHERE e.entity_type = 'project' AND e.entity_id = project.id AND e.action = 'unbound'
		         AND e.actor = 'migration' AND e.field = 'root_path' ORDER BY e.seq DESC LIMIT 1)`},
		rawStep{q: `DELETE FROM event WHERE entity_type = 'project' AND action = 'unbound' AND actor = 'migration'`},
		rawStep{q: `CREATE INDEX project_identity ON project(identity_value)`},
		rawStep{q: `CREATE UNIQUE INDEX project_root ON project(root_path)`},
	)

	if err := execSteps(ctx, tx, steps); err != nil {
		return err
	}

	// The real paths, derived the same way Core.docPath and Core.artifactPath
	// build them: a project-scoped entry or artifact lives under
	// <root>/projects/<KEY>/{knowledge,artifacts}/..., a global entry under
	// <root>/global/knowledge/....
	type downDoc struct {
		ID     string
		Slug   string
		Global bool
		Key    string
	}
	var docs []downDoc
	docRows, err := tx.QueryContext(ctx,
		`SELECT k.id, k.slug, k.global, p.key FROM knowledge k JOIN project p ON p.id = k.project_id`)
	if err != nil {
		return err
	}
	for docRows.Next() {
		var d downDoc
		if err := docRows.Scan(&d.ID, &d.Slug, &d.Global, &d.Key); err != nil {
			docRows.Close()
			return err
		}
		docs = append(docs, d)
	}
	if err := docRows.Err(); err != nil {
		return err
	}
	docRows.Close()
	for _, d := range docs {
		dir := filepath.Join(root, "projects", d.Key, "knowledge")
		if d.Global {
			dir = filepath.Join(root, "global", "knowledge")
		}
		path := filepath.Join(dir, filepath.FromSlash(d.Slug)+".md")
		if _, err := tx.ExecContext(ctx, `UPDATE knowledge SET path = ? WHERE id = ?`, path, d.ID); err != nil {
			return err
		}
	}

	type downArtifact struct {
		ID   string
		Name string
		Key  string
	}
	var arts []downArtifact
	artRows, err := tx.QueryContext(ctx,
		`SELECT a.id, a.name, p.key FROM artifact a JOIN project p ON p.id = a.project_id`)
	if err != nil {
		return err
	}
	for artRows.Next() {
		var a downArtifact
		if err := artRows.Scan(&a.ID, &a.Name, &a.Key); err != nil {
			artRows.Close()
			return err
		}
		arts = append(arts, a)
	}
	if err := artRows.Err(); err != nil {
		return err
	}
	artRows.Close()
	for _, a := range arts {
		path := filepath.Join(root, "projects", a.Key, "artifacts", a.Name)
		if _, err := tx.ExecContext(ctx, `UPDATE artifact SET path = ? WHERE id = ?`, path, a.ID); err != nil {
			return err
		}
	}

	if err := foreignKeyCheck(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
