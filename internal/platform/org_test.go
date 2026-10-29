package platform

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
)

func tenancyPlatform(t *testing.T) (*Platform, *sqlx.DB) {
	t.Helper()
	db := pgtest.Open(t)
	if err := pgstore.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func rowCounts(t *testing.T, db *sqlx.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"org", "member", "project", "project_member", "token", "board"} {
		var n int
		mustDo(t, db.Get(&n, `SELECT count(*) FROM `+table))
		out[table] = n
	}
	return out
}

func assertUnchanged(t *testing.T, db *sqlx.DB, before map[string]int) {
	t.Helper()
	after := rowCounts(t, db)
	for table, n := range before {
		if after[table] != n {
			t.Errorf("table %s has %d rows, want %d", table, after[table], n)
		}
	}
}

func assertCode(t *testing.T, err error, code string, kind Kind) *Error {
	t.Helper()
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("err = %v, want a platform error %s", err, code)
	}
	if e.Code != code || e.Kind != kind {
		t.Fatalf("err = %s/%s (%v), want %s/%s", e.Kind, e.Code, e, kind, code)
	}
	return e
}

func TestPlat_PLAT_C5_ProjectsAndCardRefsAreScopedToTheirOrg(t *testing.T) {
	p, db := tenancyPlatform(t)
	ctx := t.Context()
	var projects = map[string]Project{}
	var members = map[string]Member{}
	for _, org := range []string{"A", "B"} {
		_, err := p.CreateOrg(ctx, org)
		mustDo(t, err)
		m, err := p.AddMember(ctx, org, "ming", "")
		mustDo(t, err)
		members[org] = m
		pr, err := p.CreateProject(ctx, org, "APP", "")
		mustDo(t, err)
		projects[org] = pr
		mustDo(t, p.AddProjectMember(ctx, org, "APP", "ming", "member"))
	}

	got, err := p.ListProjects(ctx, MemberAgent(members["A"].OrgID, members["A"].ID, "ming", "laptop", ""))
	mustDo(t, err)
	if len(got) != 1 || got[0].ID != projects["A"].ID || got[0].Key != "APP" {
		t.Fatalf("A's member lists %+v, want only A's APP %s", got, projects["A"].ID)
	}

	cardIDs := map[string]string{}
	for _, org := range []string{"A", "B"} {
		pr := projects[org]
		var boardID, columnID, cardID string
		mustDo(t, db.Get(&boardID, `SELECT id FROM board WHERE project_id = $1`, pr.ID.String()))
		mustDo(t, db.Get(&columnID, `SELECT id FROM board_column WHERE board_id = $1 AND name = 'backlog'`, boardID))
		mustDo(t, db.Get(&cardID, `INSERT INTO card (project_id, board_id, column_id, number, ref, title, created_by)
			VALUES ($1, $2, $3, 1, 'APP-1', 'first', 'test') RETURNING id`, pr.ID.String(), boardID, columnID))
		cardIDs[org] = cardID
	}
	if cardIDs["A"] == cardIDs["B"] {
		t.Fatalf("APP-1 in A and APP-1 in B are the same card %s", cardIDs["A"])
	}
}

func TestPlat_PLAT_C6_CreatingAnExistingProjectKeyConflicts(t *testing.T) {
	p, db := tenancyPlatform(t)
	ctx := t.Context()
	_, err := p.CreateOrg(ctx, "A")
	mustDo(t, err)
	_, err = p.CreateProject(ctx, "A", "APP", "first")
	mustDo(t, err)
	before := rowCounts(t, db)

	_, err = p.CreateProject(ctx, "A", "APP", "second")
	assertCode(t, err, "project_exists", KindConflict)
	assertUnchanged(t, db, before)
	var name string
	mustDo(t, db.Get(&name, `SELECT name FROM project WHERE key = 'APP'`))
	if name != "first" {
		t.Fatalf("project name = %q, want first", name)
	}
}
