package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
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

// ownedEntities selects every id a project owns that a link row can name.
// Links carry no foreign key, so the cascade from project never reaches them.
const ownedEntities = `SELECT id FROM card WHERE project_id = ?
	UNION ALL SELECT id FROM knowledge WHERE project_id = ?
	UNION ALL SELECT id FROM artifact WHERE project_id = ?`

// DeleteProject removes a project and everything it owns: boards, cards,
// notes, labels, knowledge rows, and the project's directory under the
// Trellis home, which holds its knowledge files, artifacts and vectors. The
// event log is kept; it is the change feed.
//
// Two things refuse rather than proceed. A card an agent holds right now,
// because deleting work out from under a running session is not a cleanup.
// And an entry this project escalated to the global vault: the vault row still
// names its origin project, so it would be deleted with it.
//
// The directory is staged before the transaction and restored if it fails, so
// a failed delete never leaves rows without their files. Running trellis in
// the repository again creates a fresh, empty project.
func (c *Core) DeleteProject(ctx context.Context, key string) error {
	key = strings.ToUpper(strings.TrimSpace(key))
	var staged *stagedRemoval
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var p Project
		err := tx.Get(&p, `SELECT * FROM project WHERE key = ?`, key)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound("project_not_found", "no project "+key, "trellis ui   # the Projects page lists every project")
		}
		if err != nil {
			return err
		}

		var held int
		if err := tx.Get(&held,
			`SELECT COUNT(*) FROM card WHERE project_id = ? AND owner IS NOT NULL AND lease_until > ?`,
			p.ID, c.clock.NowMS()); err != nil {
			return err
		}
		if held > 0 {
			return ErrConflict("project_leased",
				fmt.Sprintf("%s has %d %s held by an agent right now", p.Key, held, plural(held, "card", "cards")),
				"wait for the leases to expire, or take them first")
		}

		var vault int
		if err := tx.Get(&vault, `SELECT COUNT(*) FROM knowledge WHERE project_id = ? AND global = 1`, p.ID); err != nil {
			return err
		}
		if vault > 0 {
			return ErrConflict("project_has_vault_entries",
				fmt.Sprintf("%d global vault %s came from %s and would be deleted with it",
					vault, plural(vault, "entry", "entries"), p.Key),
				"trellis knowledge demote <slug>   # to delete them too; otherwise keep the project")
		}

		root, err := c.root()
		if err != nil {
			return err
		}
		if staged, err = stageRemoval(filepath.Join(root, "projects", p.Key)); err != nil {
			return err
		}

		// A link from outside into this project survives as a stub, the same
		// way deleting one entry leaves its inbound links (§10.4). Links from
		// inside go with their source.
		ids := []any{p.ID, p.ID, p.ID}
		if _, err := tx.Exec(
			`UPDATE link SET to_id = NULL
			 WHERE to_id IN (`+ownedEntities+`) AND from_id NOT IN (`+ownedEntities+`)`,
			append(ids, ids...)...); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM link WHERE from_id IN (`+ownedEntities+`)`, ids...); err != nil {
			return err
		}

		if _, err := tx.Exec(`DELETE FROM project WHERE id = ?`, p.ID); err != nil {
			return err
		}
		if err := c.rebuildKnowledgeFTS(tx); err != nil {
			return err
		}
		return c.recordEvent(tx, "project", p.ID, "deleted", "key", p.Key, "")
	})
	if err != nil {
		if restoreErr := staged.restore(); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
		return err
	}
	return staged.finalize()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
