package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationNoTxContext("0014_memory_groundwork.go", upMemoryGroundwork, downMemoryGroundwork)
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
//   - a card's notes become comments.
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
//
// knowledge.template keeps the column default "note" that doc_type had;
// changing a default needs a table rebuild, and Trellis always writes the
// value.
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

	now := time.Now().UnixMilli()
	steps := []struct {
		q    string
		args []any
	}{
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

		// A template is the only classification; "note" was "none".
		{`ALTER TABLE knowledge RENAME COLUMN doc_type TO template`, nil},
		{`UPDATE knowledge SET template = '' WHERE template = 'note'`, nil},

		// A card's notes are comments.
		{`ALTER TABLE note RENAME TO comment`, nil},
		{`DROP INDEX note_card`, nil},
		{`CREATE INDEX comment_card ON comment(card_id, created_at)`, nil},
		{`UPDATE event SET entity_type = 'comment' WHERE entity_type = 'note'`, nil},
	}
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return fmt.Errorf("%s: %w", firstLine(s.q), err)
		}
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
func downMemoryGroundwork(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`UPDATE event SET entity_type = 'note' WHERE entity_type = 'comment'`,
		`DROP INDEX comment_card`,
		`ALTER TABLE comment RENAME TO note`,
		`CREATE INDEX note_card ON note(card_id, created_at)`,
		`UPDATE knowledge SET template = 'note' WHERE template = ''`,
		`ALTER TABLE knowledge RENAME COLUMN template TO doc_type`,
		`DROP INDEX link_card_card`,
		`DROP INDEX pin_project_wide`,
		`DROP TABLE merged_project`,
		`DROP INDEX card_ref`,
		`ALTER TABLE card DROP COLUMN ref`,
		`DROP INDEX event_project_seq`,
		`ALTER TABLE event DROP COLUMN project_id`,
		`DROP TABLE event_consumer`,
		`ALTER TABLE project ADD COLUMN identity_kind TEXT NOT NULL DEFAULT 'pin'`,
		`ALTER TABLE project ADD COLUMN identity_value TEXT`,
		`ALTER TABLE project ADD COLUMN root_path TEXT`,
		`CREATE INDEX project_identity ON project(identity_value)`,
		`CREATE UNIQUE INDEX project_root ON project(root_path)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	return tx.Commit()
}
