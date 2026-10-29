package platform

import (
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
)

type world struct {
	t       *testing.T
	p       *Platform
	db      *sqlx.DB
	org     uuid.UUID
	project uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := pgtest.Open(t)
	if err := pgstore.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, p: New(db), db: db}
	w.org = w.newOrg("acme")
	w.project = w.newProject(w.org, "APP")
	return w
}

func (w *world) newOrg(name string) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO org (name) VALUES ($1) RETURNING id`, name); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) newProject(org uuid.UUID, key string) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO project (org_id, key) VALUES ($1, $2) RETURNING id`, org, key); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) join(handle string) Principal {
	w.t.Helper()
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO member (org_id, handle) VALUES ($1, $2) RETURNING id`, w.org, handle); err != nil {
		w.t.Fatal(err)
	}
	w.exec(`INSERT INTO project_member (org_id, project_id, member_id) VALUES ($1, $2, $3)`, w.org, w.project, id)
	err := w.p.Tx(w.t.Context(), func(tx *sqlx.Tx) error {
		_, err := createBoard(tx, w.project, id, handle)
		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return MemberAgent(w.org, id, handle, "laptop", "")
}

func (w *world) system() Principal {
	return System(w.org, "test")
}

func (w *world) exec(q string, args ...any) {
	w.t.Helper()
	if _, err := w.db.Exec(q, args...); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) count(table string) int {
	w.t.Helper()
	var n int
	if err := w.db.Get(&n, `SELECT count(*) FROM `+table); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *world) must[T any](v T, err error) T {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
	return v
}

func wantCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != code {
		t.Fatalf("want error %s, got %v", code, err)
	}
	return e
}

func hasBoard(boards []Board, id uuid.UUID) bool {
	return slices.ContainsFunc(boards, func(b Board) bool { return b.ID == id })
}

func hasCard(cards []Card, id uuid.UUID) bool {
	return slices.ContainsFunc(cards, func(c Card) bool { return c.ID == id })
}
