package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/resolve"
)

type Project struct {
	ID            string `db:"id" json:"id"`
	Key           string `db:"key" json:"key"`
	IdentityKind  string `db:"identity_kind" json:"identity_kind"`
	IdentityValue string `db:"identity_value" json:"identity_value"`
	RootPath      string `db:"root_path" json:"root_path"`
	Name          string `db:"name" json:"name"`
	CreatedAt     int64  `db:"created_at" json:"created_at"`
}

// EnsureProject finds the project for an identity, rebinding or creating it.
//
// Rebinding is the important case: a repository with no remote gets a "path"
// identity, and adding a remote later would otherwise resolve to nothing and
// create a second, empty board. Matching on root_path and updating the identity
// in place keeps the cards.
func (c *Core) EnsureProject(ctx context.Context, id resolve.Identity) (Project, error) {
	var p Project
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		// 1. exact identity match
		err := tx.Get(&p, `SELECT * FROM project WHERE identity_value = ?`, id.Value)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		// 2. same root path, different identity: rebind in place
		if id.RootPath != "" {
			err = tx.Get(&p, `SELECT * FROM project WHERE root_path = ?`, id.RootPath)
			if err == nil {
				old := p.IdentityValue
				if _, err := tx.Exec(
					`UPDATE project SET identity_kind = ?, identity_value = ? WHERE id = ?`,
					id.Kind, id.Value, p.ID); err != nil {
					return err
				}
				p.IdentityKind, p.IdentityValue = id.Kind, id.Value
				return c.recordEvent(tx, "project", p.ID, "rebound", "identity_value", old, id.Value)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		// 3. create
		var taken int
		if err := tx.Get(&taken, `SELECT count(*) FROM project WHERE key = ?`, id.SuggestedKey); err != nil {
			return err
		}
		if taken > 0 {
			return ErrUsage("key_collision",
				fmt.Sprintf("project key %q is already used by another repository", id.SuggestedKey),
				"trellis init --key <UNIQUE-KEY>")
		}

		p = Project{
			ID: NewCardID(), Key: id.SuggestedKey, IdentityKind: id.Kind,
			IdentityValue: id.Value, RootPath: id.RootPath,
			Name: id.SuggestedKey, CreatedAt: c.clock.NowMS(),
		}
		if _, err := tx.Exec(
			`INSERT INTO project (id, key, identity_kind, identity_value, root_path, name, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.Key, p.IdentityKind, p.IdentityValue, p.RootPath, p.Name, p.CreatedAt); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "project", p.ID, "created", "", "", p.Key); err != nil {
			return err
		}
		_, err = c.createBoard(tx, p.ID, strings.ToLower(p.Key), true, true)
		return err
	})
	return p, err
}

// ListProjects returns every project, newest first.
func (c *Core) ListProjects(ctx context.Context) ([]Project, error) {
	projects := []Project{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&projects, `SELECT * FROM project ORDER BY created_at DESC`)
	})
	return projects, err
}

// ProjectByKey resolves the human key ("XPSCTL"), so an agent can act on a
// project it is not standing in.
func (c *Core) ProjectByKey(ctx context.Context, key string) (Project, error) {
	var p Project
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		err := tx.Get(&p, `SELECT * FROM project WHERE key = ?`, strings.ToUpper(key))
		if errors.Is(err, sql.ErrNoRows) {
			var keys []string
			if err := tx.Select(&keys, `SELECT key FROM project ORDER BY key`); err != nil {
				return err
			}
			return ErrNotFound("project_not_found", "no project "+strings.ToUpper(key),
				"trellis card ls --all-projects   # known: "+strings.Join(keys, ", "))
		}
		return err
	})
	return p, err
}
