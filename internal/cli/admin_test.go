package cli

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
	"github.com/mtch3n/trellis/internal/platform"
)

func adminDB(t *testing.T) *sqlx.DB {
	t.Helper()
	url := pgtest.URL(t)
	t.Setenv("TRELLIS_DATABASE_URL", url)
	db := sqlx.MustOpen("pgx", url)
	t.Cleanup(func() { db.Close() })
	return db
}

func admin(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runCmdErr(t, append([]string{"--json", "admin"}, args...)...)
	if err != nil {
		t.Fatalf("trellis admin %v: %v\n%s", args, err, out)
	}
	return out
}

func adminCounts(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	var s string
	if err := db.Get(&s, `SELECT concat_ws(',', (SELECT count(*) FROM org), (SELECT count(*) FROM member), (SELECT count(*) FROM project),
		(SELECT count(*) FROM project_member), (SELECT count(*) FROM token), (SELECT count(*) FROM board))`); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPLAT_C6_AdminCLIAnswersProjectExists(t *testing.T) {
	db := adminDB(t)
	admin(t, "org", "new", "A")
	admin(t, "project", "new", "--org", "A", "APP")
	before := adminCounts(t, db)

	_, err := runCmdErr(t, "--json", "admin", "project", "new", "--org", "A", "APP")
	if cliErrCode(err) != "project_exists" {
		t.Fatalf("err = %v, want project_exists", err)
	}
	if after := adminCounts(t, db); after != before {
		t.Fatalf("rows %s, want %s", after, before)
	}
}

func TestPLAT_C8_AdminCLIAnswersBadRoleWithTheAllowedValues(t *testing.T) {
	db := adminDB(t)
	admin(t, "org", "new", "A")
	admin(t, "member", "add", "--org", "A", "ming")
	admin(t, "project", "new", "--org", "A", "APP")
	admin(t, "project", "add-member", "--org", "A", "APP", "ming")
	before := adminCounts(t, db)

	for _, c := range []struct {
		args    []string
		allowed []string
	}{
		{[]string{"member", "role", "--org", "A", "ming", "owner"}, platform.OrgRoles},
		{[]string{"project", "role", "--org", "A", "APP", "ming", "admin"}, platform.ProjectRoles},
	} {
		_, err := runCmdErr(t, append([]string{"--json", "admin"}, c.args...)...)
		if cliErrCode(err) != "bad_role" {
			t.Fatalf("%v: err = %v, want bad_role", c.args, err)
		}
		for _, r := range c.allowed {
			if !strings.Contains(err.Error(), r) {
				t.Errorf("%v: %q does not list %s", c.args, err, r)
			}
		}
	}
	var roles string
	if err := db.Get(&roles, `SELECT m.role || '/' || pm.role FROM member m JOIN project_member pm ON pm.member_id = m.id`); err != nil {
		t.Fatal(err)
	}
	if roles != "member/member" {
		t.Fatalf("roles = %s, want member/member", roles)
	}
	if after := adminCounts(t, db); after != before {
		t.Fatalf("rows %s, want %s", after, before)
	}
}

func TestPLAT_C11_AdminCLIBootstrapsAnEmptyDatabase(t *testing.T) {
	db := adminDB(t)
	admin(t, "org", "new", "acme")
	admin(t, "member", "add", "--org", "acme", "ming")
	admin(t, "project", "new", "--org", "acme", "APP")
	admin(t, "project", "add-member", "--org", "acme", "APP", "ming")
	out := admin(t, "token", "issue", "--org", "acme", "ming", "claude-laptop")

	var issued struct {
		Token string `json:"token"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(out), &issued); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	id, err := platform.New(db).VerifyToken(t.Context(), issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != platform.TokenAgent || id.Handle != "ming" || id.TokenName != "claude-laptop" {
		t.Fatalf("identity = %+v", id)
	}
	var board string
	if err := db.Get(&board, `SELECT b.scope FROM board b JOIN project p ON p.id = b.project_id JOIN member m ON m.id = b.owner_id
		WHERE p.key = 'APP' AND m.handle = 'ming' AND b.name = 'ming'`); err != nil {
		t.Fatalf("personal board: %v", err)
	}
	if board != "personal" {
		t.Fatalf("board scope = %s", board)
	}
}

func TestPLAT_C12_AdminCLIAnswersNotFoundAcrossOrgs(t *testing.T) {
	db := adminDB(t)
	admin(t, "org", "new", "A")
	admin(t, "org", "new", "B")
	admin(t, "member", "add", "--org", "A", "ming")
	admin(t, "project", "new", "--org", "B", "WEB")
	before := adminCounts(t, db)

	for org, code := range map[string]string{"A": "project_not_found", "B": "member_not_found"} {
		_, err := runCmdErr(t, "--json", "admin", "project", "add-member", "--org", org, "WEB", "ming")
		if cliErrCode(err) != code {
			t.Errorf("--org %s: err = %v, want %s", org, err, code)
		}
	}
	if after := adminCounts(t, db); after != before {
		t.Fatalf("rows %s, want %s", after, before)
	}
}
