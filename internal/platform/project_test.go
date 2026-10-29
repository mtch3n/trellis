package platform

import "testing"

func TestPLAT_C11_AdminStepsFromAnEmptyDatabaseGiveAWorkingTokenAndAPersonalBoard(t *testing.T) {
	p, db := tenancyPlatform(t)
	ctx := t.Context()
	_, err := p.CreateOrg(ctx, "acme")
	mustDo(t, err)
	m, err := p.AddMember(ctx, "acme", "ming", "")
	mustDo(t, err)
	pr, err := p.CreateProject(ctx, "acme", "APP", "")
	mustDo(t, err)
	mustDo(t, p.AddProjectMember(ctx, "acme", "APP", "ming", "member"))
	secret, err := p.IssueToken(ctx, "acme", "ming", "claude-laptop", TokenAgent)
	mustDo(t, err)

	id, err := p.VerifyToken(ctx, secret)
	mustDo(t, err)
	if id.Kind != TokenAgent || id.MemberID != m.ID || id.TokenName != "claude-laptop" {
		t.Fatalf("token identity = %+v, want agent token claude-laptop of %s", id, m.ID)
	}
	if got := id.Principal("").Actor(); got != "agent:ming/claude-laptop" {
		t.Fatalf("actor = %q", got)
	}

	var board struct {
		Name  string `db:"name"`
		Scope string `db:"scope"`
	}
	mustDo(t, db.Get(&board, `SELECT name, scope FROM board WHERE project_id = $1 AND owner_id = $2`, pr.ID.String(), m.ID.String()))
	if board.Name != "ming" || board.Scope != "personal" {
		t.Fatalf("personal board = %+v, want ming/personal", board)
	}
}

func TestPLAT_C12_AddingAMemberToAProjectOfAnotherOrgIsNotFound(t *testing.T) {
	p, db := tenancyPlatform(t)
	ctx := t.Context()
	for _, org := range []string{"A", "B"} {
		_, err := p.CreateOrg(ctx, org)
		mustDo(t, err)
	}
	_, err := p.AddMember(ctx, "A", "ming", "")
	mustDo(t, err)
	_, err = p.CreateProject(ctx, "B", "WEB", "")
	mustDo(t, err)
	before := rowCounts(t, db)

	err = p.AddProjectMember(ctx, "A", "WEB", "ming", "member")
	assertCode(t, err, "project_not_found", KindNotFound)
	err = p.AddProjectMember(ctx, "B", "WEB", "ming", "member")
	assertCode(t, err, "member_not_found", KindNotFound)
	assertUnchanged(t, db, before)
}
