package platform

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Platform struct {
	db       *sqlx.DB
	embedder Embedder
}

func New(db *sqlx.DB) *Platform {
	return &Platform{db: db}
}

func (p *Platform) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *Platform) Tx(ctx context.Context, fn func(tx *sqlx.Tx) error) error {
	tx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
