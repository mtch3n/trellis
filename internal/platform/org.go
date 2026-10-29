package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Org struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

func (p *Platform) CreateOrg(ctx context.Context, name string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, ErrUsage("missing_name", "an org needs a name", "trellis admin org new <name>")
	}
	o := Org{Name: name}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		err := tx.QueryRowxContext(ctx, `INSERT INTO org (name) VALUES ($1) RETURNING id`, name).Scan(uuidDest(&o.ID))
		if isUniqueViolation(err) {
			return ErrConflict("org_exists", fmt.Sprintf("org %q already exists", name), "choose another name")
		}
		return err
	})
	return o, err
}

func orgByName(ctx context.Context, tx *sqlx.Tx, name string) (Org, error) {
	o := Org{Name: name}
	err := tx.QueryRowxContext(ctx, `SELECT id FROM org WHERE name = $1`, name).Scan(uuidDest(&o.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return Org{}, ErrNotFound("org_not_found", fmt.Sprintf("org %q not found", name), "trellis admin org new <name>")
	}
	return o, err
}

type uuidScanner struct{ dst *uuid.UUID }

func uuidDest(dst *uuid.UUID) uuidScanner { return uuidScanner{dst} }

func (s uuidScanner) Scan(src any) error {
	switch v := src.(type) {
	case string:
		u, err := uuid.Parse(v)
		*s.dst = u
		return err
	case []byte:
		u, err := uuid.Parse(string(v))
		*s.dst = u
		return err
	}
	return fmt.Errorf("cannot scan %T into a uuid", src)
}
