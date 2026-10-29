package platform

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"uuid"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.md
var builtInTemplates embed.FS

type Template struct {
	Name     string              `json:"name"`
	Enforce  string              `json:"enforce"`
	Required []string            `json:"required,omitempty"`
	Choices  map[string][]string `json:"choices,omitempty"`
	Resolve  []string            `json:"resolve,omitempty"`
	Body     string              `json:"body"`
	Raw      string              `json:"raw"`
	BuiltIn  bool                `json:"built_in"`
	Version  int64               `json:"version"`
}

type templateRules struct {
	Enforce  string              `yaml:"enforce,omitempty"`
	Required []string            `yaml:"required,omitempty"`
	Choices  map[string][]string `yaml:"choices,omitempty"`
	Resolve  []string            `yaml:"resolve,omitempty"`
}

const optionalMarker = "<!-- optional -->"

var (
	sectionHeadingRE = regexp.MustCompile(`(?m)^## (.+?)[ \t]*$`)
	placeholderRE    = regexp.MustCompile(`\{\{([a-zA-Z0-9_]+)\}\}`)
	fenceRE          = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")
)

func errUnknownTemplate(name string) *Error {
	return ErrUsage("unknown_template", "no template "+name+" in this project", "template list")
}

func splitHeader(raw string) (header, body string, ok bool) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", s, false
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return "", s, false
	}
	return s[4 : 4+end], strings.TrimPrefix(s[4+end+4:], "\n"), true
}

func parseTemplate(name, raw string) (Template, error) {
	if !address.ValidSlug(name) {
		return Template{}, ErrUsage("bad_template_name",
			`"`+name+`" is not a valid template name: use lower-case letters and digits joined by single hyphens`, "template list")
	}
	header, body, ok := splitHeader(raw)
	var rules templateRules
	if ok {
		if err := yaml.Unmarshal([]byte(header), &rules); err != nil {
			return Template{}, ErrUsage("bad_template", name+": the template's frontmatter does not parse: "+err.Error(), "template edit "+name)
		}
	}
	var problems []string
	if rules.Enforce != "" && rules.Enforce != "reject" && rules.Enforce != "warn" {
		problems = append(problems, `enforce must be "reject" or "warn", not "`+rules.Enforce+`"`)
	}
	for _, field := range slices.Sorted(maps.Keys(rules.Choices)) {
		if len(rules.Choices[field]) == 0 {
			problems = append(problems, "choices."+field+" is empty")
		}
	}
	if len(problems) > 0 {
		return Template{}, ErrUsage("bad_template", name+": "+strings.Join(problems, "; "), "template edit "+name)
	}
	if rules.Enforce == "" {
		rules.Enforce = "warn"
	}
	return Template{Name: name, Enforce: rules.Enforce, Required: rules.Required, Choices: rules.Choices,
		Resolve: rules.Resolve, Body: body, Raw: raw}, nil
}

func builtInTemplate(name string) (Template, bool) {
	raw, err := builtInTemplates.ReadFile("templates/" + name + ".md")
	if err != nil {
		return Template{}, false
	}
	t, err := parseTemplate(name, string(raw))
	if err != nil {
		panic(err)
	}
	t.BuiltIn, t.Version = true, 1
	return t, true
}

func builtInTemplateNames() []string {
	files, _ := builtInTemplates.ReadDir("templates")
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, strings.TrimSuffix(f.Name(), ".md"))
	}
	return names
}

type templateRow struct {
	Name    string `db:"name"`
	Raw     string `db:"raw"`
	BuiltIn bool   `db:"built_in"`
	Version int64  `db:"version"`
}

func (r templateRow) template() (Template, error) {
	t, err := parseTemplate(r.Name, r.Raw)
	t.BuiltIn, t.Version = r.BuiltIn, r.Version
	return t, err
}

func projectTemplates(tx *sqlx.Tx, projectID uuid.UUID) ([]Template, error) {
	byName := map[string]Template{}
	for _, name := range builtInTemplateNames() {
		byName[name], _ = builtInTemplate(name)
	}
	var rows []templateRow
	if err := tx.Select(&rows, `SELECT name, raw, built_in, version FROM vault_template WHERE project_id = $1`, projectID); err != nil {
		return nil, err
	}
	for _, r := range rows {
		t, err := r.template()
		if err != nil {
			return nil, err
		}
		byName[r.Name] = t
	}
	out := make([]Template, 0, len(byName))
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		out = append(out, byName[name])
	}
	return out, nil
}

func templateNamed(tx *sqlx.Tx, projectID uuid.UUID, name string) (Template, error) {
	var r templateRow
	err := tx.Get(&r, `SELECT name, raw, built_in, version FROM vault_template WHERE project_id = $1 AND name = $2`, projectID, name)
	if errors.Is(err, sql.ErrNoRows) {
		if t, ok := builtInTemplate(name); ok {
			return t, nil
		}
		return Template{}, errUnknownTemplate(name)
	}
	if err != nil {
		return Template{}, err
	}
	return r.template()
}

func templateSections(body string) (required []string, present map[string]bool) {
	present = map[string]bool{}
	for _, m := range sectionHeadingRE.FindAllStringSubmatch(fenceRE.ReplaceAllString(body, ""), -1) {
		heading, optional := strings.CutSuffix(strings.TrimSpace(m[1]), optionalMarker)
		heading = strings.TrimSpace(heading)
		present[heading] = true
		if !optional {
			required = append(required, heading)
		}
	}
	return required, present
}

func stripOptionalMarkers(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		m := sectionHeadingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if heading, ok := strings.CutSuffix(strings.TrimSpace(m[1]), optionalMarker); ok {
			lines[i] = "## " + strings.TrimSpace(heading)
		}
	}
	return strings.Join(lines, "\n")
}

func (t Template) render(title string, fields map[string]string) string {
	return stripOptionalMarkers(placeholderRE.ReplaceAllStringFunc(t.Body, func(m string) string {
		name := m[2 : len(m)-2]
		if name == "title" {
			return title
		}
		return fields[name]
	}))
}

func (t Template) violations(fields map[string][]string, body string, checkSections bool) []string {
	var out []string
	for _, name := range t.Required {
		if !slices.ContainsFunc(fields[name], func(v string) bool { return strings.TrimSpace(v) != "" }) {
			out = append(out, "missing required field "+name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(t.Choices)) {
		for _, v := range fields[name] {
			if v != "" && !slices.Contains(t.Choices[name], v) {
				out = append(out, name+" must be one of "+strings.Join(t.Choices[name], ", ")+", not "+v)
			}
		}
	}
	if checkSections {
		required, _ := templateSections(t.Body)
		_, present := templateSections(body)
		for _, heading := range required {
			if !present[heading] {
				out = append(out, "missing section "+heading)
			}
		}
	}
	return out
}

func (t Template) unresolved(tx *sqlx.Tx, w linkWriter, fields map[string][]string, body string) ([]string, error) {
	var out []string
	for _, name := range t.Resolve {
		values := fields[name]
		if name == "body" {
			values = nil
			for _, target := range parseWikilinks(body) {
				values = append(values, "[["+target+"]]")
			}
		}
		for _, v := range values {
			ok, isRef, err := w.resolves(tx, v)
			if err != nil {
				return nil, err
			}
			if isRef && !ok {
				out = append(out, name+" cites "+strings.TrimSpace(v)+", which does not resolve")
			}
		}
	}
	return out, nil
}

func (t Template) enforce(problems []string) error {
	if len(problems) == 0 || t.Enforce != "reject" {
		return nil
	}
	return &Error{Kind: KindUsage, Code: "template_violation", Msg: "the entry does not meet template " + t.Name + ": " + strings.Join(problems, "; "),
		Fix: "fill what the template asks for, or template show " + t.Name, Detail: map[string]any{"problems": problems}}
}

func (p *Platform) ListTemplates(ctx context.Context, pr Principal, project string) ([]Template, error) {
	var out []Template
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		out, err = projectTemplates(tx, proj.ID)
		return err
	})
	return out, err
}

func (p *Platform) ShowTemplate(ctx context.Context, pr Principal, project, name string) (Template, error) {
	var t Template
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		t, err = templateNamed(tx, proj.ID, name)
		return err
	})
	return t, err
}

func (p *Platform) NewTemplate(ctx context.Context, pr Principal, project, name, raw string) (Template, error) {
	return p.writeTemplate(ctx, pr, project, name, raw, func(tx *sqlx.Tx, proj projectAccess, current Template, exists bool) error {
		if exists {
			return ErrConflict("template_exists", "template "+name+" already exists in this project", "template edit "+name)
		}
		_, err := tx.Exec(`INSERT INTO vault_template (project_id, name, raw) VALUES ($1, $2, $3)`, proj.ID, name, raw)
		if isUniqueViolation(err) {
			return ErrConflict("template_exists", "template "+name+" already exists in this project", "template edit "+name)
		}
		return err
	})
}

func (p *Platform) EditTemplate(ctx context.Context, pr Principal, project, name, raw string, ifVersion int64) (Template, error) {
	return p.writeTemplate(ctx, pr, project, name, raw, func(tx *sqlx.Tx, proj projectAccess, current Template, exists bool) error {
		if !exists {
			return errUnknownTemplate(name)
		}
		if ifVersion != current.Version {
			return ErrConflict("conflict", "template "+name+" changed since you read it", "template show "+name)
		}
		_, err := tx.Exec(`INSERT INTO vault_template (project_id, name, raw, built_in, version) VALUES ($1, $2, $3, $4, $5 + 1)
			ON CONFLICT (project_id, name) DO UPDATE SET raw = EXCLUDED.raw, version = EXCLUDED.version, updated_at = now()`,
			proj.ID, name, raw, current.BuiltIn, current.Version)
		return err
	})
}

func (p *Platform) writeTemplate(ctx context.Context, pr Principal, project, name, raw string,
	write func(tx *sqlx.Tx, proj projectAccess, current Template, exists bool) error) (Template, error) {
	var t Template
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		if _, err := parseTemplate(name, raw); err != nil {
			return err
		}
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, proj.ID.String()+"/template/"+name); err != nil {
			return err
		}
		current, err := templateNamed(tx, proj.ID, name)
		exists := err == nil
		if err != nil && !IsCode(err, "unknown_template") {
			return err
		}
		if err := write(tx, proj, current, exists); err != nil {
			return err
		}
		t, err = templateNamed(tx, proj.ID, name)
		return err
	})
	return t, err
}
