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

type Project struct {
	ID    uuid.UUID `json:"id"`
	OrgID uuid.UUID `json:"org_id"`
	Key   string    `json:"key"`
	Name  string    `json:"name"`
}

type ProjectMember struct {
	Project string `json:"project"`
	Handle  string `json:"handle"`
	Role    string `json:"role"`
}

func (p *Platform) CreateProject(ctx context.Context, org, key, name string) (Project, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Project{}, ErrUsage("missing_key", "a project needs a key", "trellis admin project new --org <org> <key>")
	}
	pr := Project{Key: key, Name: name}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		o, err := orgByName(ctx, tx, org)
		if err != nil {
			return err
		}
		pr.OrgID = o.ID
		err = tx.QueryRowxContext(ctx, `INSERT INTO project (org_id, key, name) VALUES ($1, $2, $3) RETURNING id`,
			o.ID.String(), key, name).Scan(uuidDest(&pr.ID))
		if isUniqueViolation(err) {
			return ErrConflict("project_exists", fmt.Sprintf("project %q already exists in org %q", key, org), "choose another key")
		}
		return err
	})
	return pr, err
}

func (p *Platform) AddProjectMember(ctx context.Context, org, key, handle, role string) error {
	if err := checkRole(role, ProjectRoles); err != nil {
		return err
	}
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		m, err := memberByHandle(ctx, tx, org, handle)
		if err != nil {
			return err
		}
		pr, err := projectByKey(ctx, tx, m.OrgID, org, key)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO project_member (org_id, project_id, member_id, role) VALUES ($1, $2, $3, $4)`,
			m.OrgID.String(), pr.ID.String(), m.ID.String(), role)
		if isUniqueViolation(err) {
			return ErrConflict("already_member", fmt.Sprintf("member %q is already in project %q", handle, key),
				"trellis admin project role --org <org> <key> <handle> <role>")
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO board (project_id, owner_id, name, scope) VALUES ($1, $2, $3, 'personal')
			ON CONFLICT (project_id, owner_id, name) DO NOTHING`, pr.ID.String(), m.ID.String(), handle)
		return err
	})
}

func (p *Platform) SetProjectRole(ctx context.Context, org, key, handle, role string) error {
	if err := checkRole(role, ProjectRoles); err != nil {
		return err
	}
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		m, pr, err := membership(ctx, tx, org, key, handle)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE project_member SET role = $1 WHERE project_id = $2 AND member_id = $3`,
			role, pr.ID.String(), m.ID.String())
		return err
	})
}

func (p *Platform) RemoveProjectMember(ctx context.Context, org, key, handle string) error {
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		m, pr, err := membership(ctx, tx, org, key, handle)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM project_member WHERE project_id = $1 AND member_id = $2`, pr.ID.String(), m.ID.String())
		return err
	})
}

func (p *Platform) ProjectMembers(ctx context.Context, org, key string) ([]ProjectMember, error) {
	var out []ProjectMember
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		o, err := orgByName(ctx, tx, org)
		if err != nil {
			return err
		}
		pr, err := projectByKey(ctx, tx, o.ID, org, key)
		if err != nil {
			return err
		}
		return tx.SelectContext(ctx, &out, `SELECT p.key AS project, m.handle, pm.role
			FROM project_member pm JOIN member m ON m.id = pm.member_id JOIN project p ON p.id = pm.project_id
			WHERE pm.project_id = $1 ORDER BY m.handle`, pr.ID.String())
	})
	return out, err
}

func (p *Platform) ListProjects(ctx context.Context, by Principal) ([]Project, error) {
	query := `SELECT p.id, p.org_id, p.key, p.name FROM project p
		JOIN project_member pm ON pm.project_id = p.id
		WHERE p.org_id = $1 AND pm.member_id = $2 ORDER BY p.key`
	args := []any{by.OrgID.String(), by.MemberID.String()}
	if by.IsSystem() {
		query = `SELECT p.id, p.org_id, p.key, p.name FROM project p WHERE p.org_id = $1 ORDER BY p.key`
		args = args[:1]
	}
	out := []Project{}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		rows, err := tx.QueryxContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var pr Project
			if err := rows.Scan(uuidDest(&pr.ID), uuidDest(&pr.OrgID), &pr.Key, &pr.Name); err != nil {
				return err
			}
			out = append(out, pr)
		}
		return rows.Err()
	})
	return out, err
}

func projectByKey(ctx context.Context, tx *sqlx.Tx, orgID uuid.UUID, org, key string) (Project, error) {
	pr := Project{OrgID: orgID, Key: key}
	err := tx.QueryRowxContext(ctx, `SELECT id, name FROM project WHERE org_id = $1 AND key = $2`, orgID.String(), key).
		Scan(uuidDest(&pr.ID), &pr.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound("project_not_found", fmt.Sprintf("project %q not found in org %q", key, org), "trellis admin project new --org <org> <key>")
	}
	return pr, err
}

func membership(ctx context.Context, tx *sqlx.Tx, org, key, handle string) (Member, Project, error) {
	m, err := memberByHandle(ctx, tx, org, handle)
	if err != nil {
		return Member{}, Project{}, err
	}
	pr, err := projectByKey(ctx, tx, m.OrgID, org, key)
	if err != nil {
		return Member{}, Project{}, err
	}
	var one int
	err = tx.QueryRowxContext(ctx, `SELECT 1 FROM project_member WHERE project_id = $1 AND member_id = $2`, pr.ID.String(), m.ID.String()).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, Project{}, ErrNotFound("not_a_member", fmt.Sprintf("member %q is not in project %q", handle, key),
			"trellis admin project add-member --org <org> <key> <handle>")
	}
	return m, pr, err
}
