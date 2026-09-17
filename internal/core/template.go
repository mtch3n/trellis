package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/vpath"
	"gopkg.in/yaml.v3"
)

// TemplateRules is a template file's own frontmatter — the rules a document
// created from it must satisfy. It is a different shape from Frontmatter (a
// document's metadata), so it gets its own type rather than reusing it.
type TemplateRules struct {
	// Enforce is "reject" or "warn". Empty means warn.
	Enforce string `yaml:"enforce,omitempty"`
	// Required names fields that must be supplied and non-blank. A
	// required list field (sources, see TRELLIS-35) means at least one
	// non-blank item, not "as many as the template has in mind".
	Required []string `yaml:"required,omitempty"`
	// Choices restricts a field to a closed list of values. A field may
	// appear here without being Required, in which case it may be
	// omitted, but if supplied it must be one of its choices.
	Choices map[string][]string `yaml:"choices,omitempty"`
	// Verify names fields (or the literal "body") whose internal
	// references must resolve. Anything that is not a wikilink or an
	// absolute Trellis address passes unchecked: a URL, a path:lines
	// pointer and prose all cite without being verifiable.
	Verify []string `yaml:"verify,omitempty"`
}

// Template is a parsed template file: its rules, its skeleton body (with
// placeholders still in it), and where it lives on disk.
type Template struct {
	Name     string
	Path     string
	Enforce  string
	Required []string
	Choices  map[string][]string
	Verify   []string
	Body     string
	BuiltIn  bool
}

// TemplateSection is one "## " heading a template's skeleton declares, and
// whether a document must have it to satisfy the template.
type TemplateSection struct {
	Heading  string
	Optional bool
}

const optionalMarker = "<!-- optional -->"

// sectionHeadingRE matches a level-2 heading exactly: two "#" characters,
// then a space. "### Option A" does not match, because its third character
// is "#", not a space — decision.md's placeholder subsections must never be
// mistaken for required sections.
var sectionHeadingRE = regexp.MustCompile(`(?m)^## (.+?)[ \t]*$`)

// placeholderRE matches {{name}} in a template body.
var placeholderRE = regexp.MustCompile(`\{\{([a-zA-Z0-9_]+)\}\}`)

// templateSections lists a skeleton's "## " headings in order, skipping code
// spans and fences the same way ParseWikilinks does.
func templateSections(body string) []TemplateSection {
	clean := fenceRE.ReplaceAllString(body, "")
	var out []TemplateSection
	for _, m := range sectionHeadingRE.FindAllStringSubmatch(clean, -1) {
		heading := strings.TrimSpace(m[1])
		optional := strings.HasSuffix(heading, optionalMarker)
		if optional {
			heading = strings.TrimSpace(strings.TrimSuffix(heading, optionalMarker))
		}
		out = append(out, TemplateSection{Heading: heading, Optional: optional})
	}
	return out
}

// requiredSections is the subset of a skeleton's sections a document must
// have to satisfy the template — every one not marked <!-- optional -->.
func requiredSections(body string) []string {
	var out []string
	for _, s := range templateSections(body) {
		if !s.Optional {
			out = append(out, s.Heading)
		}
	}
	return out
}

// presentSections is the set of "## " headings an actual document body has.
func presentSections(body string) map[string]bool {
	set := map[string]bool{}
	for _, s := range templateSections(body) {
		set[s.Heading] = true
	}
	return set
}

// stripOptionalMarkers removes the <!-- optional --> marker from a rendered
// document's headings. Only the skeleton carries the marker; the document it
// produces must not.
func stripOptionalMarkers(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		m := sectionHeadingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		heading := strings.TrimSpace(m[1])
		if strings.HasSuffix(heading, optionalMarker) {
			lines[i] = "## " + strings.TrimSpace(strings.TrimSuffix(heading, optionalMarker))
		}
	}
	return strings.Join(lines, "\n")
}

// renderTemplateBody substitutes {{name}} with the value of field name in
// fields, plus the always-available {{title}}. A placeholder with no value
// renders as empty, never as the literal "{{name}}".
func renderTemplateBody(body, title string, fields map[string]string) string {
	return placeholderRE.ReplaceAllStringFunc(body, func(m string) string {
		name := m[2 : len(m)-2]
		if name == "title" {
			return title
		}
		return fields[name]
	})
}

// validateTemplateRules is the check every template passes before it is
// used or written: `template new` and `template edit` refuse to write a
// template that fails it, and loadTemplate refuses to use one that reached
// disk broken anyway (a hand edit, most likely).
func validateTemplateRules(rules TemplateRules) error {
	var problems []string
	if rules.Enforce != "" && rules.Enforce != "reject" && rules.Enforce != "warn" {
		problems = append(problems, `enforce must be "reject" or "warn", not "`+rules.Enforce+`"`)
	}
	var emptyChoices []string
	for field, choices := range rules.Choices {
		if len(choices) == 0 {
			emptyChoices = append(emptyChoices, field)
		}
	}
	sort.Strings(emptyChoices)
	for _, field := range emptyChoices {
		problems = append(problems, "choices."+field+" is empty")
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// checkTemplateName is the one place every template lookup and write
// validates name before joining it into a filesystem path. A template name
// is a slug: no "/", "\", ".." or "." component can survive it, so neither
// `template rm ../../x` nor a --template naming another entry's address can
// ever resolve outside <root>/templates.
func checkTemplateName(name string) error {
	if !vpath.ValidSlug(name) {
		return ErrUsage("bad_template_name",
			`"`+name+`" is not a valid template name: use lower-case letters and digits joined by single hyphens`,
			"trellis knowledge template ls")
	}
	return nil
}

// loadTemplate reads and validates the template named name inside dir. A
// missing file is unknown_template; a file that exists but fails to parse
// or validate is bad_template, and its message always leads with the
// file's path — a broken template must never be silently treated as having
// no rules.
func loadTemplate(dir, name string) (Template, error) {
	if err := checkTemplateName(name); err != nil {
		return Template{}, err
	}
	path := filepath.Join(dir, name+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Template{}, ErrUsage("unknown_template", "no template "+name, "trellis knowledge template ls")
		}
		return Template{}, err
	}
	header, body, ok := splitHeader(string(raw))
	var rules TemplateRules
	if ok {
		if err := yaml.Unmarshal([]byte(header), &rules); err != nil {
			return Template{}, ErrUsage("bad_template",
				path+": the template's frontmatter does not parse: "+err.Error(),
				"trellis knowledge template edit "+name)
		}
	}
	if err := validateTemplateRules(rules); err != nil {
		return Template{}, ErrUsage("bad_template", path+": "+err.Error(), "trellis knowledge template edit "+name)
	}
	enforce := rules.Enforce
	if enforce == "" {
		enforce = "warn"
	}
	return Template{
		Name: name, Path: path, Enforce: enforce,
		Required: rules.Required, Choices: rules.Choices, Verify: rules.Verify, Body: body,
	}, nil
}

// templatesDir returns <root>/templates, seeding it from the built-in
// templates the first time it is needed. Templates are global: one
// directory serves every project, and seeding never touches a directory
// that already exists — a built-in the user deleted stays deleted.
func (c *Core) templatesDir() (string, error) {
	dir := filepath.Join(c.root, "templates")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := seedTemplates(dir); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return dir, nil
}

// seedTemplates writes the six built-ins into dir, which must not yet
// exist.
func seedTemplates(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range Templates() {
		raw, err := templateFS.ReadFile("templates/" + name + ".md")
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(dir, name+".md"), raw, false); err != nil {
			return err
		}
	}
	return nil
}

// reservedFrontmatterFields maps a Frontmatter struct field's YAML key to
// the flag that sets it. A --set value using one of these would collide
// with Extra's inline map, which yaml.v3 panics on rather than returning
// an error, so it is refused up front instead — and pointed at the flag
// that actually sets it, not just told no.
var reservedFrontmatterFields = map[string]string{
	"title": "--title", "template": "--template", "status": "(not yet settable)",
	"summary": "--summary", "provenance": "--provenance", "private": "--private",
	"board": "--board", "tags": "--tag", "labels": "--label",
	"artifacts": "`trellis artifact link`", "created": "(set automatically)",
	"updated": "(set automatically)", "sources": "--source",
}

// templateViolations checks supplied field values against a template's
// rules. fields maps a field name to its values — a slice so a required
// list field (sources, see TRELLIS-35) can mean "at least one", the same
// rule a required scalar means "non-blank". Required fields are checked in
// the order the template lists them; choices fields follow in sorted
// order, so the result is deterministic regardless of Go's randomised map
// iteration. checkSections is true only when the caller wrote the body
// themselves: a skeleton's sections are present by construction.
func templateViolations(t Template, fields map[string][]string, body string, checkSections bool) []string {
	var out []string
	for _, name := range t.Required {
		nonBlank := 0
		for _, v := range fields[name] {
			if strings.TrimSpace(v) != "" {
				nonBlank++
			}
		}
		if nonBlank == 0 {
			out = append(out, "missing required field "+name)
		}
	}
	choiceFields := make([]string, 0, len(t.Choices))
	for name := range t.Choices {
		choiceFields = append(choiceFields, name)
	}
	sort.Strings(choiceFields)
	for _, name := range choiceFields {
		for _, v := range fields[name] {
			if v == "" {
				continue
			}
			if !slices.Contains(t.Choices[name], v) {
				out = append(out, name+" must be one of "+strings.Join(t.Choices[name], ", ")+", not "+v)
			}
		}
	}
	if checkSections {
		present := presentSections(body)
		for _, heading := range requiredSections(t.Body) {
			if !present[heading] {
				out = append(out, "missing section "+heading)
			}
		}
	}
	return out
}

// templateNamed loads one template from the templates directory.
func (c *Core) templateNamed(name string) (Template, error) {
	dir, err := c.templatesDir()
	if err != nil {
		return Template{}, err
	}
	return loadTemplate(dir, name)
}

// templateProblems is every way a document falls short of its template: the
// field and section rules, then the verify rule, read in the caller's
// transaction.
func (c *Core) templateProblems(tx *sqlx.Tx, projectID string, t Template, fields map[string][]string,
	body string, checkSections bool) ([]string, error) {
	problems := templateViolations(t, fields, body, checkSections)
	unresolved, err := c.templateVerifyViolations(tx, projectID, t, fields, body)
	return append(problems, unresolved...), err
}

// enforceTemplate refuses a document under a reject template that has
// problems. Under warn the problems are the caller's warnings.
func enforceTemplate(t Template, problems []string) error {
	if len(problems) == 0 || t.Enforce != "reject" {
		return nil
	}
	return &Error{Code: "template_violation", Exit: 2,
		Msg: t.Name + " does not meet its template", Problems: problems,
		Fix: templateViolationFix(t.Name, problems)}
}

// frontmatterFields is a document's frontmatter as template fields: every
// value a required or choices rule can name. A list contributes each item.
func frontmatterFields(fm Frontmatter) map[string][]string {
	fields := map[string][]string{
		"title": {fm.Title}, "summary": {fm.Summary}, "status": {fm.Status},
		"provenance": {fm.Provenance}, "board": {fm.Board},
		"tags": fm.Tags, "labels": fm.Labels, "artifacts": fm.Artifacts, "sources": fm.Sources,
	}
	for k, v := range fm.Extra {
		switch v := v.(type) {
		case nil:
		case []any:
			for _, item := range v {
				fields[k] = append(fields[k], fmt.Sprint(item))
			}
		default:
			fields[k] = []string{fmt.Sprint(v)}
		}
	}
	return fields
}

// TemplateInfo is one template's summary for `template ls`.
type TemplateInfo struct {
	Name     string              `json:"name"`
	Enforce  string              `json:"enforce"`
	BuiltIn  bool                `json:"builtin"`
	Required []string            `json:"required"`
	Choices  map[string][]string `json:"choices"`
	// Sections are the "## " headings an entry must keep.
	Sections []string `json:"sections"`
}

// ListTemplates lists every template in <root>/templates, alphabetically.
func (c *Core) ListTemplates(ctx context.Context) ([]TemplateInfo, error) {
	dir, err := c.templatesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	builtins := Templates()
	out := make([]TemplateInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		t, err := loadTemplate(dir, name)
		if err != nil {
			return nil, err
		}
		out = append(out, TemplateInfo{
			Name: name, Enforce: t.Enforce, BuiltIn: slices.Contains(builtins, name),
			Required: t.Required, Choices: t.Choices, Sections: requiredSections(t.Body),
		})
	}
	slices.SortFunc(out, func(a, b TemplateInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// ShowTemplate returns one template's rules and skeleton.
func (c *Core) ShowTemplate(ctx context.Context, name string) (Template, error) {
	dir, err := c.templatesDir()
	if err != nil {
		return Template{}, err
	}
	t, err := loadTemplate(dir, name)
	if err != nil {
		return Template{}, err
	}
	t.BuiltIn = slices.Contains(Templates(), name)
	return t, nil
}

// NewTemplate writes a minimal template — enforce: warn, no rules, a
// "# {{title}}" heading — and refuses a name that already exists.
func (c *Core) NewTemplate(ctx context.Context, name string) (Template, error) {
	if err := checkTemplateName(name); err != nil {
		return Template{}, err
	}
	dir, err := c.templatesDir()
	if err != nil {
		return Template{}, err
	}
	path := filepath.Join(dir, name+".md")
	raw := "---\nenforce: warn\n---\n# {{title}}\n"
	if err := writeAtomic(path, []byte(raw), false); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Template{}, ErrConflict("template_exists", "a template named "+name+" already exists",
				"trellis knowledge template edit "+name)
		}
		return Template{}, err
	}
	return loadTemplate(dir, name)
}

// EditTemplate replaces name's whole file — frontmatter and body — after
// checking it: a template that fails to parse or validate is not written.
func (c *Core) EditTemplate(ctx context.Context, name, raw string) (Template, error) {
	if err := checkTemplateName(name); err != nil {
		return Template{}, err
	}
	dir, err := c.templatesDir()
	if err != nil {
		return Template{}, err
	}
	path := filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Template{}, ErrUsage("unknown_template", "no template "+name, "trellis knowledge template ls")
		}
		return Template{}, err
	}
	header, _, ok := splitHeader(raw)
	var rules TemplateRules
	if ok {
		if err := yaml.Unmarshal([]byte(header), &rules); err != nil {
			return Template{}, ErrUsage("bad_template",
				"the template's frontmatter does not parse: "+err.Error(), "")
		}
	}
	if err := validateTemplateRules(rules); err != nil {
		return Template{}, ErrUsage("bad_template", err.Error(), "")
	}
	if err := writeAtomic(path, []byte(raw), true); err != nil {
		return Template{}, err
	}
	return loadTemplate(dir, name)
}

// DeleteTemplate removes a template file. Templates have no database row —
// nothing else can point at one — so a plain remove is the whole operation.
func (c *Core) DeleteTemplate(ctx context.Context, name string) error {
	if err := checkTemplateName(name); err != nil {
		return err
	}
	dir, err := c.templatesDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".md")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrUsage("unknown_template", "no template "+name, "trellis knowledge template ls")
		}
		return err
	}
	return syncDirectory(dir)
}

// ReinstallTemplate overwrites name with its shipped version. It recreates
// a deleted built-in and discards any edits to an existing one. It is
// refused for a name that is not a built-in.
func (c *Core) ReinstallTemplate(ctx context.Context, name string) (Template, error) {
	if !slices.Contains(Templates(), name) {
		return Template{}, ErrUsage("not_builtin", name+" is not a built-in template",
			"trellis knowledge template ls   # built-ins: "+strings.Join(Templates(), ", "))
	}
	dir, err := c.templatesDir()
	if err != nil {
		return Template{}, err
	}
	raw, err := templateFS.ReadFile("templates/" + name + ".md")
	if err != nil {
		return Template{}, err
	}
	if err := writeAtomic(filepath.Join(dir, name+".md"), raw, true); err != nil {
		return Template{}, err
	}
	t, err := loadTemplate(dir, name)
	t.BuiltIn = true
	return t, err
}

// CheckTemplate reports name's violations against slug's current fields and
// sections, plus its verify rule — the same three lint and edit check. It
// never blocks and never errors because of a violation — the document
// already exists.
func (c *Core) CheckTemplate(ctx context.Context, projectID, name, slug string) ([]string, error) {
	dir, err := c.templatesDir()
	if err != nil {
		return nil, err
	}
	tmpl, err := loadTemplate(dir, name)
	if err != nil {
		return nil, err
	}
	doc, err := c.LoadKnowledge(ctx, projectID, slug)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		return nil, err
	}
	fm, body, err := splitDocFile(doc.Path, raw)
	if err != nil {
		return nil, err
	}
	var problems []string
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		var terr error
		problems, terr = c.templateProblems(tx, projectID, tmpl, frontmatterFields(fm), body, true)
		return terr
	})
	return problems, err
}

// templateViolationFix picks the fix line an agent most needs. When the
// violation involves sources, a runnable --source example beats a pointer
// to `template show`, because an agent hitting reject for the first time
// has no reason yet to know --source exists.
func templateViolationFix(tmplName string, violations []string) string {
	for _, v := range violations {
		if strings.Contains(v, "sources") {
			return `trellis knowledge new --template ` + tmplName +
				` --title "<title>" --source </KEY/cards/KEY-12|[[slug]]|url>` +
				"\n  trellis knowledge template show " + tmplName
		}
	}
	return "trellis knowledge template show " + tmplName
}
