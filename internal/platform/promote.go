package platform

import (
	"context"
	"strings"

	"github.com/jmoiron/sqlx"
)

const verifyInterval = `interval '180 days'`

func (p *Platform) Promote(ctx context.Context, pr Principal, project, slug, reason string) (ShareResult, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ShareResult{}, ErrUsage("reason_required", "a promote needs a reason",
			"vault_share promote "+slug+" with a reason")
	}
	var out ShareResult
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if err := refuseSystem(pr); err != nil {
			return err
		}
		r, err := findShared(tx, pr, proj, slug)
		if err != nil {
			return err
		}
		switch {
		case r.Scope == ScopePersonal:
			return ErrPolicy("not_published", slug+" is personal; only a project entry is promoted",
				"vault_share publish "+slug+", then promote it")
		case r.Scope == "global":
			return ErrConflict("already_global", slug+" is already global", "vault_show "+slug)
		case r.OwnerID != pr.MemberID:
			return ErrPolicy("not_owner", "only the member who owns "+slug+" promotes it", "vault_share nominate "+slug)
		case r.Private:
			return ErrPolicy("private", slug+" is private; a private entry is never promoted",
				"vault_write edit with private false")
		}
		taken, err := execUnique(tx, `UPDATE entry SET scope = 'global', verify_by = now() + `+verifyInterval+`,
			verified_at = now(), promoted_by = $2, promote_reason = $3, promoted_at = now(), updated_at = now(), version = version + 1
			WHERE id = $1`, r.ID, pr.Actor(), reason)
		if err != nil {
			return err
		}
		if taken {
			return ErrConflict("global_slug_taken", "the global vault already has an entry "+slug, "vault_show "+slug)
		}
		if _, err := tx.Exec(`DELETE FROM nomination WHERE entry_id = $1`, r.ID); err != nil {
			return err
		}
		if err := shareEvent(tx, pr, r, "entry.promoted", map[string]any{"slug": slug, "reason": reason}); err != nil {
			return err
		}
		out, err = keepShareRevision(tx, pr, r.ID)
		return err
	})
	return out, err
}

func (p *Platform) Demote(ctx context.Context, pr Principal, project, slug string) (ShareResult, error) {
	var out ShareResult
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if err := refuseSystem(pr); err != nil {
			return err
		}
		r, err := findShared(tx, pr, proj, slug)
		if err != nil {
			return err
		}
		switch r.Scope {
		case ScopePersonal:
			return ErrUsage("not_shared", slug+" is personal; only a project or global entry is demoted", "vault_show "+slug)
		case ScopeProject:
			err = demoteProject(tx, pr, proj, r)
		default:
			err = demoteGlobal(tx, pr, r)
		}
		if err != nil {
			return err
		}
		out, err = keepShareRevision(tx, pr, r.ID)
		return err
	})
	return out, err
}

func demoteProject(tx *sqlx.Tx, pr Principal, proj projectAccess, r Entry) error {
	var maintainer bool
	if err := tx.Get(&maintainer, `SELECT EXISTS (SELECT 1 FROM project_member
		WHERE project_id = $1 AND member_id = $2 AND role = 'maintainer')`, proj.ID, pr.MemberID); err != nil {
		return err
	}
	if !maintainer {
		return ErrPolicy("not_maintainer", "only a maintainer of "+proj.Key+" demotes a project entry",
			"ask a maintainer of "+proj.Key)
	}
	taken, err := execUnique(tx, `UPDATE entry SET scope = 'personal', labels = array_remove(labels, 'gate'),
		updated_at = now(), version = version + 1 WHERE id = $1`, r.ID)
	if err != nil {
		return err
	}
	if taken {
		return ErrConflict("slug_taken", "the owner already has a personal entry "+r.Slug,
			"the owner renames one of the two entries")
	}
	return shareEvent(tx, pr, r, "entry.demoted", map[string]any{"slug": r.Slug, "to": ScopePersonal})
}

func demoteGlobal(tx *sqlx.Tx, pr Principal, r Entry) error {
	var admin bool
	if err := tx.Get(&admin, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1 AND role = 'admin')`, pr.MemberID); err != nil {
		return err
	}
	if !admin {
		return ErrPolicy("not_admin", "only an org admin demotes a global entry", "ask an admin of the org")
	}
	taken, err := execUnique(tx, `UPDATE entry SET scope = 'project', verify_by = NULL, updated_at = now(), version = version + 1 WHERE id = $1`, r.ID)
	if err != nil {
		return err
	}
	if taken {
		return ErrConflict("slug_taken", "the project the entry came from already has an entry with its slug or body",
			"vault_show "+r.Slug+" in that project")
	}
	return shareEvent(tx, pr, r, "entry.demoted", map[string]any{"slug": r.Slug, "to": ScopeProject})
}
