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
	goose.AddNamedMigrationNoTxContext("0013_drop_project_identity.go", dropProjectIdentity, restoreProjectIdentity)
}

// dropProjectIdentity removes identity_kind, identity_value and root_path. A
// .trellis pin is now the only link from a directory to a project.
//
// root_path is UNIQUE, which SQLite's DROP COLUMN refuses, so the table is
// rebuilt -- and a rebuild is where this migration could destroy the
// database. Every connection runs with foreign_keys on, and dropping a parent
// table under enforcement is an implicit DELETE that cascades into board,
// card, knowledge and everything else a project owns. PRAGMA foreign_keys is
// a no-op inside a transaction, so this migration runs without goose's
// transaction: it turns enforcement off on one pinned connection, rebuilds
// inside its own transaction, and commits only once foreign_key_check is
// empty. goose's SQL runner cannot do that last step; it never reads the rows
// a PRAGMA returns.
//
// The old values go to the event log first. Migrations run on whatever
// command opens the database, so nobody gets to copy them down beforehand.
func dropProjectIdentity(ctx context.Context, db *sql.DB) (err error) {
	// goose records the version after this returns, outside the transaction
	// below. A process that dies in between leaves the rebuild committed and
	// the version unrecorded, so the next start runs this again: succeed.
	var remaining int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info('project') WHERE name = 'root_path'`).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
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
	}
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return err
		}
	}
	if err := foreignKeyCheck(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
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
		return fmt.Errorf("foreign key check failed after rebuilding project, rolled back: %s",
			strings.Join(broken, "; "))
	}
	return nil
}

// restoreProjectIdentity re-adds the three columns, empty. The values are in
// the event log, not restored.
func restoreProjectIdentity(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`ALTER TABLE project ADD COLUMN identity_kind TEXT NOT NULL DEFAULT 'pin'`,
		`ALTER TABLE project ADD COLUMN identity_value TEXT`,
		`ALTER TABLE project ADD COLUMN root_path TEXT`,
		`CREATE INDEX project_identity ON project(identity_value)`,
		`CREATE UNIQUE INDEX project_root ON project(root_path)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
