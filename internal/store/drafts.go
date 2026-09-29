package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/pressly/goose/v3"
)

// draftDir holds schema changes written during development. They carry no
// version number, so parallel branches never collide; a release turns them
// into numbered migrations and empties the directory.
const draftDir = "migrations/draft"

// registerDrafts gives each draft the next versions after every numbered
// migration, in name order, so a development build runs them like any other.
// A draft is goose-annotated SQL; only its Up section runs.
func registerDrafts() error {
	names, err := fs.Glob(migrationFS, draftDir+"/*.sql")
	if err != nil || len(names) == 0 {
		return err
	}
	slices.Sort(names)
	known, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		return err
	}
	last, err := known.Last()
	if err != nil {
		return err
	}
	for i, name := range names {
		raw, err := fs.ReadFile(migrationFS, name)
		if err != nil {
			return err
		}
		up, ok := draftUp(string(raw))
		if !ok {
			return fmt.Errorf("%s: no -- +goose Up section", name)
		}
		slug := strings.TrimSuffix(path.Base(name), ".sql")
		goose.AddNamedMigrationContext(fmt.Sprintf("%04d_draft_%s.go", last.Version+int64(i)+1, slug),
			func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, up)
				return err
			}, nil)
	}
	return nil
}

// draftUp returns the SQL between "-- +goose Up" and "-- +goose Down".
func draftUp(raw string) (string, bool) {
	_, after, ok := strings.Cut(raw, "-- +goose Up")
	if !ok {
		return "", false
	}
	up, _, _ := strings.Cut(after, "-- +goose Down")
	return up, true
}
