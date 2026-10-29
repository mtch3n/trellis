package pgstore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations
var migrationFS embed.FS

const draftDir = "migrations/draft"

func Migrate(ctx context.Context, db *sqlx.DB) error {
	numbered, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	drafts, err := draftMigrations(numbered)
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.DB, numbered,
		goose.WithDisableGlobalRegistry(true),
		goose.WithSessionLocker(locker),
		goose.WithGoMigrations(drafts...),
	)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func draftMigrations(numbered fs.FS) ([]*goose.Migration, error) {
	names, err := fs.Glob(migrationFS, draftDir+"/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	last, err := lastNumbered(numbered)
	if err != nil {
		return nil, err
	}
	var out []*goose.Migration
	for i, name := range names {
		raw, err := fs.ReadFile(migrationFS, name)
		if err != nil {
			return nil, err
		}
		_, after, ok := strings.Cut(string(raw), "-- +goose Up")
		if !ok {
			return nil, fmt.Errorf("%s: no -- +goose Up section", path.Base(name))
		}
		up, _, _ := strings.Cut(after, "-- +goose Down")
		out = append(out, goose.NewGoMigration(last+int64(i)+1, &goose.GoFunc{
			RunTx: func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, up)
				return err
			},
		}, nil))
	}
	return out, nil
}

func lastNumbered(numbered fs.FS) (int64, error) {
	names, err := fs.Glob(numbered, "*.sql")
	if err != nil {
		return 0, err
	}
	var last int64
	for _, name := range names {
		v, err := goose.NumericComponent(name)
		if err != nil {
			return 0, err
		}
		last = max(last, v)
	}
	return last, nil
}
