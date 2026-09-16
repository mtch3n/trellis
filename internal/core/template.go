package core

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

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
}

// Template is a parsed template file: its rules, its skeleton body (with
// placeholders still in it), and where it lives on disk.
type Template struct {
	Name     string
	Path     string
	Enforce  string
	Required []string
	Choices  map[string][]string
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

// loadTemplate reads and validates the template named name inside dir. A
// missing file is unknown_template; a file that exists but fails to parse
// or validate is bad_template, and its message always leads with the
// file's path — a broken template must never be silently treated as having
// no rules.
func loadTemplate(dir, name string) (Template, error) {
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
		Required: rules.Required, Choices: rules.Choices, Body: body,
	}, nil
}

// templatesDir returns <root>/templates, seeding it from the built-in
// templates the first time it is needed. Templates are global: one
// directory serves every project, and seeding never touches a directory
// that already exists — a built-in the user deleted stays deleted.
func (c *Core) templatesDir() (string, error) {
	root, err := c.root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "templates")
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

// reservedFrontmatterFields are the Frontmatter struct's own YAML keys. A
// --set value using one of these would collide with Extra's inline map,
// which yaml.v3 panics on rather than returning an error, so it is refused
// up front instead.
var reservedFrontmatterFields = map[string]bool{
	"title": true, "type": true, "status": true, "summary": true,
	"provenance": true, "private": true, "board": true, "tags": true,
	"labels": true, "artifacts": true, "created": true, "updated": true,
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
