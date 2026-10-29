package platform

import (
	"database/sql"
	"errors"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Op int

const (
	OpRead Op = iota
	OpWrite
	OpOwn
)

type Verdict int

const (
	Allow Verdict = iota
	NoProject
	Hidden
	NotOwner
	ReadOnly
)

type Target struct {
	OrgID   uuid.UUID
	OwnerID uuid.UUID
	Scope   string
}

func Decide(pr Principal, member bool, t Target, op Op) Verdict {
	switch {
	case pr.OrgID != t.OrgID:
		return NoProject
	case !member && !pr.IsSystem():
		return NoProject
	case pr.IsSystem() && op == OpRead:
		return Allow
	case pr.IsSystem():
		return ReadOnly
	case t.OwnerID == pr.MemberID:
		return Allow
	case t.Scope == ScopeProject && op == OpOwn:
		return NotOwner
	case t.Scope == ScopeProject:
		return Allow
	default:
		return Hidden
	}
}

func (v Verdict) err(hidden *Error) error {
	switch v {
	case Allow:
		return nil
	case NoProject:
		return errProjectNotFound()
	case Hidden:
		return hidden
	case NotOwner:
		return ErrPolicy("not_owner", "only the board's owner may do this", "board_list")
	default:
		return ErrPolicy("read_only", "the system principal changes nothing", "")
	}
}

func errProjectNotFound() *Error {
	return ErrNotFound("project_not_found", "no such project", "project_list")
}

type projectAccess struct {
	ID     uuid.UUID `db:"id"`
	OrgID  uuid.UUID `db:"org_id"`
	Key    string    `db:"key"`
	Member bool      `db:"member"`
}

func (a projectAccess) verdict(pr Principal, t Target, op Op) Verdict {
	t.OrgID = a.OrgID
	return Decide(pr, a.Member, t, op)
}

func (a projectAccess) decide(pr Principal, t Target, op Op, hidden *Error) error {
	return a.verdict(pr, t, op).err(hidden)
}

func enterProject(tx *sqlx.Tx, pr Principal, key string, op Op) (projectAccess, error) {
	var a projectAccess
	err := tx.Get(&a, `SELECT p.id, p.org_id, p.key,
		EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = p.id AND pm.member_id = $3) AS member
		FROM project p WHERE p.org_id = $1 AND p.key = $2`, pr.OrgID, key, pr.MemberID)
	if errors.Is(err, sql.ErrNoRows) {
		return a, errProjectNotFound()
	}
	if err != nil {
		return a, err
	}
	return a, a.decide(pr, Target{Scope: ScopeProject}, op, errProjectNotFound())
}
