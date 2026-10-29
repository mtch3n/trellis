package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/jmoiron/sqlx"
)

var (
	OrgRoles     = []string{"admin", "member"}
	ProjectRoles = []string{"maintainer", "member"}
)

type Member struct {
	ID     uuid.UUID `json:"id"`
	OrgID  uuid.UUID `json:"org_id"`
	Handle string    `json:"handle"`
	Name   string    `json:"name"`
	Role   string    `json:"role"`
}

func (p *Platform) AddMember(ctx context.Context, org, handle, name string) (Member, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return Member{}, ErrUsage("missing_handle", "a member needs a handle", "trellis admin member add --org <org> <handle>")
	}
	m := Member{Handle: handle, Name: name, Role: "member"}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		o, err := orgByName(ctx, tx, org)
		if err != nil {
			return err
		}
		m.OrgID = o.ID
		err = tx.QueryRowxContext(ctx, `INSERT INTO member (org_id, handle, name) VALUES ($1, $2, $3) RETURNING id`,
			o.ID.String(), handle, name).Scan(uuidDest(&m.ID))
		if isUniqueViolation(err) {
			return ErrConflict("member_exists", fmt.Sprintf("member %q already exists in org %q", handle, org), "choose another handle")
		}
		return err
	})
	return m, err
}

func (p *Platform) SetMemberRole(ctx context.Context, org, handle, role string) error {
	if err := checkRole(role, OrgRoles); err != nil {
		return err
	}
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		m, err := memberByHandle(ctx, tx, org, handle)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE member SET role = $1 WHERE id = $2`, role, m.ID.String())
		return err
	})
}

func (p *Platform) Member(ctx context.Context, org, handle string) (Member, error) {
	var m Member
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		m, err = memberByHandle(ctx, tx, org, handle)
		return err
	})
	return m, err
}

func memberByHandle(ctx context.Context, tx *sqlx.Tx, org, handle string) (Member, error) {
	o, err := orgByName(ctx, tx, org)
	if err != nil {
		return Member{}, err
	}
	m := Member{OrgID: o.ID, Handle: handle}
	err = tx.QueryRowxContext(ctx, `SELECT id, name, role FROM member WHERE org_id = $1 AND handle = $2`, o.ID.String(), handle).
		Scan(uuidDest(&m.ID), &m.Name, &m.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrNotFound("member_not_found", fmt.Sprintf("member %q not found in org %q", handle, org), "trellis admin member add --org <org> <handle>")
	}
	return m, err
}

func checkRole(role string, allowed []string) error {
	if slices.Contains(allowed, role) {
		return nil
	}
	return ErrUsage("bad_role", fmt.Sprintf("role %q is not allowed; the allowed roles are %s", role, strings.Join(allowed, ", ")),
		"use one of: "+strings.Join(allowed, ", "))
}
