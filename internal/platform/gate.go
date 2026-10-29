package platform

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
	"github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
	"gopkg.in/yaml.v3"
)

var gateCheckNames = []string{"private", "secret", "template", "sources", "duplicate"}

type GateCheck struct {
	Check   string `json:"check"`
	Problem string `json:"problem"`
	Fix     string `json:"fix"`
}

type ShareResult struct {
	ID       uuid.UUID  `json:"id"`
	Slug     string     `json:"slug"`
	Scope    string     `json:"scope"`
	Labels   []string   `json:"labels"`
	VerifyBy *time.Time `json:"verify_by,omitempty"`
}

type gateEntry struct {
	ID       uuid.UUID
	OwnerID  uuid.UUID
	Scope    string
	Slug     string
	Title    string
	Summary  string
	Body     string
	Template string
	Private  bool
	Sources  []string
	Fields   map[string]any
}

type shareRow struct {
	ID        uuid.UUID  `db:"id"`
	OrgID     uuid.UUID  `db:"org_id"`
	ProjectID uuid.UUID  `db:"project_id"`
	OwnerID   uuid.UUID  `db:"owner_id"`
	Scope     string     `db:"scope"`
	Slug      string     `db:"slug"`
	Title     string     `db:"title"`
	Summary   string     `db:"summary"`
	Body      string     `db:"body"`
	Template  string     `db:"template"`
	Private   bool       `db:"private"`
	Sources   []byte     `db:"sources"`
	Fields    []byte     `db:"fields"`
	Labels    []byte     `db:"labels"`
	VerifyBy  *time.Time `db:"verify_by"`
}

const shareSelect = `SELECT e.id, e.org_id, e.project_id, e.owner_id, e.scope, e.slug, e.title, e.summary, e.body,
	e.template, e.private, to_jsonb(e.sources) AS sources, e.fields, to_jsonb(e.labels) AS labels, e.verify_by
	FROM entry e`

func (r shareRow) gate() (gateEntry, error) {
	e := gateEntry{ID: r.ID, OwnerID: r.OwnerID, Scope: r.Scope, Slug: r.Slug, Title: r.Title, Summary: r.Summary,
		Body: r.Body, Template: r.Template, Private: r.Private}
	if err := json.Unmarshal(r.Sources, &e.Sources); err != nil {
		return e, err
	}
	return e, json.Unmarshal(r.Fields, &e.Fields)
}

func (r shareRow) result() (ShareResult, error) {
	out := ShareResult{ID: r.ID, Slug: r.Slug, Scope: r.Scope, VerifyBy: r.VerifyBy}
	return out, json.Unmarshal(r.Labels, &out.Labels)
}

func errEntryMissing(slug string) *Error {
	return ErrNotFound("entry_not_found", "no entry "+slug+" in this project", "vault_list")
}

func findShared(tx *sqlx.Tx, pr Principal, proj projectAccess, slug string, order []string, lock bool) (shareRow, error) {
	q := shareSelect + ` WHERE e.deleted_at IS NULL AND e.slug = $1 AND (
		(e.project_id = $2 AND e.scope = 'project') OR
		(e.project_id = $2 AND e.scope = 'personal' AND (e.owner_id = $3 OR $4)) OR
		(e.org_id = $5 AND e.scope = 'global'))
		ORDER BY array_position($6::text[], e.scope) LIMIT 1`
	if lock {
		q += ` FOR UPDATE OF e`
	}
	var r shareRow
	err := tx.Get(&r, q, slug, proj.ID, pr.MemberID, pr.IsSystem(), proj.OrgID, order)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errEntryMissing(slug)
	}
	return r, err
}

func reloadShared(tx *sqlx.Tx, id uuid.UUID) (ShareResult, error) {
	var r shareRow
	if err := tx.Get(&r, shareSelect+` WHERE e.id = $1`, id); err != nil {
		return ShareResult{}, err
	}
	return r.result()
}

func refuseSystem(pr Principal) error {
	if pr.IsSystem() {
		return ReadOnly.err(nil)
	}
	return nil
}

func execUnique(tx *sqlx.Tx, q string, args ...any) (bool, error) {
	if _, err := tx.Exec(`SAVEPOINT unique_write`); err != nil {
		return false, err
	}
	_, err := tx.Exec(q, args...)
	if isUniqueViolation(err) {
		_, err = tx.Exec(`ROLLBACK TO SAVEPOINT unique_write`)
		return true, err
	}
	return false, err
}

func shareEvent(tx *sqlx.Tx, pr Principal, r shareRow, kind string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO event (org_id, project_id, entry_id, kind, actor, member_id, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.OrgID, r.ProjectID, r.ID, kind, pr.Actor(), pr.MemberID, raw)
	return err
}

func gateFailed(slug string, failed []GateCheck) *Error {
	problems := make([]string, 0, len(failed))
	for _, c := range failed {
		problems = append(problems, c.Check+": "+c.Problem+" (fix: "+c.Fix+")")
	}
	return &Error{Kind: KindPolicy, Code: "gate_failed",
		Msg:    "the gate refused " + slug + ": " + strings.Join(problems, "; "),
		Fix:    "fix each failed check, then vault_share publish " + slug + " again",
		Detail: failed}
}

func (p *Platform) Publish(ctx context.Context, pr Principal, project, slug string) (ShareResult, error) {
	var out ShareResult
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if err := refuseSystem(pr); err != nil {
			return err
		}
		r, err := findShared(tx, pr, proj, slug, []string{ScopePersonal, ScopeProject, "global"}, true)
		if err != nil {
			return err
		}
		if r.Scope != ScopePersonal {
			return ErrUsage("already_published", slug+" is already a "+r.Scope+" entry", "vault_show "+slug)
		}
		e, err := r.gate()
		if err != nil {
			return err
		}
		failed, err := runGate(tx, pr, proj, e)
		if err != nil {
			return err
		}
		if len(failed) > 0 {
			return gateFailed(slug, failed)
		}
		taken, err := execUnique(tx, `UPDATE entry SET scope = 'project', updated_at = now(),
			labels = CASE WHEN 'gate' = ANY (labels) THEN labels ELSE array_append(labels, 'gate') END
			WHERE id = $1`, r.ID)
		if err != nil {
			return err
		}
		if taken {
			dup, err := duplicateCheck(tx, proj, e)
			if err != nil {
				return err
			}
			if dup == nil {
				return ErrConflict("conflict", slug+" changed while it was published", "vault_share publish "+slug+" again")
			}
			return gateFailed(slug, []GateCheck{*dup})
		}
		if err := shareEvent(tx, pr, r, "entry.published", map[string]any{"slug": slug, "checks": gateCheckNames}); err != nil {
			return err
		}
		out, err = reloadShared(tx, r.ID)
		return err
	})
	return out, err
}

func gateEntryWrite(tx *sqlx.Tx, pr Principal, proj projectAccess, before *gateEntry, after gateEntry) error {
	from := ""
	if before != nil {
		from = before.Scope
	}
	switch {
	case after.Scope == ScopeProject && from != ScopeProject:
		return ErrUsage("publish_required", "an entry enters project scope only through the gate",
			"write it as personal, then vault_share publish "+after.Slug)
	case after.Scope == "global" && from != "global":
		return ErrUsage("promote_required", "an entry enters the global vault only through promote",
			"vault_share promote "+after.Slug+" with a reason")
	case after.Scope != ScopeProject:
		return nil
	}
	failed, err := runGate(tx, pr, proj, after)
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		return gateFailed(after.Slug, failed)
	}
	return nil
}

func runGate(tx *sqlx.Tx, pr Principal, proj projectAccess, e gateEntry) ([]GateCheck, error) {
	var failed []GateCheck
	if e.Private {
		failed = append(failed, GateCheck{Check: "private", Problem: "the entry is private",
			Fix: "vault_write edit with private false"})
	}
	secrets, err := secretCheck(e)
	if err != nil {
		return nil, err
	}
	failed = append(failed, secrets...)
	tmpl, err := templateCheck(tx, proj, e)
	if err != nil {
		return nil, err
	}
	failed = append(failed, tmpl...)
	cited, err := sourcesCheck(tx, pr, proj, e)
	if err != nil {
		return nil, err
	}
	failed = append(failed, cited...)
	dup, err := duplicateCheck(tx, proj, e)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		failed = append(failed, *dup)
	}
	return failed, nil
}

var gitleaksConfig = sync.OnceValues(func() (config.Config, error) {
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return config.Config{}, err
	}
	return d.Config, nil
})

func secretCheck(e gateEntry) ([]GateCheck, error) {
	cfg, err := gitleaksConfig()
	if err != nil {
		return nil, err
	}
	var failed []GateCheck
	for _, part := range []struct{ name, text string }{{"title", e.Title}, {"summary", e.Summary}, {"body", e.Body}} {
		d := detect.NewDetector(cfg)
		d.IgnoreGitleaksAllow = true
		for _, f := range d.DetectString(part.text) {
			line := f.StartLine + 1
			failed = append(failed, GateCheck{Check: "secret",
				Problem: fmt.Sprintf("line %d of the %s holds a secret (%s)", line, part.name, f.RuleID),
				Fix:     fmt.Sprintf("remove the secret from line %d of the %s and rotate it", line, part.name)})
		}
	}
	return failed, nil
}

func templateCheck(tx *sqlx.Tx, proj projectAccess, e gateEntry) ([]GateCheck, error) {
	if e.Template == "" {
		return nil, nil
	}
	var raw string
	err := tx.Get(&raw, `SELECT raw FROM vault_template WHERE project_id = $1 AND name = $2`, proj.ID, e.Template)
	if errors.Is(err, sql.ErrNoRows) {
		return []GateCheck{{Check: "template", Problem: "template " + e.Template + " does not exist",
			Fix: "template_list, then vault_write edit with a template that exists"}}, nil
	}
	if err != nil {
		return nil, err
	}
	var rules struct {
		Required []string `yaml:"required"`
	}
	if rest, ok := strings.CutPrefix(raw, "---\n"); ok {
		header, _, _ := strings.Cut(rest, "\n---")
		if err := yaml.Unmarshal([]byte(header), &rules); err != nil {
			return []GateCheck{{Check: "template", Problem: "template " + e.Template + " does not parse",
				Fix: "template edit " + e.Template}}, nil
		}
	}
	values := map[string][]string{"title": {e.Title}, "summary": {e.Summary}, "sources": e.Sources}
	for k, v := range e.Fields {
		switch v := v.(type) {
		case nil:
		case []any:
			for _, item := range v {
				values[k] = append(values[k], fmt.Sprint(item))
			}
		default:
			values[k] = append(values[k], fmt.Sprint(v))
		}
	}
	var failed []GateCheck
	for _, name := range rules.Required {
		if !hasNonBlank(values[name]) {
			failed = append(failed, GateCheck{Check: "template",
				Problem: "required field " + name + " of template " + e.Template + " is empty",
				Fix:     "vault_write edit with a value for " + name})
		}
	}
	return failed, nil
}

func hasNonBlank(vs []string) bool {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func sourcesCheck(tx *sqlx.Tx, pr Principal, proj projectAccess, e gateEntry) ([]GateCheck, error) {
	var failed []GateCheck
	for _, src := range e.Sources {
		ok, err := sourceExists(tx, pr, proj, strings.TrimSpace(src))
		if err != nil {
			return nil, err
		}
		if !ok {
			failed = append(failed, GateCheck{Check: "sources", Problem: "source " + src + " does not exist",
				Fix: "vault_write edit to correct or remove the source " + src})
		}
	}
	return failed, nil
}

func sourceExists(tx *sqlx.Tx, pr Principal, proj projectAccess, src string) (bool, error) {
	if inner, ok := strings.CutPrefix(src, "[["); ok {
		target, ok := strings.CutSuffix(inner, "]]")
		if !ok {
			return true, nil
		}
		target, _, _ = strings.Cut(target, "|")
		return visibleEntry(tx, pr, proj.Key, strings.TrimSpace(target))
	}
	if strings.HasPrefix(src, "/") {
		a, err := address.Parse(src)
		if err != nil {
			return true, nil
		}
		switch a.Collection {
		case address.CollectionCards:
			return visibleCard(tx, pr, a.Project, a.Name)
		case address.CollectionVault:
			return visibleEntry(tx, pr, a.Project, a.Name)
		}
		return true, nil
	}
	if key, _, ok := strings.CutLast(strings.ToUpper(src), "-"); ok && key == proj.Key && address.ValidCardRef(strings.ToUpper(src)) {
		return visibleCard(tx, pr, proj.Key, strings.ToUpper(src))
	}
	return true, nil
}

func visibleEntry(tx *sqlx.Tx, pr Principal, key, slug string) (bool, error) {
	var ok bool
	err := tx.Get(&ok, `SELECT EXISTS (SELECT 1 FROM entry e WHERE e.deleted_at IS NULL AND e.slug = $1 AND e.org_id = $2 AND (
		e.scope = 'global' AND $3 = 'GLOBAL' OR
		e.scope <> 'global' AND e.project_id = (SELECT p.id FROM project p WHERE p.org_id = $2 AND p.key = $3)
		AND EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = e.project_id AND pm.member_id = $4)
		AND (e.scope = 'project' OR e.owner_id = $4)))`,
		slug, pr.OrgID, strings.ToUpper(key), pr.MemberID)
	return ok, err
}

func visibleCard(tx *sqlx.Tx, pr Principal, key, ref string) (bool, error) {
	var ok bool
	err := tx.Get(&ok, `SELECT EXISTS (SELECT 1 FROM card c JOIN board b ON b.id = c.board_id JOIN project p ON p.id = c.project_id
		WHERE p.org_id = $1 AND p.key = $2 AND c.ref = $3
		AND EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = p.id AND pm.member_id = $4)
		AND (b.scope = 'project' OR b.owner_id = $4))`,
		pr.OrgID, strings.ToUpper(key), strings.ToUpper(ref), pr.MemberID)
	return ok, err
}

func duplicateCheck(tx *sqlx.Tx, proj projectAccess, e gateEntry) (*GateCheck, error) {
	var other struct {
		Slug     string `db:"slug"`
		SameSlug bool   `db:"same_slug"`
	}
	err := tx.Get(&other, `SELECT slug, slug = $3 AS same_slug FROM entry
		WHERE project_id = $1 AND scope = 'project' AND deleted_at IS NULL AND id <> $2
		AND (slug = $3 OR body_key = md5(btrim(regexp_replace(lower($4), '\s+', ' ', 'g'))))
		ORDER BY slug = $3 DESC, slug LIMIT 1`, proj.ID, e.ID, e.Slug, e.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if other.SameSlug {
		return &GateCheck{Check: "duplicate", Problem: "project entry " + other.Slug + " already has this slug",
			Fix: "vault_show " + other.Slug + " and edit it, or choose another slug"}, nil
	}
	return &GateCheck{Check: "duplicate", Problem: "project entry " + other.Slug + " already has this body",
		Fix: "vault_show " + other.Slug + " and edit it instead"}, nil
}
