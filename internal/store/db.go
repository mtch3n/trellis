// Package store owns the database connection and its schema.
package store

import (
	"embed"
	"fmt"
	"net/url"
	"os"
	"sync"

	"github.com/gofrs/flock"
	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// goose keeps its base filesystem, logger and dialect as package-level state
// rather than per-call arguments, so it is configured exactly once for the
// whole process instead of on every Open call (which would both race under
// concurrent Opens in the same process and re-do pointless work). This
// guards in-process global state; it has nothing to do with the cross-process
// migration lock below.
var gooseSetup sync.Once

// Open connects to the database, applies the mandatory pragmas and brings the
// schema up to date. It is safe to call concurrently from several processes.
func Open(path string) (*sqlx.DB, error) {
	// goose v3 ships a SessionLocker/Locker for Postgres and MySQL, but not
	// for SQLite. Without one, its version-table bootstrap
	// (tryEnsureVersionTable) checks "does the table exist" as one
	// statement and, if not, creates it in a separate write transaction --
	// two processes racing a brand-new database file can both observe "no"
	// before either takes SQLite's write lock, so the loser's CREATE TABLE
	// genuinely fails once it does get the lock. _txlock=immediate does not
	// cover this: it only makes a single write transaction take the lock up
	// front, it cannot make goose's own check-then-write sequence atomic.
	// The very first connection to a brand-new file has the same shape of
	// problem even before goose runs -- converting a fresh file to
	// journal_mode=WAL is itself a first-writer race, observed directly as
	// SQLITE_BUSY out of sqlx.Connect under N-way process contention, before
	// our own busy_timeout pragma has had a chance to apply. So we take our
	// own exclusive file lock around the whole open sequence -- connect,
	// goose setup and goose.Up -- on a sidecar file next to the database,
	// rather than around goose.Up alone. Do not remove this lock as
	// "redundant" with the pragmas above -- the races it prevents live
	// inside goose's own bootstrap and in SQLite's first-connection path,
	// not in our DSN. gofrs/flock makes this a single cross-platform code
	// path: flock(2) on Linux/macOS, LockFileEx on Windows, both blocking
	// and both released by the OS if the holding process dies, so a crash
	// can never strand the lock. The sidecar path is a plain suffix on an
	// already-resolved path, not a joined component, so it needs no
	// separator handling of its own.
	lock := flock.New(path + ".migrate.lock")
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("acquiring migration lock: %w", err)
	}
	defer lock.Unlock()

	db, err := connect(path)
	if err != nil {
		return nil, err
	}
	if err := goose.Up(db.DB, "migrations"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// OpenCurrent connects to an existing database that is already at this
// binary's schema version. It never creates a file or migrates one: side
// paths that run on every command, like the invocation log, use it, so that
// `trellis --help` from a newer build is never what upgrades a database.
func OpenCurrent(path string) (*sqlx.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := connect(path)
	if err != nil {
		return nil, err
	}
	current, err := goose.GetDBVersion(db.DB)
	if err != nil {
		db.Close()
		return nil, err
	}
	known, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		db.Close()
		return nil, err
	}
	last, err := known.Last()
	if err != nil {
		db.Close()
		return nil, err
	}
	if current != last.Version {
		db.Close()
		return nil, fmt.Errorf("%s is at schema version %d; this binary uses %d", path, current, last.Version)
	}
	return db, nil
}

// connect opens path with the mandatory pragmas and prepares goose, without
// migrating. Open wraps it in the migration lock; tests use it to stop at an
// earlier schema version.
func connect(path string) (*sqlx.DB, error) {
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_time_integer_format=unix_milli&_pragma=%s&_pragma=%s&_pragma=%s&_pragma=%s",
		path,
		url.QueryEscape("journal_mode(WAL)"),
		url.QueryEscape("busy_timeout(10000)"),
		url.QueryEscape("synchronous(NORMAL)"),
		url.QueryEscape("foreign_keys(ON)"),
	)
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection: SQLite writes serialize anyway, and a pool would apply
	// pragmas per connection.
	db.SetMaxOpenConns(1)

	var setupErr error
	gooseSetup.Do(func() {
		goose.SetBaseFS(migrationFS)
		goose.SetLogger(goose.NopLogger())
		setupErr = goose.SetDialect("sqlite3")
	})
	if setupErr != nil {
		db.Close()
		return nil, setupErr
	}
	return db, nil
}
