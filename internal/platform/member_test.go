package platform

import (
	"strings"
	"testing"
)

func TestPlat_PLAT_C7_AdminSetsTheOrgRoleAndTheProjectRole(t *testing.T) {
	p, db := tenancyPlatform(t)
	ctx := t.Context()
	_, err := p.CreateOrg(ctx, "A")
	mustDo(t, err)
	m, err := p.AddMember(ctx, "A", "ming", "Ming")
	mustDo(t, err)
	_, err = p.CreateProject(ctx, "A", "APP", "")
	mustDo(t, err)
	mustDo(t, p.AddProjectMember(ctx, "A", "APP", "ming", "member"))

	mustDo(t, p.SetMemberRole(ctx, "A", "ming", "admin"))
	mustDo(t, p.SetProjectRole(ctx, "A", "APP", "ming", "maintainer"))

	var orgRole, projectRole string
	mustDo(t, db.Get(&orgRole, `SELECT role FROM member WHERE id = $1`, m.ID.String()))
	mustDo(t, db.Get(&projectRole, `SELECT role FROM project_member WHERE member_id = $1`, m.ID.String()))
	if orgRole != "admin" || projectRole != "maintainer" {
		t.Fatalf("roles = %q, %q, want admin, maintainer", orgRole, projectRole)
	}
}

func TestPlat_PLAT_C8_ARoleThatIsNotAllowedIsRefused(t *testing.T) {
	p, db := tenancyPlatform(t)
	ctx := t.Context()
	_, err := p.CreateOrg(ctx, "A")
	mustDo(t, err)
	_, err = p.AddMember(ctx, "A", "ming", "")
	mustDo(t, err)
	_, err = p.CreateProject(ctx, "A", "APP", "")
	mustDo(t, err)
	mustDo(t, p.AddProjectMember(ctx, "A", "APP", "ming", "member"))
	before := rowCounts(t, db)
	roles := func() string {
		var r string
		mustDo(t, db.Get(&r, `SELECT m.role || '/' || pm.role FROM member m JOIN project_member pm ON pm.member_id = m.id`))
		return r
	}

	cases := []struct {
		name    string
		call    func() error
		allowed []string
	}{
		{"org role", func() error { return p.SetMemberRole(ctx, "A", "ming", "maintainer") }, OrgRoles},
		{"project role", func() error { return p.SetProjectRole(ctx, "A", "APP", "ming", "lead") }, ProjectRoles},
		{"join with role", func() error { return p.AddProjectMember(ctx, "A", "APP", "ming", "root") }, ProjectRoles},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := assertCode(t, c.call(), "bad_role", KindUsage)
			for _, r := range c.allowed {
				if !strings.Contains(e.Msg, r) {
					t.Errorf("message %q does not list %q", e.Msg, r)
				}
			}
			if got := roles(); got != "member/member" {
				t.Errorf("roles = %s, want member/member", got)
			}
			assertUnchanged(t, db, before)
		})
	}
}
