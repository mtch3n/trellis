package platform

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
)

const ScopeGlobal = "global"

const maxRevisions = 100

type textList []string

func (l *textList) Scan(src any) error {
	return scanJSON(src, (*[]string)(l))
}

type fieldMap map[string]string

func (m *fieldMap) Scan(src any) error {
	return scanJSON(src, (*map[string]string)(m))
}

func scanJSON(src, dst any) error {
	switch v := src.(type) {
	case string:
		return json.Unmarshal([]byte(v), dst)
	case []byte:
		return json.Unmarshal(v, dst)
	}
	return fmt.Errorf("cannot scan %T as json", src)
}

type Entry struct {
	ID         uuid.UUID   `db:"id" json:"id"`
	OrgID      uuid.UUID   `db:"org_id" json:"-"`
	ProjectID  uuid.UUID   `db:"project_id" json:"-"`
	OwnerID    uuid.UUID   `db:"owner_id" json:"-"`
	Owner      string      `db:"owner" json:"owner"`
	Project    string      `db:"project" json:"project"`
	Scope      string      `db:"scope" json:"scope"`
	Slug       string      `db:"slug" json:"slug"`
	Title      string      `db:"title" json:"title"`
	Summary    string      `db:"summary" json:"summary,omitempty"`
	Body       string      `db:"body" json:"body,omitempty"`
	Template   string      `db:"template" json:"template,omitempty"`
	Provenance string      `db:"provenance" json:"provenance"`
	Private    bool        `db:"private" json:"private,omitzero"`
	Tags       textList    `db:"tags" json:"tags,omitempty"`
	Labels     textList    `db:"labels" json:"labels,omitempty"`
	Sources    textList    `db:"sources" json:"sources,omitempty"`
	Fields     fieldMap    `db:"fields" json:"fields,omitempty"`
	Recap      *string     `db:"recap" json:"recap,omitempty"`
	Version    int64       `db:"version" json:"version"`
	VerifyBy   *time.Time  `db:"verify_by" json:"verify_by,omitempty"`
	CreatedBy  string      `db:"created_by" json:"created_by"`
	CreatedAt  time.Time   `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time   `db:"updated_at" json:"updated_at"`
	DeletedAt  *time.Time  `db:"deleted_at" json:"deleted_at,omitempty"`
	Links      []EntryLink `db:"-" json:"links,omitempty"`
	Warnings   []string    `db:"-" json:"warnings,omitempty"`
}

type NewEntry struct {
	Scope      string
	Slug       string
	Title      string
	Summary    string
	Body       string
	Template   string
	Provenance string
	Private    bool
	Tags       []string
	Labels     []string
	Sources    []string
	Set        map[string]string
}

type EntryEdit struct {
	Scope     *string
	Title     *string
	Summary   *string
	Body      *string
	Private   *bool
	Tags      *[]string
	Labels    *[]string
	Sources   *[]string
	Set       map[string]string
	IfVersion int64
}

const entrySelect = `SELECT e.id, e.org_id, e.project_id, e.owner_id, m.handle AS owner, p.key AS project, e.scope, e.slug,
	e.title, e.summary, e.body, e.template, e.provenance, e.private, to_json(e.tags) AS tags, to_json(e.labels) AS labels,
	to_json(e.sources) AS sources, e.fields, e.recap, e.version, e.verify_by, e.created_by, e.created_at, e.updated_at, e.deleted_at
	FROM entry e JOIN member m ON m.id = e.owner_id JOIN project p ON p.id = e.project_id`

var provenances = []string{"authored", "prompted", "extracted"}

var reservedFields = []string{"title", "template", "summary", "provenance", "private", "tags", "labels", "sources", "created", "updated"}

var nonSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return strings.Trim(nonSlugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}

func (e Entry) target() Target {
	scope := e.Scope
	if scope == ScopeGlobal {
		scope = ScopeProject
	}
	return Target{OwnerID: e.OwnerID, Scope: scope}
}

func (e Entry) nearness() int {
	switch e.Scope {
	case ScopePersonal:
		return 0
	case ScopeProject:
		return 1
	default:
		return 2
	}
}

func (e Entry) fieldValues() map[string][]string {
	fields := map[string][]string{
		"title": {e.Title}, "summary": {e.Summary}, "provenance": {e.Provenance},
		"tags": e.Tags, "labels": e.Labels, "sources": e.Sources,
	}
	for k, v := range e.Fields {
		fields[k] = []string{v}
	}
	return fields
}

func errEntryNotFound(slug string) *Error {
	return ErrNotFound("entry_not_found", "no entry "+slug+" in this project", "vault_list")
}

func errEntryExists(slug string, id uuid.UUID) *Error {
	e := ErrConflict("entry_exists", "an entry "+slug+" already exists in this scope", "vault_show "+slug+", or choose another slug")
	if id != (uuid.UUID{}) {
		e.Detail = map[string]any{"id": id.String(), "slug": slug}
	}
	return e
}

func entryDecide(pr Principal, proj projectAccess, e Entry, op Op, slug string) error {
	v := proj.verdict(pr, e.target(), op)
	if v == NotOwner {
		return ErrPolicy("not_owner", "only the entry's owner may do this", "vault_show "+slug)
	}
	return v.err(errEntryNotFound(slug))
}

func reachable(proj projectAccess, e Entry) bool {
	if e.Scope == ScopeGlobal {
		return e.OrgID == proj.OrgID
	}
	return e.ProjectID == proj.ID
}

func findEntry(tx *sqlx.Tx, pr Principal, proj projectAccess, slug string, op Op, lock, trashed bool) (Entry, error) {
	slug = strings.TrimSpace(slug)
	state := `e.deleted_at IS NULL`
	if trashed {
		state = `e.deleted_at IS NOT NULL`
	}
	q := entrySelect + ` WHERE (e.slug = $1 OR e.id::text = $1) AND ` + state + `
		AND ((e.project_id = $2 AND e.scope <> 'global') OR (e.org_id = $3 AND e.scope = 'global'))
		ORDER BY e.deleted_at DESC NULLS LAST`
	if lock {
		q += ` FOR UPDATE OF e`
	}
	var all []Entry
	if err := tx.Select(&all, q, slug, proj.ID, proj.OrgID); err != nil {
		return Entry{}, err
	}
	seen := slices.DeleteFunc(all, func(e Entry) bool { return proj.verdict(pr, e.target(), OpRead) != Allow })
	slices.SortStableFunc(seen, func(a, b Entry) int { return cmp.Compare(a.nearness(), b.nearness()) })
	switch {
	case len(seen) == 0:
		return Entry{}, errEntryNotFound(slug)
	case !trashed && len(seen) > 1 && seen[0].nearness() == seen[1].nearness():
		return Entry{}, ErrUsage("ambiguous_entry", "more than one entry you see is named "+slug, "vault_list, then name the entry by its id")
	}
	return seen[0], entryDecide(pr, proj, seen[0], op, slug)
}

func liveTwin(tx *sqlx.Tx, e Entry) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.Get(&id, `SELECT id FROM entry WHERE slug = $1 AND scope = $2 AND deleted_at IS NULL AND id <> $3
		AND CASE $2 WHEN 'personal' THEN project_id = $4 AND owner_id = $5
		            WHEN 'project' THEN project_id = $4
		            ELSE org_id = $6 END`,
		e.Slug, e.Scope, e.ID, e.ProjectID, e.OwnerID, e.OrgID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.UUID{}, nil
	}
	return id, err
}

func loadEntry(tx *sqlx.Tx, pr Principal, proj projectAccess, id uuid.UUID) (Entry, error) {
	var e Entry
	if err := tx.Get(&e, entrySelect+` WHERE e.id = $1`, id); err != nil {
		return e, err
	}
	var err error
	e.Links, err = visibleLinks(tx, pr, proj, id)
	return e, err
}

func recordEntryEvent(tx *sqlx.Tx, pr Principal, proj projectAccess, entryID uuid.UUID, kind string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO event (org_id, project_id, entry_id, kind, actor, member_id, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		proj.OrgID, proj.ID, entryID, kind, pr.Actor(), pr.MemberID, string(raw))
	return err
}

func cleanList(in []string) []string {
	out := []string{}
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func checkScope(scope string) error {
	if !slices.Contains([]string{ScopePersonal, ScopeProject, ScopeGlobal}, scope) {
		return ErrUsage("bad_scope", `scope must be personal, project or global, not "`+scope+`"`, "omit scope for a personal entry")
	}
	return nil
}

func checkSet(set map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(set)) {
		if slices.Contains(reservedFields, name) {
			return ErrUsage("reserved_field", `"`+name+`" is a built-in field and cannot be set as a template field`, "pass "+name+" as its own argument")
		}
	}
	return nil
}

func applyTemplate(tx *sqlx.Tx, w linkWriter, t Template, e Entry, checkSections bool) ([]string, error) {
	problems := t.violations(e.fieldValues(), e.Body, checkSections)
	unresolved, err := t.unresolved(tx, w, e.fieldValues(), e.Body)
	if err != nil {
		return nil, err
	}
	problems = append(problems, unresolved...)
	return problems, t.enforce(problems)
}

func (p *Platform) CreateEntry(ctx context.Context, pr Principal, project string, in NewEntry) (Entry, error) {
	var out Entry
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e := Entry{OrgID: proj.OrgID, ProjectID: proj.ID, OwnerID: pr.MemberID, Scope: ScopePersonal}
		if err := proj.verdict(pr, e.target(), OpWrite).err(errProjectNotFound()); err != nil {
			return err
		}
		e.Title = strings.TrimSpace(in.Title)
		if e.Title == "" {
			return ErrUsage("title_required", "an entry needs a title", "vault_write new with a title")
		}
		e.Slug = cmp.Or(strings.TrimSpace(in.Slug), slugify(e.Title))
		if !address.ValidEntrySlug(e.Slug) {
			return ErrUsage("bad_slug", `"`+e.Slug+`" is not a valid slug: use lower-case letters, digits and hyphens, with / between directories`, "vault_write new with another slug")
		}
		scope := cmp.Or(strings.TrimSpace(in.Scope), ScopePersonal)
		if err := checkScope(scope); err != nil {
			return err
		}
		if err := gateEntryWrite(tx, pr, proj, nil, gateEntry{Scope: scope, Slug: e.Slug}); err != nil {
			return err
		}
		e.Provenance = cmp.Or(in.Provenance, "authored")
		if !slices.Contains(provenances, e.Provenance) {
			return ErrUsage("bad_provenance", "provenance must be one of "+strings.Join(provenances, ", "), "vault_write new with a provenance of "+strings.Join(provenances, ", "))
		}
		if err := checkSet(in.Set); err != nil {
			return err
		}
		e.Summary, e.Body, e.Template, e.Private = in.Summary, in.Body, in.Template, in.Private
		e.Tags, e.Labels, e.Sources = cleanList(in.Tags), cleanList(in.Labels), cleanList(in.Sources)
		e.Fields = fieldMap(maps.Clone(in.Set))
		if e.Fields == nil {
			e.Fields = fieldMap{}
		}
		w := linkWriter{proj: proj, memberID: pr.MemberID}
		switch {
		case e.Template != "":
			t, err := templateNamed(tx, proj.ID, e.Template)
			if err != nil {
				return err
			}
			checkSections := e.Body != ""
			if e.Body == "" {
				e.Body = t.render(e.Title, in.Set)
			}
			if e.Warnings, err = applyTemplate(tx, w, t, e, checkSections); err != nil {
				return err
			}
		case e.Body == "":
			e.Body = "# " + e.Title + "\n"
		}
		twin, err := liveTwin(tx, e)
		if err != nil {
			return err
		}
		if twin != (uuid.UUID{}) {
			return errEntryExists(e.Slug, twin)
		}
		fields, err := json.Marshal(e.Fields)
		if err != nil {
			return err
		}
		err = tx.Get(&e.ID, `INSERT INTO entry (org_id, project_id, owner_id, slug, title, summary, body, template, provenance,
			private, tags, labels, sources, fields, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id`,
			e.OrgID, e.ProjectID, e.OwnerID, e.Slug, e.Title, e.Summary, e.Body, e.Template, e.Provenance,
			e.Private, []string(e.Tags), []string(e.Labels), []string(e.Sources), string(fields), pr.Actor())
		if isUniqueViolation(err) {
			return errEntryExists(e.Slug, uuid.UUID{})
		}
		if err != nil {
			return err
		}
		e.Version = 1
		if err := saveRevision(tx, pr, e); err != nil {
			return err
		}
		if err := syncLinks(tx, w, e.ID, e.Body, e.Sources); err != nil {
			return err
		}
		if err := fillStubs(tx, e); err != nil {
			return err
		}
		if err := recordEntryEvent(tx, pr, proj, e.ID, "entry.created", map[string]any{"slug": e.Slug}); err != nil {
			return err
		}
		warnings := e.Warnings
		out, err = loadEntry(tx, pr, proj, e.ID)
		out.Warnings = warnings
		return err
	})
	return out, err
}

func (p *Platform) ShowEntry(ctx context.Context, pr Principal, project, slug string) (Entry, error) {
	var out Entry
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpRead, false, false)
		if err != nil {
			return err
		}
		out, err = loadEntry(tx, pr, proj, e.ID)
		return err
	})
	return out, err
}

func visibleEntries(tx *sqlx.Tx, pr Principal, proj projectAccess, where string, args ...any) ([]Entry, error) {
	var all []Entry
	q := entrySelect + ` WHERE e.deleted_at IS NULL
		AND ((e.project_id = $1 AND e.scope <> 'global') OR (e.org_id = $2 AND e.scope = 'global'))` + where
	if err := tx.Select(&all, q, append([]any{proj.ID, proj.OrgID}, args...)...); err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, e := range all {
		if proj.verdict(pr, e.target(), OpRead) == Allow {
			e.Body = ""
			out = append(out, e)
		}
	}
	return out, nil
}

func (p *Platform) ListEntries(ctx context.Context, pr Principal, project string) ([]Entry, error) {
	var out []Entry
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		out, err = visibleEntries(tx, pr, proj, ` ORDER BY CASE e.scope WHEN 'personal' THEN 0 WHEN 'project' THEN 1 ELSE 2 END, e.slug, m.handle`)
		return err
	})
	return out, err
}

func (p *Platform) EditEntry(ctx context.Context, pr Principal, project, slug string, in EntryEdit) (Entry, error) {
	var out Entry
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpWrite, true, false)
		if err != nil {
			return err
		}
		if in.Scope == nil && in.Title == nil && in.Summary == nil && in.Body == nil && in.Private == nil && in.Tags == nil &&
			in.Labels == nil && in.Sources == nil && len(in.Set) == 0 {
			return ErrUsage("nothing_to_edit", "an edit needs a field to change", "vault_write edit with a title, summary, body or field")
		}
		if err := checkVersion(e, in.IfVersion, "an edit"); err != nil {
			return err
		}
		if err := checkSet(in.Set); err != nil {
			return err
		}
		before := e.gate()
		if in.Title != nil {
			if e.Title = strings.TrimSpace(*in.Title); e.Title == "" {
				return ErrUsage("title_required", "an entry needs a title", "vault_write edit with a title")
			}
		}
		if in.Summary != nil {
			e.Summary = *in.Summary
		}
		if in.Body != nil {
			e.Body = *in.Body
		}
		if in.Private != nil {
			e.Private = *in.Private
		}
		if in.Tags != nil {
			e.Tags = cleanList(*in.Tags)
		}
		if in.Labels != nil {
			e.Labels = cleanList(*in.Labels)
		}
		if in.Sources != nil {
			e.Sources = cleanList(*in.Sources)
		}
		e.Fields = fieldMap(maps.Clone(map[string]string(e.Fields)))
		if e.Fields == nil {
			e.Fields = fieldMap{}
		}
		for k, v := range in.Set {
			if v == "" {
				delete(e.Fields, k)
			} else {
				e.Fields[k] = v
			}
		}
		w := linkWriter{proj: proj, memberID: pr.MemberID}
		var warnings []string
		if e.Template != "" {
			t, err := templateNamed(tx, proj.ID, e.Template)
			if err != nil {
				return err
			}
			if warnings, err = applyTemplate(tx, w, t, e, in.Body != nil); err != nil {
				return err
			}
		}
		after := e.gate()
		if in.Scope != nil {
			after.Scope = strings.TrimSpace(*in.Scope)
			if err := checkScope(after.Scope); err != nil {
				return err
			}
		}
		if err := gateEntryWrite(tx, pr, proj, &before, after); err != nil {
			return err
		}
		if after.Scope != before.Scope {
			return ErrUsage("scope_change", "an edit does not change an entry's scope", "vault_share demote "+e.Slug)
		}
		fields, err := json.Marshal(e.Fields)
		if err != nil {
			return err
		}
		if err := tx.Get(&e.Version, `UPDATE entry SET title = $2, summary = $3, body = $4, private = $5, tags = $6, labels = $7,
			sources = $8, fields = $9, version = version + 1, updated_at = now() WHERE id = $1 RETURNING version`,
			e.ID, e.Title, e.Summary, e.Body, e.Private, []string(e.Tags), []string(e.Labels), []string(e.Sources), string(fields)); err != nil {
			return err
		}
		if err := saveRevision(tx, pr, e); err != nil {
			return err
		}
		if err := syncLinks(tx, w, e.ID, e.Body, e.Sources); err != nil {
			return err
		}
		if err := recordEntryEvent(tx, pr, proj, e.ID, "entry.edited", map[string]any{"version": e.Version}); err != nil {
			return err
		}
		out, err = loadEntry(tx, pr, proj, e.ID)
		out.Warnings = warnings
		return err
	})
	return out, err
}
