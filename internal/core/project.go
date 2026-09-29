package core

import (
	"cmp"
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
// it; a .trellis marker is how a directory reaches it.
type Project struct {
	ID   string `db:"id" json:"id"`
	Key  string `db:"key" json:"key"`
	Name string `db:"name" json:"name"`
	// Description says what the project is, in at most MaxDescriptionWords.
	Description string `db:"description" json:"description"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
	TrashedAt   *int64 `db:"-" json:"trashed_at,omitzero"`
}

// MaxDescriptionWords bounds a project description: enough to tell projects
// apart in a switcher, short enough to stay a description.
const MaxDescriptionWords = 50

// SetProjectDescription replaces a project's description. Whitespace is
// collapsed, so the stored text is one line; an empty description clears it.
func (c *Core) SetProjectDescription(ctx context.Context, key, description string) (Project, error) {
	words := strings.Fields(description)
	if len(words) > MaxDescriptionWords {
		return Project{}, ErrUsage("description_too_long",
			fmt.Sprintf("the description has %d words; a project description holds at most %d", len(words), MaxDescriptionWords),
			"shorten it to one or two sentences")
	}
	description = strings.Join(words, " ")
	p, err := c.ProjectByKey(ctx, key)
	if err != nil {
		return Project{}, err
	}
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&p.Description, `SELECT description FROM project WHERE id = ?`, p.ID); err != nil {
			return err
		}
		if p.Description == description {
			return nil
		}
		if _, err := tx.Exec(`UPDATE project SET description = ? WHERE id = ?`, description, p.ID); err != nil {
			return err
		}
		return c.recordEvent(tx, "project", p.ID, "edited", "description", p.Description, description)
	})
	if err != nil {
		return Project{}, err
	}
	p.Description = description
	return p, nil
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
			if trashed, err := projectInTrash(tx, strings.ToUpper(key)); err != nil || trashed {
				return cmp.Or(err, ErrNotFound("project_trashed", "project "+strings.ToUpper(key)+" is in the trash",
					"trellis project restore "+strings.ToUpper(key)))
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

// DeleteProject moves a project and everything it owns to the trash:
// boards, cards, comments, labels and entry rows. Its directory under the
// Trellis home, which holds its entry files, artifacts and vectors, stays
// where it is until the purge, and its key stays reserved until then. The
// event log is kept: it records every change.
//
// Two things refuse rather than proceed. A card an agent holds right now,
// because trashing work out from under a running session is not a cleanup.
// And an entry this project promoted to the global vault: the vault row still
// names its origin project, so it would go with it.
//
// A marker that still names the trashed key fails with project_trashed,
// whose fix is trellis project restore.
func (c *Core) DeleteProject(ctx context.Context, key string) error {
	key = strings.ToUpper(strings.TrimSpace(key))
	// Dropped first, while the vector file is still where the tables point.
	// A refused or failed delete loses nothing: the tables come back on use.
	_ = c.dropDerived(ctx, key)
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var p Project
		err := tx.Get(&p, `SELECT * FROM project WHERE key = ?`, key)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound("project_not_found", "no project "+key, "trellis project ls")
		}
		if err != nil {
			return err
		}

		var claimed int
		if err := tx.Get(&claimed,
			`SELECT COUNT(*) FROM card WHERE project_id = ? AND claimed_by IS NOT NULL AND claim_until > ?`,
			p.ID, c.clock.NowMS()); err != nil {
			return err
		}
		if claimed > 0 {
			return ErrConflict("project_has_claims",
				fmt.Sprintf("%s has %d %s claimed by an agent right now", p.Key, claimed, plural(claimed, "card", "cards")),
				"wait for the claims to expire, or steal them first")
		}

		var vault int
		if err := tx.Get(&vault, `SELECT COUNT(*) FROM entry WHERE project_id = ? AND global = 1`, p.ID); err != nil {
			return err
		}
		if vault > 0 {
			return ErrConflict("project_has_global_entries",
				fmt.Sprintf("%d global vault %s came from %s and would be trashed with it",
					vault, plural(vault, "entry", "entries"), p.Key),
				"trellis vault demote <entry>   # to trash them too; otherwise keep the project")
		}

		if err := c.recordEvent(tx, "project", p.ID, "trashed", "key", p.Key, ""); err != nil {
			return err
		}
		if err := c.trashRows(tx, trashItem{id: NewID(), kind: TrashProject, table: "project", itemID: p.ID,
			name: p.Key, title: p.Name, projectID: p.ID, projectKey: p.Key}); err != nil {
			return err
		}

		// A link from outside into this project survives as a stub, the same
		// way trashing one entry leaves its inbound links (§10.4).
		ids := []any{p.ID, p.ID, p.ID}
		if _, err := tx.Exec(
			`UPDATE link SET to_id = NULL
			 WHERE to_id IN (`+ownedEntities+`) AND from_id NOT IN (`+ownedEntities+`)`,
			append(ids, ids...)...); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project WHERE id = ?`, p.ID); err != nil {
			return err
		}
		return c.rebuildEntryFTS(tx)
	})
}

// RestoreProject puts a trashed project back with everything that was in it.
func (c *Core) RestoreProject(ctx context.Context, key string) (Project, error) {
	key = normalizeKey(key)
	var trashID string
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		it, err := findTrash(tx, "", TrashProject, key)
		trashID = it.ID
		return err
	})
	if err != nil {
		return Project{}, err
	}
	it, err := c.restoreTrash(ctx, trashID, func(tx *sqlx.Tx, it TrashItem) error {
		var n int
		if err := tx.Get(&n, `SELECT count(*) FROM project WHERE key = ?`, it.Name); err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict("key_collision", fmt.Sprintf("project %s already exists", it.Name), "trellis project ls")
		}
		return nil
	})
	if err != nil {
		return Project{}, err
	}
	return c.ProjectByKey(ctx, it.Name)
}

// TrashedProjects lists the projects in the trash, newest first.
func (c *Core) TrashedProjects(ctx context.Context) ([]Project, error) {
	var out []Project
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var items []TrashItem
		if err := tx.Select(&items, `SELECT * FROM trash WHERE kind = 'project' ORDER BY trashed_at DESC, id DESC`); err != nil {
			return err
		}
		for _, it := range items {
			snap, err := decodeSnapshot(it.Rows)
			if err != nil {
				return err
			}
			var p Project
			if err := scanRow(tx.Unsafe(), &p, it.rootRow(snap)); err != nil {
				return err
			}
			p.TrashedAt = &it.TrashedAt
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// projectInTrash reports whether key names a trashed project, whose key is
// reserved until the purge.
func projectInTrash(tx *sqlx.Tx, key string) (bool, error) {
	var n int
	err := tx.Get(&n, `SELECT count(*) FROM trash WHERE kind = 'project' AND name = ?`, key)
	return n > 0, err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// CreateProject makes a project and its default board. No directory is
// involved: this is `trellis project new`, for a project no marker names yet.
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
	if trashed, err := projectInTrash(tx, key); err != nil || trashed {
		return Project{}, cmp.Or(err, ErrConflict("project_trashed",
			fmt.Sprintf("project %s is in the trash, and its key stays reserved until it is purged", key),
			"trellis project restore "+key))
	}
	var taken int
	if err := tx.Get(&taken, `SELECT count(*) FROM project WHERE key = ?`, key); err != nil {
		return Project{}, err
	}
	if taken > 0 {
		return Project{}, ErrConflict("key_collision",
			fmt.Sprintf("project %s already exists", key), "trellis project ls")
	}
	p := Project{ID: NewID(), Key: key, Name: key, CreatedAt: c.clock.NowMS()}
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
	// Dir is the directory that receives the marker.
	Dir string
	// Key is the project key, any case. With Existing set it may be empty; if
	// not, it must agree with the marker.
	Key string
	// Join allows marking Dir with a project that already exists. Set it only
	// when the key was named explicitly: a key merely derived from a directory
	// name must never join an unrelated project that happens to share it.
	Join bool
	// BoardName is --board: the board, by name, that the marker should name.
	BoardName string
	// Existing is the marker already in Dir. Its target is honored, and the
	// file is never rewritten.
	Existing *resolve.Marker
	// Preset seeds the default labels into a project that has none.
	Preset bool
}

// InitResult reports what InitProject did.
type InitResult struct {
	Project    Project
	Board      *Board // the board the marker names, or nil
	MarkerPath string
	Created    bool // false: an existing project was joined
	Wrote      bool // false: the marker was already there
}

// InitProject makes Dir resolvable. Every database change happens in one
// transaction; the marker is published afterwards with a no-clobber link, so
// two concurrent inits can never overwrite each other's marker. A marker that
// appears in between is accepted if it says the same thing and refused
// otherwise, and a project created by the losing call stays behind with no
// marker naming it.
//
// InitProject never creates a board other than a new project's default:
// slugs are derived from names with collision suffixes, so a board created
// to match a marker's slug could not be promised to receive that slug.
func (c *Core) InitProject(ctx context.Context, req InitRequest) (InitResult, error) {
	key := normalizeKey(req.Key)
	join := req.Join
	markerBoard := ""
	if e := req.Existing; e != nil {
		if key != "" && key != e.Target.Project {
			return InitResult{}, markerExists(e.Path, e.Target, "--key "+key)
		}
		key, markerBoard, join = e.Target.Project, e.Target.Board(), true
	}
	// Every marker this writes must parse, including one that joins a project
	// whose key predates the grammar: such a project is reached by --project.
	if err := checkNewKey(key); err != nil {
		return InitResult{}, err
	}

	res := InitResult{MarkerPath: filepath.Join(req.Dir, resolve.MarkerFile)}
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
			if req.Existing != nil && b.Slug != markerBoard {
				return markerExists(req.Existing.Path, req.Existing.Target, "--board "+req.BoardName)
			}
			res.Board = &b
		case markerBoard != "":
			b, err := boardBySlug(ctx, tx, res.Project.ID, markerBoard)
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
	err = atomicfile.Write(res.MarkerPath, []byte(target.String()+"\n"), false)
	switch {
	case err == nil:
		res.Wrote = true
		return res, nil
	case errors.Is(err, fs.ErrExist):
		if other, readErr := resolve.ReadMarker(res.MarkerPath); readErr == nil && other.Target == target {
			return res, nil
		}
		msg := res.MarkerPath + " was written by another init while this one ran"
		if res.Created {
			msg += fmt.Sprintf("; project %s was created and no marker names it", res.Project.Key)
		}
		return InitResult{}, ErrConflict("marker_exists", msg, "trellis project ls")
	default:
		return InitResult{}, fmt.Errorf("writing %s: %w; project %s is ready, rerun trellis init --key %s",
			res.MarkerPath, err, res.Project.Key, res.Project.Key)
	}
}

func markerExists(path string, target address.Address, flag string) error {
	return ErrConflict("marker_exists",
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
