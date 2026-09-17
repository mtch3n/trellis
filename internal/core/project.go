package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/atomicfile"
	"github.com/mtch3n/trellis/internal/resolve"
)

// Project is a virtual namespace, named by its key. No directory belongs to
// it; a .trellis pin is how a directory reaches it.
type Project struct {
	ID        string `db:"id" json:"id"`
	Key       string `db:"key" json:"key"`
	Name      string `db:"name" json:"name"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
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
			into, merr := mergedTarget(tx, strings.ToUpper(key))
			if merr != nil {
				return merr
			}
			if into != "" {
				return errProjectMerged(strings.ToUpper(key), into)
			}
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
	UNION ALL SELECT id FROM entry WHERE project_id = ?
	UNION ALL SELECT id FROM artifact WHERE project_id = ?`

// DeleteProject removes a project and everything it owns: boards, cards,
// comments, labels, entry rows, and the project's directory under the
// Trellis home, which holds its entry files, artifacts and vectors. The
// event log is kept; it is the change feed.
//
// Two things refuse rather than proceed. A card an agent holds right now,
// because deleting work out from under a running session is not a cleanup.
// And an entry this project escalated to the global vault: the vault row still
// names its origin project, so it would be deleted with it.
//
// The directory is staged before the transaction and restored if it fails, so
// a failed delete never leaves rows without their files. A pin that still
// names the deleted key then fails with project_not_found; trellis init in
// that directory creates a fresh, empty project.
func (c *Core) DeleteProject(ctx context.Context, key string) error {
	key = strings.ToUpper(strings.TrimSpace(key))
	// Dropped first, while the vector file is still where the tables point.
	// A refused or failed delete loses nothing: the tables come back on use.
	_ = c.dropDerived(ctx, key)
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
			`SELECT COUNT(*) FROM card WHERE project_id = ? AND claimed_by IS NOT NULL AND claim_until > ?`,
			p.ID, c.clock.NowMS()); err != nil {
			return err
		}
		if held > 0 {
			return ErrConflict("project_leased",
				fmt.Sprintf("%s has %d %s held by an agent right now", p.Key, held, plural(held, "card", "cards")),
				"wait for the leases to expire, or take them first")
		}

		var vault int
		if err := tx.Get(&vault, `SELECT COUNT(*) FROM entry WHERE project_id = ? AND global = 1`, p.ID); err != nil {
			return err
		}
		if vault > 0 {
			return ErrConflict("project_has_vault_entries",
				fmt.Sprintf("%d global vault %s came from %s and would be deleted with it",
					vault, plural(vault, "entry", "entries"), p.Key),
				"trellis knowledge demote <slug>   # to delete them too; otherwise keep the project")
		}

		if staged, err = stageRemoval(filepath.Join(c.root, "projects", p.Key)); err != nil {
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

		if err := c.recordEvent(tx, "project", p.ID, "deleted", "key", p.Key, ""); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project WHERE id = ?`, p.ID); err != nil {
			return err
		}
		if err := c.rebuildEntryFTS(tx); err != nil {
			return err
		}
		return nil
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

// CreateProject makes a project and its default board. No directory is
// involved: this is `trellis project new`, for a project nothing pins yet.
func (c *Core) CreateProject(ctx context.Context, key string, preset bool) (Project, error) {
	var p Project
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		if p, err = c.createProject(tx, normalizeKey(key)); err != nil {
			return err
		}
		if preset {
			_, err = c.seedLabelsIfNone(tx, p.ID)
		}
		return err
	})
	return p, err
}

func normalizeKey(key string) string { return strings.ToUpper(strings.TrimSpace(key)) }

// checkNewKey enforces the key grammar on keys being created. Existing rows
// may predate it; they stay reachable by --project.
func checkNewKey(key string) error {
	if !address.ValidKey(key) {
		return ErrUsage("bad_key",
			fmt.Sprintf("%q is not a project key: use upper-case letters, digits and single hyphens, starting with a letter", key),
			"trellis init --key <KEY>")
	}
	if key == GlobalKey {
		return ErrUsage("reserved_key", "GLOBAL is the global vault and cannot name a project",
			"trellis init --key <OTHER-KEY>")
	}
	return nil
}

// createProject inserts a project and its default board in the caller's
// transaction.
func (c *Core) createProject(tx *sqlx.Tx, key string) (Project, error) {
	if err := checkNewKey(key); err != nil {
		return Project{}, err
	}
	into, err := mergedTarget(tx, key)
	if err != nil {
		return Project{}, err
	}
	if into != "" {
		return Project{}, ErrConflict("key_reserved",
			fmt.Sprintf("%s was merged into %s, and its key stays reserved while cards still carry it", key, into),
			"trellis init --key "+into)
	}
	var taken int
	if err := tx.Get(&taken, `SELECT count(*) FROM project WHERE key = ?`, key); err != nil {
		return Project{}, err
	}
	if taken > 0 {
		return Project{}, ErrConflict("key_collision",
			fmt.Sprintf("project %s already exists", key), "trellis project ls")
	}
	p := Project{ID: NewCardID(), Key: key, Name: key, CreatedAt: c.clock.NowMS()}
	if _, err := tx.Exec(
		`INSERT INTO project (id, key, name, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.Key, p.Name, p.CreatedAt); err != nil {
		return Project{}, err
	}
	if err := c.recordEvent(tx, "project", p.ID, "created", "", "", p.Key); err != nil {
		return Project{}, err
	}
	if _, err := c.createBoard(tx, p.ID, strings.ToLower(p.Key), true, true); err != nil {
		return Project{}, err
	}
	return p, nil
}

// InitRequest describes one `trellis init`.
type InitRequest struct {
	// Dir is the directory that receives the pin.
	Dir string
	// Key is the project key, any case. With Existing set it may be empty; if
	// not, it must agree with the pin.
	Key string
	// Join allows pinning a project that already exists. Set it only when the
	// key was named explicitly: a key merely derived from a directory name
	// must never join an unrelated project that happens to share it.
	Join bool
	// BoardName is --board: the board, by name, that the pin should name.
	BoardName string
	// Existing is the pin already in Dir. Its target is honored, and the file
	// is never rewritten.
	Existing *resolve.Pin
	// Preset seeds the default labels into a project that has none.
	Preset bool
}

// InitResult reports what InitProject did.
type InitResult struct {
	Project Project
	Board   *Board // the board the pin names, or nil
	PinPath string
	Created bool // false: an existing project was joined
	Wrote   bool // false: the pin was already there
}

// InitProject makes Dir resolvable. Every database change happens in one
// transaction; the pin is published afterwards with a no-clobber link, so
// two concurrent inits can never overwrite each other's pin. A pin that
// appears in between is accepted if it says the same thing and refused
// otherwise, and a project created by the losing call stays behind unpinned.
//
// InitProject never creates a board other than a new project's default:
// slugs are derived from names with collision suffixes, so a board created
// to match a pinned slug could not be promised to receive that slug.
func (c *Core) InitProject(ctx context.Context, req InitRequest) (InitResult, error) {
	key := normalizeKey(req.Key)
	join := req.Join
	pinBoard := ""
	if e := req.Existing; e != nil {
		if key != "" && key != e.Target.Project {
			return InitResult{}, pinExists(e.Path, e.Target, "--key "+key)
		}
		key, pinBoard, join = e.Target.Project, e.Target.Board(), true
	}
	// Every pin this writes must parse, including one that joins a project
	// whose key predates the grammar: such a project is reached by --project.
	if err := checkNewKey(key); err != nil {
		return InitResult{}, err
	}

	res := InitResult{PinPath: filepath.Join(req.Dir, resolve.PinFile)}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		err := tx.Get(&res.Project, `SELECT * FROM project WHERE key = ?`, key)
		switch {
		case err == nil && !join:
			return ErrConflict("key_collision",
				fmt.Sprintf("project %s already exists, and a key taken from the directory name never joins one", key),
				"trellis init --key "+key+"   # join it deliberately, or pick --key <OTHER-KEY>")
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			if res.Project, err = c.createProject(tx, key); err != nil {
				return err
			}
			res.Created = true
		default:
			return err
		}

		if req.Preset {
			if _, err := c.seedLabelsIfNone(tx, res.Project.ID); err != nil {
				return err
			}
		}

		switch {
		case req.BoardName != "":
			b, err := c.boardByName(tx, res.Project.ID, req.BoardName)
			if err != nil {
				return err
			}
			if req.Existing != nil && b.Slug != pinBoard {
				return pinExists(req.Existing.Path, req.Existing.Target, "--board "+req.BoardName)
			}
			res.Board = &b
		case pinBoard != "":
			b, err := boardBySlug(ctx, tx, res.Project.ID, pinBoard)
			if err != nil {
				return err
			}
			res.Board = &b
		}
		return nil
	})
	if err != nil {
		return InitResult{}, err
	}
	if req.Existing != nil {
		return res, nil
	}

	target := address.Project(res.Project.Key)
	if res.Board != nil {
		target = address.Board(res.Project.Key, res.Board.Slug)
	}
	err = atomicfile.Write(res.PinPath, []byte(target.String()+"\n"), false)
	switch {
	case err == nil:
		res.Wrote = true
		return res, nil
	case errors.Is(err, fs.ErrExist):
		if other, readErr := resolve.ReadPin(res.PinPath); readErr == nil && other.Target == target {
			return res, nil
		}
		msg := res.PinPath + " was written by another init while this one ran"
		if res.Created {
			msg += fmt.Sprintf("; project %s was created and is not pinned", res.Project.Key)
		}
		return InitResult{}, ErrConflict("pin_exists", msg, "trellis project ls")
	default:
		return InitResult{}, fmt.Errorf("writing %s: %w; project %s is ready, rerun trellis init --key %s",
			res.PinPath, err, res.Project.Key, res.Project.Key)
	}
}

func pinExists(path string, target address.Address, flag string) error {
	return ErrConflict("pin_exists",
		fmt.Sprintf("%s already names %s, which %s contradicts", path, target, flag),
		"edit or delete "+path+", then rerun trellis init")
}

// mergedTarget is the key of the project a retired key was merged into, or
// "" when key was never merged away.
func mergedTarget(tx *sqlx.Tx, key string) (string, error) {
	var into string
	err := tx.Get(&into,
		`SELECT p.key FROM merged_project m JOIN project p ON p.id = m.into_id WHERE m.key = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return into, err
}

// errProjectMerged is what naming a retired key gets. Detail carries the
// survivor's key for callers that can build a better fix, such as the same
// address in the survivor.
func errProjectMerged(key, into string) error {
	return &Error{
		Code: "project_merged", Exit: 3,
		Msg:    fmt.Sprintf("project %s was merged into %s", key, into),
		Fix:    "trellis --project " + into + " <command>",
		Detail: map[string]string{"into": into},
	}
}
