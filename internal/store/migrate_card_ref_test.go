package store

import (
	"slices"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestCardRefsAreBackfilledAndUnique(t *testing.T) {
	db := openAtVersion(t, 15)
	for _, q := range []string{
		`INSERT INTO project (id, key, name, created_at) VALUES ('p1', 'ALPHA', 'ALPHA', 1), ('p2', 'MY-APP', 'MY-APP', 1)`,
		`INSERT INTO board (id, project_id, name, slug, is_default, created_at) VALUES ('b1', 'p1', 'alpha', 'alpha', 1, 1), ('b2', 'p2', 'app', 'app', 1, 1)`,
		`INSERT INTO column_ (id, board_id, name, position) VALUES ('c1', 'b1', 'backlog', 0), ('c2', 'b2', 'backlog', 0)`,
		`INSERT INTO card (id, project_id, board_id, seq, column_id, rank, title, created_at, updated_at) VALUES
		   ('k1', 'p1', 'b1', 1, 'c1', 'a', 'one', 1, 1),
		   ('k2', 'p1', 'b1', 12, 'c1', 'b', 'twelve', 1, 1),
		   ('k3', 'p2', 'b2', 1, 'c2', 'a', 'other', 1, 1)`,
	} {
		mustExec(t, db, q)
	}
	if err := goose.Up(db.DB, "migrations"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var refs []string
	if err := db.Select(&refs, `SELECT ref FROM card ORDER BY ref`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ALPHA-1", "ALPHA-12", "MY-APP-1"}; !slices.Equal(refs, want) {
		t.Errorf("refs = %v, want %v", refs, want)
	}
	if _, err := db.Exec(`UPDATE card SET ref = 'ALPHA-1' WHERE id = 'k3'`); err == nil {
		t.Error("two cards can share a ref")
	}

	mustExec(t, db, `INSERT INTO merged_project (key, into_id, merged_at) VALUES ('OLD', 'p1', 1)`)
	mustExec(t, db, `DELETE FROM project WHERE id = 'p1'`)
	var reserved int
	if err := db.Get(&reserved, `SELECT count(*) FROM merged_project`); err != nil || reserved != 0 {
		t.Errorf("merged_project rows = %d, %v; deleting the survivor must free the key", reserved, err)
	}
}
