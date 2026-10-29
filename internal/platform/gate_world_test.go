package platform

import (
	"encoding/json/v2"
	"testing"
	"time"
	"uuid"
)

type gateSeed struct {
	slug     string
	body     string
	scope    string
	template string
	private  bool
	sources  []string
	fields   map[string]any
}

func (w *world) gateSeedEntry(owner Principal, s gateSeed) uuid.UUID {
	w.t.Helper()
	if s.scope == "" {
		s.scope = "personal"
	}
	if s.fields == nil {
		s.fields = map[string]any{}
	}
	if s.sources == nil {
		s.sources = []string{}
	}
	fields, err := json.Marshal(s.fields)
	if err != nil {
		w.t.Fatal(err)
	}
	var verifyBy *time.Time
	if s.scope == "global" {
		verifyBy = new(time.Now().Add(180 * 24 * time.Hour))
	}
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO entry (org_id, project_id, owner_id, scope, slug, title, body, template, private,
		sources, fields, verify_by, created_by)
		VALUES ($1, (SELECT project_id FROM project_member WHERE member_id = $2 ORDER BY created_at LIMIT 1),
		        $2, $3, $4, $4, $5, $6, $7, $8, $9, $10, 'test') RETURNING id`,
		owner.OrgID, owner.MemberID, s.scope, s.slug, s.body, s.template, s.private, s.sources, fields, verifyBy); err != nil {
		w.t.Fatal(err)
	}
	return id
}

type gateRow struct {
	Scope    string    `db:"scope"`
	OwnerID  uuid.UUID `db:"owner_id"`
	Version  int64     `db:"version"`
	Body     string    `db:"body"`
	Labels   string    `db:"labels"`
	VerifyBy string    `db:"verify_by"`
	Project  string    `db:"project"`
}

func (w *world) entryRow(id uuid.UUID) gateRow {
	w.t.Helper()
	var r gateRow
	if err := w.db.Get(&r, `SELECT e.scope, e.owner_id, e.version, e.body, array_to_string(e.labels, ',') AS labels,
		COALESCE(e.verify_by::text, '') AS verify_by, p.key AS project FROM entry e JOIN project p ON p.id = e.project_id WHERE e.id = $1`, id); err != nil {
		w.t.Fatal(err)
	}
	return r
}

func (w *world) joinProject(handle, key string) Principal {
	w.t.Helper()
	var project uuid.UUID
	if err := w.db.Get(&project, `SELECT id FROM project WHERE org_id = $1 AND key = $2`, w.org, key); err != nil {
		project = w.newProject(w.org, key)
	}
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO member (org_id, handle) VALUES ($1, $2) RETURNING id`, w.org, handle); err != nil {
		w.t.Fatal(err)
	}
	w.exec(`INSERT INTO project_member (org_id, project_id, member_id) VALUES ($1, $2, $3)`, w.org, project, id)
	return MemberAgent(w.org, id, handle, "laptop", "")
}

func gateChecks(t *testing.T, err error) []GateCheck {
	t.Helper()
	e := wantCode(t, err, "gate_failed")
	checks, ok := e.Detail.([]GateCheck)
	if !ok {
		t.Fatalf("detail = %#v", e.Detail)
	}
	return checks
}
