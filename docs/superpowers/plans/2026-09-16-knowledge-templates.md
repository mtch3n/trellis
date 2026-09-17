# Knowledge Templates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **WHERE TO WORK — read before anything else.** Every path in this plan is
> relative to the worktree **`/home/mtchen/Personal/trellis-worktrees/knowledge-artifacts`**
> on branch `feat/knowledge-artifacts`. Run every command from there, e.g.
> `cd /home/mtchen/Personal/trellis-worktrees/knowledge-artifacts && go test ./...`.
> **Never** run a command in, or edit a file under, `/home/mtchen/Personal/trellis`.
> That is a shared checkout where other sessions hold uncommitted work.

**Goal:** Move Trellis's six knowledge templates out of the binary and onto
disk, where they can be listed, shown, edited, replaced and enforced; let
`Frontmatter` keep any key a template asks for instead of dropping it; and
(TRELLIS-35) let the `decision` and `finding` templates require and verify
cited sources.

**Architecture:** A template is a markdown file with its own small YAML
frontmatter (`enforce`, `required`, `choices`, and after TRELLIS-35,
`verify`) plus a skeleton body. Templates live at `<root>/templates/`,
global to every project, seeded from the six built-ins only when that
directory does not yet exist. A template is read and checked exactly once,
at the moment `knowledge new` selects it (or when `template check` asks for
an existing document) — never on edit, never on a hand edit, never during
`lint`. `Frontmatter` gains an inline map so a key a template supplies
survives every later rewrite of the file. TRELLIS-35 adds a `sources:` list
field, a `verify` rule that resolves internal references (wikilinks and a
small subset of absolute Trellis addresses) while leaving URLs and prose
alone, and applies `required: [sources]` / `verify: [sources, body]` to the
`decision` and `finding` built-ins.

**Tech Stack:** Go 1.27, `gopkg.in/yaml.v3` (pinned in `go.mod`), SQLite via
`sqlx`, `encoding/json/v2`, cobra.

**Spec:** `docs/superpowers/specs/2026-09-16-knowledge-templates-design.md`
(base plan, Tasks 1–6) amended by TRELLIS-35 (Tasks 7–11 below implement the
card's decisions; there is no separate spec document for TRELLIS-35 — its
card text is the authority for those tasks).

## Global Constraints

- Templates live in `<root>/templates/`, global only. There are no
  per-project templates.
- Seeding writes the six built-ins into `<root>/templates/` only when that
  directory does not exist. Once it exists, Trellis never writes into it on
  its own — a built-in the user deleted stays deleted.
- The rule language is four keys (five after TRELLIS-35): `enforce`
  (`reject` or `warn`; absent means `warn`), `required` (field names that
  must be supplied and non-blank; a required *list* field, e.g. `sources`,
  means at least one non-blank item), `choices` (a closed list of values for
  a field; a field may be in `choices` without being `required`), and
  required sections read from the skeleton's `## ` headings. After
  TRELLIS-35: `verify` (field names, or the literal `body`, whose internal
  references must resolve).
- Required sections match **exactly two `#` followed by a space**. `### ` is
  a subsection and never counts — `decision.md` ships
  `### Option A — <name>` placeholders, and a prefix match would make every
  real decision fail its own template.
- A heading ending in `<!-- optional -->` is exempt from being required, and
  the marker is stripped from the heading when a skeleton renders into a
  document.
- `{{name}}` in a template body substitutes the value of field `name` (from
  `--set` or, for `sources`, joined from `--source`); `{{title}}` is always
  available; a placeholder with no value renders as empty, never as the
  literal `{{name}}`.
- A template is checked only when it is selected: `knowledge new --template`
  (fields always; sections too when `--body` was given, since a
  caller-written body's headings are what count) and
  `knowledge template check <name> <slug>` (fields and sections of an
  existing document, and it never blocks). Never on `knowledge edit`, a hand
  edit, or `lint`.
- Under `enforce: reject`, any violation writes nothing. Under `enforce:
  warn`, the document is written and every violation comes back as a
  warning.
- `template reinstall <name>` is refused for a name that is not one of the
  six built-ins (`core.Templates()`, unchanged: it still lists the shipped
  embedded names).
- A broken template — frontmatter that fails to parse, an `enforce` value
  that is neither `reject` nor `warn`, or a `choices` entry with no values —
  fails whatever selected it, and the error names the template's file path.
  It is never silently treated as having no rules.
- `Frontmatter.Extra map[string]any` (`yaml:",inline"`) keeps every key the
  struct does not name. Verified against the pinned `gopkg.in/yaml.v3
  v3.0.1` (see Task 1) that: an inline map must have string keys (any value
  type); unmarshal puts an unrecognised key's raw text into it; marshal
  writes it back sorted by key (`sorter.go`'s `keyList`, used via
  `sort.Sort`), so rendering is deterministic regardless of Go's randomised
  map iteration — the content hash and every diff depend on that. A value
  supplied through `--set` (or, after TRELLIS-35, `--source`) that names one
  of `Frontmatter`'s own YAML keys (`title`, `type`, `status`, `summary`,
  `provenance`, `private`, `board`, `tags`, `labels`, `artifacts`,
  `created`, `updated`, and after TRELLIS-35, `sources`) must be refused
  before `RenderDoc` ever runs: yaml.v3's encoder **panics**, it does not
  return an error, when an inline map's key collides with a named struct
  field's key.
- `knowledge lint` reports a frontmatter key that neither the `Frontmatter`
  struct nor any template's `required`/`choices` names, as a new
  `unknown_field` finding.
- No template versioning; no Doctor check comparing an installed template
  against the shipped one; no record on a document of which template
  created it; no checking of a document against a template after creation.
- The web UI (`internal/ui/server.go`'s `handleKnowledgeCreate`) and the TUI
  (`internal/cli/tui_workspace.go`'s `edit(create)`) both create knowledge
  entries by calling `core.CreateKnowledge` directly. Neither is modified by
  this plan. Both automatically gain template checking: the TUI always
  creates through the implicit `note` template (it sets no `Template` and no
  `Body` is ever empty from its form, so — see Task 4 — sections are
  checked against whatever body the user typed; `note` has no required
  sections, so this is a no-op today). The web UI already forwards
  `Template` from its request body; it does not send `Set` or `Source`, so
  a user who tightens a template to `required` fields will see the same
  `template_violation` a CLI caller would when creating from the web UI,
  with no code change needed to produce that behaviour. Neither surface
  gains a way to supply `--set`/`--source` values in this plan; that is a
  UI/TUI feature gap this plan deliberately leaves open, since the built-ins
  ship either with no required fields (four of them) or — after
  TRELLIS-35 — `sources`, which is exactly the case that would need it. This
  is called out again at Task 10.
- Cross-platform: Linux, macOS and Windows CI must all pass. Template files
  are plain UTF-8 markdown; nothing in this plan touches fsync or executes a
  script, so no OS-specific code path is introduced.
- Gates before any task is called done: `go build ./...`, `go test ./...`,
  `go vet ./...`, `gofmt -l .` (must print nothing).
  `GOOS=windows go build ./...` at the end of the whole plan (it is
  redundant to run after every task; run it once after Task 6 and once more
  after Task 11).
- **Known flake:** `TestListKnowledgeFiltersByTypeAndProvenance/both_dimensions`
  fails about one run in five (TRELLIS-26, pre-existing —
  `KnowledgeFilter.where()` ranges over a Go map). If that exact subtest
  fails, say so and move on. **Investigate any other failure; never
  attribute it to this flake.**
- **Coordination point:** a parallel plan adds revision capture inside
  `CreateKnowledge` and `EditKnowledgeFields`. This plan's edits to those two
  functions are additive and are called out precisely (exact insertion
  points) in Tasks 4, 7 and 9 — do not restructure either function beyond
  what a task states.
- **Coordination point (TRELLIS-35 tasks only):** `internal/vpath` (Task 8)
  is a deliberately small subset of
  `docs/superpowers/specs/2026-09-16-virtual-paths-design.md` — cards,
  knowledge and artifacts only, `Path{Project, Collection, Name}`, `Parse`,
  and three collection constants (`CollectionCards`, `CollectionKnowledge`,
  `CollectionArtifacts`), exactly as that spec names them. That spec's own
  layer — boards, anchors, `SplitAnchor`, the `KnowledgePath` /
  `GlobalKnowledgePath` / `CardPath` / `ArtifactPath` constructors,
  cross-project argument handling — is **not built here** and must not be
  anticipated. A later session extends this package; this plan does not
  replace it.
- Stage by explicit path only. Before every commit run
  `git diff --cached --name-only` and confirm each path is one this task
  changed.
- Every commit message ends with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/markdown.go` | Modify: `Frontmatter.Extra`, `Frontmatter.Sources`; `splitHeader` factored out of `SplitFrontmatter` |
| `internal/core/markdown_test.go` | Modify: `Extra` round trip and stable-order tests; `Sources` round trip |
| `internal/core/template.go` | Create: `TemplateRules`, `Template`, `TemplateSection`, section/placeholder helpers, `validateTemplateRules`, `loadTemplate`; grows in later tasks: seeding, `reservedFrontmatterFields`, `templateViolations`, template CRUD, `TemplateInfo`, `templateViolationFix` |
| `internal/core/template_test.go` | Create: sections, rendering, validation, `loadTemplate`, seeding |
| `internal/core/templates/*.md` | Modify: all six gain frontmatter and lower-case `{{title}}`; `decision.md`/`finding.md` later become `enforce: reject` |
| `internal/core/knowledge.go` | Modify: `NewKnowledge.Set`/`.Sources`, `Knowledge.Warnings`/`.Sources`, `CreateKnowledge`, `refreshFromFile`, `KnowledgeEdit.Sources`, `EditKnowledgeFields`, `cleanSources` |
| `internal/core/knowledge_test.go` | Modify: two existing tests updated for the now-stricter `decision`/`finding` templates; new tests throughout |
| `internal/core/lint.go` | Modify: `unknown_field` finding, `knownExtraFields` |
| `internal/core/template_lint_test.go` | Create: unknown-field lint tests |
| `internal/vpath/vpath.go` | Create: `Parse`, `Path`, collection constants |
| `internal/vpath/vpath_test.go` | Create: grammar tests |
| `internal/core/template_verify.go` | Create: `resolvesInternalReference`, `verifyFieldValues`, `verifyBody`, `templateVerifyViolations`, `wikilinkTarget`, `bodyAbsoluteAddresses` |
| `internal/core/template_verify_test.go` | Create: generic verify-mechanism tests (ad hoc templates) |
| `internal/core/template_citations_test.go` | Create: `decision`/`finding` end-to-end tests |
| `internal/cli/knowledge.go` | Modify: `--set`/`--source` on `new`, warnings printed, `edit` gains `--source`, `newKnowledgeTemplateCmd` registered |
| `internal/cli/knowledge_template.go` | Create: `template ls/show/new/edit/rm/reinstall/check`, `parseSetFlags`, `withGlobalCore` |
| `internal/cli/knowledge_template_test.go` | Create: CLI-level template, `--set` and `--source` tests |

---

### Task 1: `Frontmatter` keeps every key it does not recognise

**Files:**
- Modify: `internal/core/markdown.go`
- Modify: `internal/core/markdown_test.go`

**Interfaces:**
- Consumes: existing `Frontmatter`, `RenderDoc`, `SplitFrontmatter`.
- Produces:
  - `Frontmatter.Extra map[string]any` (`yaml:",inline"`)
  - `func splitHeader(raw string) (header, body string, ok bool)` — the
    delimiter logic `SplitFrontmatter` and the Task 2 template parser both
    build on.

- [ ] **Step 1: Write the failing tests**

Add `"strings"` to the imports of `internal/core/markdown_test.go` (it
currently imports only `"reflect"` and `"testing"`), then append:

```go
func TestFrontmatterExtraKeysRoundTrip(t *testing.T) {
	raw := "---\ntitle: Rollback the API\ntype: runbook\nowner: alice\nseverity: high\n---\n# Rollback the API\n"
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Extra["owner"] != "alice" || fm.Extra["severity"] != "high" {
		t.Fatalf("Extra = %+v, want owner and severity kept", fm.Extra)
	}
	out := RenderDoc(fm, body)
	back, _, err := SplitFrontmatter(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Extra["owner"] != "alice" || back.Extra["severity"] != "high" {
		t.Errorf("round trip lost an unknown key: Extra = %+v", back.Extra)
	}
	if back.Title != "Rollback the API" || back.Type != "runbook" {
		t.Errorf("a named field was lost: %+v", back)
	}
}

func TestFrontmatterExtraKeysRenderInStableOrder(t *testing.T) {
	a := Frontmatter{Title: "X", Extra: map[string]any{"zebra": "z", "apple": "a", "mango": "m"}}
	b := Frontmatter{Title: "X", Extra: map[string]any{"mango": "m", "apple": "a", "zebra": "z"}}
	rendered := RenderDoc(a, "body\n")
	for range 20 {
		if got := RenderDoc(b, "body\n"); got != rendered {
			t.Fatalf("rendering is not deterministic:\n%q\n%q", rendered, got)
		}
	}
	if !strings.Contains(rendered, "apple: a\nmango: m\nzebra: z\n") {
		t.Errorf("Extra keys did not render in sorted order:\n%s", rendered)
	}
}

func TestFrontmatterWithNoExtraKeysIsUnchanged(t *testing.T) {
	fm := Frontmatter{Title: "Plain", Type: "note"}
	got := RenderDoc(fm, "body\n")
	want := "---\ntitle: Plain\ntype: note\n---\n\nbody\n"
	if got != want {
		t.Errorf("RenderDoc with no Extra = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run TestFrontmatterExtra -v`
Expected: the package does not compile — `fm.Extra undefined`.

- [ ] **Step 3: Add the field**

In `internal/core/markdown.go`, inside `type Frontmatter struct`, as the
last field (after `Updated`):

```go
	// Extra keeps every frontmatter key this struct does not name. A
	// template may ask for a field ("owner", "severity") that has no
	// dedicated column here; without this, yaml.Unmarshal would silently
	// drop it, and the first `knowledge edit` — which re-renders the
	// frontmatter from this struct — would erase it from the file.
	Extra map[string]any `yaml:",inline"`
```

- [ ] **Step 4: Factor `splitHeader` out of `SplitFrontmatter`**

Replace the current `SplitFrontmatter`:

```go
func SplitFrontmatter(raw string) (Frontmatter, string, error) {
	var fm Frontmatter
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return fm, s, nil
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return fm, s, nil
	}
	header := s[4 : 4+end]
	body := strings.TrimPrefix(s[4+end+4:], "\n")
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return fm, body, ErrUsage("bad_frontmatter", "the YAML frontmatter does not parse: "+err.Error(), "")
	}
	return fm, body, nil
}
```

with:

```go
// splitHeader separates a "---\n...\n---\n" YAML header from the body of any
// file using that convention, without assuming what the header unmarshals
// into. SplitFrontmatter and the template parser in template.go both build
// on this, so the delimiter rule exists in exactly one place.
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

func SplitFrontmatter(raw string) (Frontmatter, string, error) {
	var fm Frontmatter
	header, body, ok := splitHeader(raw)
	if !ok {
		return fm, body, nil
	}
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return fm, body, ErrUsage("bad_frontmatter", "the YAML frontmatter does not parse: "+err.Error(), "")
	}
	return fm, body, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestFrontmatter|TestSplitFrontmatter' -v`
Expected: PASS, including the pre-existing `TestFrontmatterRoundTrip` and
`TestSplitFrontmatterToleratesNone`.

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass; `gofmt -l .` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/core/markdown.go internal/core/markdown_test.go
git commit -m "$(cat <<'EOF'
feat(core): Frontmatter keeps every key it does not recognise

Extra is an inline YAML map, so a key a template supplies survives every
later rewrite of the file instead of being silently dropped by
yaml.Unmarshal. Rendering stays deterministic: yaml.v3 writes an inline
map's keys sorted, regardless of Go's randomised map order, which matters
because the content hash and every diff depend on it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: The template file model — rules, sections, rendering

**Files:**
- Create: `internal/core/template.go`
- Create: `internal/core/template_test.go`

**Interfaces:**
- Consumes: `fenceRE` (`internal/core/markdown.go`), `splitHeader` (Task 1),
  `ErrUsage` (`internal/core/errors.go`).
- Produces:
  - `type TemplateRules struct { Enforce string; Required []string; Choices map[string][]string }`
  - `type Template struct { Name, Path, Enforce string; Required []string; Choices map[string][]string; Body string; BuiltIn bool }`
  - `type TemplateSection struct { Heading string; Optional bool }`
  - `func templateSections(body string) []TemplateSection`
  - `func requiredSections(body string) []string`
  - `func presentSections(body string) map[string]bool`
  - `func stripOptionalMarkers(body string) string`
  - `func renderTemplateBody(body, title string, fields map[string]string) string`
  - `func validateTemplateRules(rules TemplateRules) error`
  - `func loadTemplate(dir, name string) (Template, error)` — errors:
    `unknown_template` (missing file) or `bad_template` (parse/validation
    failure, message always leads with the file's path)

- [ ] **Step 1: Write the failing tests**

Create `internal/core/template_test.go`:

```go
package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredSectionsMatchLevelTwoExactly(t *testing.T) {
	body := "# {{title}}\n\n## Context\n\n## Options considered\n\n" +
		"### Option A — <name>\n\n### Option B — <name>\n\n## Decision\n\n## Consequences\n"
	got := requiredSections(body)
	want := []string{"Context", "Options considered", "Decision", "Consequences"}
	if len(got) != len(want) {
		t.Fatalf("requiredSections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("requiredSections[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOptionalSectionIsNotRequiredAndMarkerIsStripped(t *testing.T) {
	body := "# {{title}}\n\n## Steps\n\n## Rollback <!-- optional -->\n"
	if got := requiredSections(body); len(got) != 1 || got[0] != "Steps" {
		t.Fatalf("requiredSections = %v, want just [Steps]", got)
	}
	rendered := stripOptionalMarkers(body)
	if rendered != "# {{title}}\n\n## Steps\n\n## Rollback\n" {
		t.Errorf("stripOptionalMarkers = %q", rendered)
	}
}

func TestPresentSectionsMatchesADocumentBody(t *testing.T) {
	body := "# Title\n\n## Preconditions\n\nSome text.\n\n## Steps\n\n1. Do it.\n"
	set := presentSections(body)
	if !set["Preconditions"] || !set["Steps"] {
		t.Errorf("presentSections = %v, want Preconditions and Steps", set)
	}
	if set["Verification"] {
		t.Error("Verification was never written and must not be present")
	}
}

func TestRenderTemplateBodySubstitutesAndDefaultsToEmpty(t *testing.T) {
	body := "# {{title}}\n\nOwner: {{owner}}. Extra: {{missing}}.\n"
	got := renderTemplateBody(body, "Rollback the API", map[string]string{"owner": "alice"})
	want := "# Rollback the API\n\nOwner: alice. Extra: .\n"
	if got != want {
		t.Errorf("renderTemplateBody = %q, want %q", got, want)
	}
}

func TestValidateTemplateRulesRejectsBadEnforceAndEmptyChoices(t *testing.T) {
	if err := validateTemplateRules(TemplateRules{Enforce: "warn"}); err != nil {
		t.Errorf("warn should be valid: %v", err)
	}
	if err := validateTemplateRules(TemplateRules{}); err != nil {
		t.Errorf("empty enforce (defaults to warn) should be valid: %v", err)
	}
	if err := validateTemplateRules(TemplateRules{Enforce: "sometimes"}); err == nil {
		t.Error("want an error for an unknown enforce value")
	}
	if err := validateTemplateRules(TemplateRules{Choices: map[string][]string{"severity": {}}}); err == nil {
		t.Error("want an error for an empty choices list")
	}
}

func TestLoadTemplateMissingIsUnknownTemplate(t *testing.T) {
	_, err := loadTemplate(t.TempDir(), "nope")
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "unknown_template" {
		t.Fatalf("err = %v, want unknown_template", err)
	}
}

func TestLoadTemplateNamesItsFileWhenBroken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte("---\nenforce: sometimes\n---\n# {{title}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadTemplate(dir, "bad")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want it to name %s", err, path)
	}
}

func TestLoadTemplateReadsRulesAndBody(t *testing.T) {
	dir := t.TempDir()
	raw := "---\nenforce: reject\nrequired: [owner]\nchoices:\n  severity: [low, high]\n---\n# {{title}}\n\n## Steps\n"
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpl, err := loadTemplate(dir, "runbook")
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if tmpl.Enforce != "reject" || len(tmpl.Required) != 1 || tmpl.Required[0] != "owner" {
		t.Errorf("tmpl = %+v", tmpl)
	}
	if len(tmpl.Choices["severity"]) != 2 {
		t.Errorf("choices = %+v", tmpl.Choices)
	}
	if !strings.Contains(tmpl.Body, "## Steps") {
		t.Errorf("Body = %q", tmpl.Body)
	}
}

func TestLoadTemplateWithNoFrontmatterDefaultsToWarnAndNoRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain.md"), []byte("# {{title}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpl, err := loadTemplate(dir, "plain")
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if tmpl.Enforce != "warn" || len(tmpl.Required) != 0 || len(tmpl.Choices) != 0 {
		t.Errorf("tmpl = %+v, want warn with no rules", tmpl)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestRequiredSections|TestOptionalSection|TestPresentSections|TestRenderTemplateBody|TestValidateTemplateRules|TestLoadTemplate' -v`
Expected: the package does not compile — `undefined: requiredSections` (and
the rest of the new names).

- [ ] **Step 3: Create `internal/core/template.go`**

```go
package core

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestRequiredSections|TestOptionalSection|TestPresentSections|TestRenderTemplateBody|TestValidateTemplateRules|TestLoadTemplate' -v`
Expected: PASS, all eight.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/template.go internal/core/template_test.go
git commit -m "$(cat <<'EOF'
feat(core): the template file model — rules, sections, rendering

A template is a markdown file with its own small frontmatter (enforce,
required, choices) and a skeleton body. Required sections are read from
"## " headings, matched exactly two hashes and a space so a "###"
subsection placeholder is never mistaken for one. A heading marked
<!-- optional --> is exempt, and the marker is stripped when a skeleton
renders. A template that fails to parse or validate names its own file
path in the error.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: The six built-ins move to disk, seeded once

**Files:**
- Modify: `internal/core/templates/decision.md`
- Modify: `internal/core/templates/finding.md`
- Modify: `internal/core/templates/note.md`
- Modify: `internal/core/templates/reference.md`
- Modify: `internal/core/templates/research.md`
- Modify: `internal/core/templates/runbook.md`
- Modify: `internal/core/template.go`
- Modify: `internal/core/template_test.go`

**Interfaces:**
- Consumes: `Templates()`, `templateFS` (`internal/core/knowledge.go`),
  `writeAtomic` (`internal/core/file_store.go`), `loadTemplate` (Task 2).
- Produces: `func (c *Core) templatesDir() (string, error)`; `func seedTemplates(dir string) error`.

- [ ] **Step 1: Rewrite the six built-in template files**

`internal/core/templates/decision.md`:

```markdown
---
enforce: warn
---
# {{title}}

## Context

<!-- What is the situation? Why does this need a decision? -->

## Options considered

### Option A — <name>

- **What**:
- **Pros**:
- **Cons**:

### Option B — <name>

- **What**:
- **Pros**:
- **Cons**:

## Decision

<!-- What was chosen, in one sentence. -->

## Consequences

<!-- What this makes easy, and what it makes hard. -->
```

`internal/core/templates/finding.md`:

```markdown
---
enforce: warn
---
# {{title}}

## Symptom

<!-- What was observed. Lead with the symptom. -->

## Cause

<!-- The mechanism, not the guess. How it was confirmed. -->

## Fix

<!-- The change that resolved it, as a command or a diff. -->

## How to recognise it again

<!-- The cheapest signal that says "this again". -->
```

`internal/core/templates/note.md`:

```markdown
---
enforce: warn
---
# {{title}}

<!-- What is true, and how you know. -->
```

`internal/core/templates/reference.md`:

```markdown
---
enforce: warn
---
# {{title}}

<!-- Facts that are looked up, not reasoned about: endpoints, paths, IDs,
     conventions. Keep it scannable. -->

| Thing | Value |
|---|---|
|  |  |
```

`internal/core/templates/research.md`:

```markdown
---
enforce: warn
---
# {{title}}

## Question

<!-- What was being asked, precisely enough to be answerable. -->

## Method

<!-- What was run or read. Enough for someone to repeat it. -->

## Findings

<!-- Measured results. Numbers where there are numbers. -->

## Conclusion

<!-- The answer, and what it does not cover. -->
```

`internal/core/templates/runbook.md`:

```markdown
---
enforce: warn
---
# {{title}}

## When to use this

## Preconditions

<!-- Access, credentials, state that must already be true. -->

## Steps

1.

## Verification

<!-- How to know it worked. -->

## Rollback

<!-- How to undo it, or why it cannot be undone. -->
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/core/template_test.go`:

```go
func TestSeedingWritesBuiltinsOnceAndNeverAgain(t *testing.T) {
	c := testCore(t)
	c.WithKBRoot(t.TempDir())

	dir, err := c.templatesDir()
	if err != nil {
		t.Fatalf("templatesDir: %v", err)
	}
	for _, name := range Templates() {
		if _, err := os.Stat(filepath.Join(dir, name+".md")); err != nil {
			t.Errorf("%s was not seeded: %v", name, err)
		}
	}

	// Deleting a built-in and asking for the directory again must not
	// bring it back: seeding runs only when the directory itself is
	// absent.
	if err := os.Remove(filepath.Join(dir, "note.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.templatesDir(); err != nil {
		t.Fatalf("templatesDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.md")); !os.IsNotExist(err) {
		t.Error("note.md was reseeded after being deleted")
	}
}

func TestSeededTemplatesAllParse(t *testing.T) {
	c := testCore(t)
	c.WithKBRoot(t.TempDir())
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range Templates() {
		tmpl, err := loadTemplate(dir, name)
		if err != nil {
			t.Fatalf("loadTemplate(%s): %v", name, err)
		}
		if tmpl.Enforce != "warn" {
			t.Errorf("%s: enforce = %q, want warn", name, tmpl.Enforce)
		}
		if len(tmpl.Required) != 0 || len(tmpl.Choices) != 0 {
			t.Errorf("%s: has rules, want none", name)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestSeeding|TestSeededTemplatesAllParse' -v`
Expected: the package does not compile — `c.templatesDir undefined`.

- [ ] **Step 4: Add seeding to `internal/core/template.go`**

Append to the end of the file:

```go
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestSeeding|TestSeededTemplatesAllParse|TestCreateKnowledgeWritesFileAndRow' -v`
Expected: PASS. `TestCreateKnowledgeWritesFileAndRow` still passes unchanged:
`decision.md`'s `## Options considered` heading survives the rewrite, and
`{{TITLE}}` is gone (this task does not yet wire template rendering into
`CreateKnowledge` — that is Task 4 — so `CreateKnowledge` is still calling
the old `templateBody`, which still works against the embedded
`templateFS` exactly as before; only its *content* changed, and
`{{TITLE}}` no longer appears in any built-in, so no doc created today
still has the literal string `{{TITLE}}` in it. This is expected and is
fixed properly in Task 4).

- [ ] **Step 6: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/core/templates/decision.md internal/core/templates/finding.md \
        internal/core/templates/note.md internal/core/templates/reference.md \
        internal/core/templates/research.md internal/core/templates/runbook.md \
        internal/core/template.go internal/core/template_test.go
git commit -m "$(cat <<'EOF'
feat(core): seed the six built-in templates onto disk

Each shipped template gains a frontmatter header (enforce: warn, no rules)
and switches to lower-case {{title}}. templatesDir creates
<root>/templates/ and copies the six built-ins into it the first time it
is asked for, and never again once the directory exists — a built-in the
user deletes stays deleted.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: `CreateKnowledge` checks a template's rules

**Files:**
- Modify: `internal/core/template.go`
- Modify: `internal/core/knowledge.go`
- Modify: `internal/core/knowledge_test.go`

**Interfaces:**
- Consumes: `loadTemplate`, `Template`, `renderTemplateBody`,
  `stripOptionalMarkers`, `requiredSections`, `presentSections` (Task 2);
  `c.templatesDir()` (Task 3); existing `CreateKnowledge`, `Knowledge`,
  `NewKnowledge`, `cmpOr`.
- Produces:
  - `NewKnowledge.Set map[string]string`
  - `Knowledge.Warnings []string` (`db:"-" json:"warnings,omitempty"`) — set
    only by `CreateKnowledge`, only when `enforce: warn` found a problem
  - `var reservedFrontmatterFields map[string]bool`
  - `func templateViolations(t Template, fields map[string][]string, body string, checkSections bool) []string`
    — **the `map[string][]string` shape is deliberate**: Task 9 adds a
    `sources` key whose value is a list; every later task that builds
    `fields` uses this same shape, never `map[string]string`.
  - error code `template_violation` (from `CreateKnowledge`, `ErrUsage`,
    exit 2)
  - error code `reserved_field` (from `CreateKnowledge`, `ErrUsage`, exit 2)

- [ ] **Step 1: Write the failing tests**

Add `"errors"` and `"path/filepath"` to the imports of
`internal/core/knowledge_test.go` (it currently imports `"os"`, `"strings"`,
`"testing"`, `"time"`), then append:

```go
func TestCreateKnowledgeSetWritesExtraFields(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Rollback the API", Template: "runbook", Set: map[string]string{"owner": "alice"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Extra["owner"] != "alice" {
		t.Errorf("Extra = %+v, want owner: alice", fm.Extra)
	}
}

func TestCreateKnowledgeSetRefusesANamedField(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "X", Set: map[string]string{"title": "Y"},
	})
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "reserved_field" {
		t.Fatalf("err = %v, want reserved_field", err)
	}
}

func TestCreateKnowledgeRejectsAMissingRequiredField(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: reject\nrequired: [owner]\n---\n# {{title}}\n\nOwner: {{owner}}\n"
	if err := os.WriteFile(filepath.Join(dir, "strict.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Template: "strict"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "owner") {
		t.Fatalf("err = %v, want template_violation naming owner", err)
	}
	docs, err := c.ListKnowledge(t.Context(), p.ID, KnowledgeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Error("a rejected template must write nothing")
	}
}

func TestCreateKnowledgeWarnsInsteadOfRejecting(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: warn\nrequired: [owner]\n---\n# {{title}}\n"
	if err := os.WriteFile(filepath.Join(dir, "lenient.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Template: "lenient"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "owner") {
		t.Errorf("Warnings = %v, want one naming owner", doc.Warnings)
	}
}

func TestCreateKnowledgeChecksSectionsOnlyWhenBodyIsSupplied(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: reject\n---\n# {{title}}\n\n## Steps\n"
	if err := os.WriteFile(filepath.Join(dir, "sectioned.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "From skeleton", Template: "sectioned",
	}); err != nil {
		t.Fatalf("skeleton body: %v", err)
	}

	_, err = c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Missing a section", Template: "sectioned", Body: "# Missing a section\n\nNo headings here.\n",
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "Steps") {
		t.Fatalf("err = %v, want template_violation naming Steps", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestCreateKnowledgeSet|TestCreateKnowledgeRejects|TestCreateKnowledgeWarns|TestCreateKnowledgeChecksSections' -v`
Expected: the package does not compile — `NewKnowledge.Set undefined`.

- [ ] **Step 3: Add `templateViolations` and `reservedFrontmatterFields`**

Append to `internal/core/template.go` (add `"slices"` to its imports):

```go
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
```

- [ ] **Step 4: Add `Set` and `Warnings`**

In `internal/core/knowledge.go`, inside `type NewKnowledge struct`, after
`Labels []string`:

```go
	// Set supplies values for fields a template asks for (required or
	// choices), and any other field the caller wants recorded. Every entry
	// is written into the new document's frontmatter.
	Set map[string]string
```

Inside `type Knowledge struct`, in the `// Computed for display.` block,
after `Artifacts []ArtifactRef`:

```go
	// Warnings is set only by CreateKnowledge, when creating from a
	// template under enforce: warn found a problem: a missing required
	// field, a value outside its choices, or a missing section. It is
	// never persisted or reloaded — the render-once model checks a
	// document against its template once, at creation.
	Warnings []string `db:"-" json:"warnings,omitempty"`
```

- [ ] **Step 5: Wire template checking into `CreateKnowledge`**

Replace the whole function (current form: title check, provenance check,
`body := in.Body; if body == "" { templateBody(...) }`, `checkWrite`, then
`c.Tx(...)`) with:

```go
// CreateKnowledge writes the file first and the row second: the file is the
// record, and a row pointing at a file that was never written would be a lie.
func (c *Core) CreateKnowledge(ctx context.Context, projectID string, in NewKnowledge) (Knowledge, error) {
	if strings.TrimSpace(in.Title) == "" {
		return Knowledge{}, ErrUsage("missing_title", "a knowledge entry needs a title",
			`trellis knowledge new --title "Concurrency model"`)
	}
	provenance, err := checkProvenance(in.Provenance)
	if err != nil {
		return Knowledge{}, err
	}
	for name := range in.Set {
		if reservedFrontmatterFields[name] {
			return Knowledge{}, ErrUsage("reserved_field",
				`"`+name+`" is a built-in frontmatter field and cannot be set with --set`,
				"trellis knowledge new --title ...   # use the matching flag instead")
		}
	}
	templatesDirPath, err := c.templatesDir()
	if err != nil {
		return Knowledge{}, err
	}
	tmpl, err := loadTemplate(templatesDirPath, cmpOr(in.Template, "note"))
	if err != nil {
		return Knowledge{}, err
	}
	fields := map[string][]string{}
	for k, v := range in.Set {
		fields[k] = []string{v}
	}
	body := in.Body
	checkSections := body != ""
	if body == "" {
		body = stripOptionalMarkers(renderTemplateBody(tmpl.Body, in.Title, in.Set))
	}
	violations := templateViolations(tmpl, fields, body, checkSections)
	if len(violations) > 0 && tmpl.Enforce == "reject" {
		return Knowledge{}, ErrUsage("template_violation",
			tmpl.Name+" does not meet its template:\n  - "+strings.Join(violations, "\n  - "),
			"trellis knowledge template show "+tmpl.Name)
	}
	if err := c.checkWrite(ctx, ProposedWrite{
		Op: "doc.write", EntityType: "knowledge", ProjectID: projectID,
		Fields: map[string]string{"title": in.Title, "body": body},
	}); err != nil {
		return Knowledge{}, err
	}

	var doc Knowledge
	var writtenPath string
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		dir, err := c.kbDir(key, false)
		if err != nil {
			return err
		}
		slug, err := uniqueSlug(tx, projectID, Slugify(in.Title))
		if err != nil {
			return err
		}

		var boardID *string
		boardName := ""
		if in.Board != "" {
			b, err := c.boardByName(tx, projectID, in.Board)
			if err != nil {
				return err
			}
			boardID, boardName = &b.ID, b.Name
		}

		now := c.clock.NowMS()
		fm := Frontmatter{
			Title: in.Title, Type: cmpOr(in.Template, "note"), Summary: in.Summary,
			Provenance: provenance,
			Private:    in.Private,
			Board:      boardName, Tags: in.Tags, Labels: in.Labels,
			Created: msToRFC3339(now), Updated: msToRFC3339(now),
		}
		if len(in.Set) > 0 {
			fm.Extra = make(map[string]any, len(in.Set))
			for k, v := range in.Set {
				fm.Extra[k] = v
			}
		}
		raw := RenderDoc(fm, body)
		path := filepath.Join(dir, slug+".md")
		if err := writeAtomic(path, []byte(raw), false); err != nil {
			return err
		}
		writtenPath = path
		st, err := os.Stat(path)
		if err != nil {
			return err
		}

		doc = Knowledge{
			ID: NewCardID(), ProjectID: projectID, BoardID: boardID, Slug: slug,
			Title: in.Title, Path: path, DocType: fm.Type, Summary: in.Summary,
			Provenance: provenance,
			Private:    in.Private,
			BodyMD:     body, ContentHash: ContentHash(raw), MTime: st.ModTime().UnixMilli(),
			Size: st.Size(), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertKnowledge(tx, doc); err != nil {
			return err
		}
		if err := c.syncDocRelations(tx, &doc, fm, body); err != nil {
			return err
		}
		if err := c.resolveDocStubs(tx, &doc); err != nil {
			return err
		}
		if err := c.rebuildKnowledgeFTS(tx); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "knowledge", doc.ID, "created", "", "", doc.Title); err != nil {
			return err
		}
		return c.docView(tx, &doc)
	})
	if err != nil && writtenPath != "" {
		// A failed transaction must not leave a database-less knowledge file.
		// Keep a committed file intact if SQLite reports an ambiguous commit by
		// only removing the path when it still has the exact bytes we wrote.
		if raw, readErr := os.ReadFile(writtenPath); readErr == nil && ContentHash(string(raw)) == doc.ContentHash {
			_ = os.Remove(writtenPath)
			_ = syncDirectory(filepath.Dir(writtenPath))
		}
	}
	if err == nil {
		c.notifyKnowledgeChanged(ctx, projectID)
		if len(violations) > 0 {
			doc.Warnings = violations
		}
	}
	return doc, err
}
```

This changes one existing behaviour on purpose: previously, supplying
`--body` skipped `templateBody` entirely, so an unknown `--template` name
combined with `--body` silently succeeded (the name was used only as a free
`type` label). Now `loadTemplate` always runs, so an unknown template name
is rejected even when `--body` is given — this closes a gap rather than
opening one, and no existing test relies on the old, silent behaviour (only
`internal/core/knowledge_test.go`'s two other `Template:` usages pass real
built-in names with no `Body`, and are unaffected).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestCreateKnowledgeSet|TestCreateKnowledgeRejects|TestCreateKnowledgeWarns|TestCreateKnowledgeChecksSections|TestCreateKnowledgeWritesFileAndRow' -v`
Expected: PASS, all five.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/core/template.go internal/core/knowledge.go internal/core/knowledge_test.go
git commit -m "$(cat <<'EOF'
feat(core): CreateKnowledge checks a template's rules

A template's required fields and choices are checked whenever new selects
one; a caller-written body's sections are checked too, since a skeleton's
own sections are present by construction. reject writes nothing and lists
every violation; warn writes the document and returns the same list on
Knowledge.Warnings. --set values that would collide with a named
Frontmatter field are refused before they could ever reach RenderDoc,
which panics rather than erroring on that collision.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: CLI — `knowledge new --set` and `knowledge template ...`

**Files:**
- Modify: `internal/core/template.go`
- Modify: `internal/cli/knowledge.go`
- Create: `internal/cli/knowledge_template.go`
- Create: `internal/cli/knowledge_template_test.go`

**Interfaces:**
- Consumes: `loadTemplate`, `Template`, `validateTemplateRules`,
  `Templates()`, `c.templatesDir()` (Tasks 2–3); `writeAtomic`,
  `syncDirectory` (`internal/core/file_store.go`); `openCore`, `withBoard`,
  `Emit`, `TextValue` (`internal/cli`).
- Produces:
  - `type TemplateInfo struct { Name, Enforce string; BuiltIn bool }`
  - `func (c *Core) ListTemplates(ctx context.Context) ([]TemplateInfo, error)`
  - `func (c *Core) ShowTemplate(ctx context.Context, name string) (Template, error)`
  - `func (c *Core) NewTemplate(ctx context.Context, name string) (Template, error)` —
    error `template_exists` (`ErrConflict`) for a name already present
  - `func (c *Core) EditTemplate(ctx context.Context, name, raw string) (Template, error)`
  - `func (c *Core) DeleteTemplate(ctx context.Context, name string) error`
  - `func (c *Core) ReinstallTemplate(ctx context.Context, name string) (Template, error)` —
    error `not_builtin` (`ErrUsage`) for a name that is not one of the six
  - `func (c *Core) CheckTemplate(ctx context.Context, projectID, name, slug string) ([]string, error)`
  - CLI: `trellis knowledge template ls|show|new|edit|rm|reinstall|check`,
    `trellis knowledge new --set name=value` (repeatable)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/knowledge_template_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestTemplateLsListsTheSixBuiltins(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "template", "ls", "--json")
	for _, name := range []string{"decision", "finding", "note", "reference", "research", "runbook"} {
		if !strings.Contains(out, `"`+name+`"`) {
			t.Errorf("ls does not list %s:\n%s", name, out)
		}
	}
}

func TestTemplateShowReturnsRulesAndSkeleton(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "template", "show", "runbook")
	if !strings.Contains(out, "## Preconditions") {
		t.Errorf("show did not print the skeleton:\n%s", out)
	}
}

func TestTemplateNewRefusesAnExistingName(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "custom")
	if _, err := runCmdErr(t, "knowledge", "template", "new", "custom"); cliErrCode(err) != "template_exists" {
		t.Errorf("second new: err = %v, want template_exists", err)
	}
}

func TestTemplateEditThenNewEntryUsesIt(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "checklist")
	runCmd(t, "knowledge", "template", "edit", "checklist", "--body",
		"---\nenforce: warn\n---\n# {{title}}\n\n## Done\n")

	out := runCmd(t, "knowledge", "new", "--title", "Ship it", "--template", "checklist", "--json")
	if !strings.Contains(out, "checklist") {
		t.Errorf("new entry did not use the edited template's type:\n%s", out)
	}
}

func TestTemplateRmThenReinstallRestoresABuiltin(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "rm", "note")
	if _, err := runCmdErr(t, "knowledge", "template", "show", "note"); cliErrCode(err) != "unknown_template" {
		t.Fatalf("after rm: err = %v, want unknown_template", err)
	}
	runCmd(t, "knowledge", "template", "reinstall", "note")
	runCmd(t, "knowledge", "template", "show", "note")
}

func TestTemplateReinstallRefusesANonBuiltin(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "custom")
	if _, err := runCmdErr(t, "knowledge", "template", "reinstall", "custom"); cliErrCode(err) != "not_builtin" {
		t.Errorf("err = %v, want not_builtin", err)
	}
}

func TestTemplateCheckReportsWithoutBlocking(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "strict")
	runCmd(t, "knowledge", "template", "edit", "strict", "--body",
		"---\nenforce: reject\nrequired: [owner]\n---\n# {{title}}\n")
	runCmd(t, "knowledge", "new", "--title", "Loose", "--template", "note")

	out := runCmd(t, "knowledge", "template", "check", "strict", "loose", "--json")
	if !strings.Contains(out, "owner") {
		t.Errorf("check did not report the missing field:\n%s", out)
	}
	// check never blocks: the entry it inspected is untouched.
	runCmd(t, "knowledge", "show", "loose")
}

func TestKnowledgeNewSetRefusesAReservedField(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "knowledge", "new", "--title", "X", "--set", "title=Y"); cliErrCode(err) != "reserved_field" {
		t.Errorf("err = %v, want reserved_field", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestTemplate|TestKnowledgeNewSetRefuses' -v`
Expected: the package does not compile — `unknown command "template"` at
runtime is not even reached; `newKnowledgeTemplateCmd` and friends do not
exist yet, but since these are only referenced inside `knowledge_template.go`
which does not exist, the actual failure is `cliErrCode`/`runCmdErr`
resolving fine while `root.Execute()` reports "unknown command \"template\""
— the tests fail on that error rather than a compile error, since
`knowledge.go` compiles standalone. Create the file in Step 3 and this
becomes moot.

- [ ] **Step 3: Add the core functions**

Append to `internal/core/template.go`:

```go
// TemplateInfo is one template's summary for `template ls`.
type TemplateInfo struct {
	Name    string `json:"name"`
	Enforce string `json:"enforce"`
	BuiltIn bool   `json:"builtin"`
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
		out = append(out, TemplateInfo{Name: name, Enforce: t.Enforce, BuiltIn: slices.Contains(builtins, name)})
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
// sections. It never blocks and never errors because of a violation — the
// document already exists.
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
	fields := map[string][]string{}
	for k, v := range fm.Extra {
		fields[k] = []string{fmt.Sprint(v)}
	}
	return templateViolations(tmpl, fields, body, true), nil
}
```

Add `"context"`, `"errors"` (already present), `"fmt"`, `"io/fs"` to
`internal/core/template.go`'s imports (`"errors"`, `"os"`,
`"path/filepath"`, `"regexp"`, `"slices"`, `"sort"`, `"strings"`,
`"gopkg.in/yaml.v3"` are already there from Tasks 2 and 4).

- [ ] **Step 4: Create the CLI file**

Create `internal/cli/knowledge_template.go`:

```go
package cli

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newKnowledgeTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "List, show and edit the shared knowledge templates",
		Long: "Templates live in one place, shared by every project: there is no\n" +
			"per-project template.",
	}
	cmd.AddCommand(newTemplateLsCmd(), newTemplateShowCmd(), newTemplateNewCmd(),
		newTemplateEditCmd(), newTemplateRmCmd(), newTemplateReinstallCmd(), newTemplateCheckCmd())
	return cmd
}

// parseSetFlags turns repeated --set name=value flags into a map. A flag
// without "=" is a usage error naming the exact text that was wrong.
func parseSetFlags(flags []string) (map[string]string, error) {
	out := map[string]string{}
	for _, f := range flags {
		name, value, ok := strings.Cut(f, "=")
		if !ok {
			return nil, core.ErrUsage("bad_set", `--set wants name=value, not "`+f+`"`,
				`trellis knowledge new --set owner=alice`)
		}
		out[name] = value
	}
	return out, nil
}

// withGlobalCore opens core without resolving a project or board:
// templates are global, so no working-directory resolution belongs here.
func withGlobalCore(fn func(c *core.Core) error) error {
	c, db, err := openCore()
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(c)
}

func sortedChoiceFields(choices map[string][]string) []string {
	names := make([]string, 0, len(choices))
	for name := range choices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderTemplateShow(t core.Template) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", t.Name)
	fmt.Fprintf(&b, "enforce: %s\n", t.Enforce)
	if len(t.Required) > 0 {
		fmt.Fprintf(&b, "required: %s\n", strings.Join(t.Required, ", "))
	}
	for _, name := range sortedChoiceFields(t.Choices) {
		fmt.Fprintf(&b, "choices.%s: %s\n", name, strings.Join(t.Choices[name], ", "))
	}
	b.WriteString("---\n")
	b.WriteString(t.Body)
	return strings.TrimRight(b.String(), "\n")
}

func newTemplateLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List the shared templates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withGlobalCore(func(c *core.Core) error {
				items, err := c.ListTemplates(cmd.Context())
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"templates": items}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, t := range items {
						builtin := ""
						if t.BuiltIn {
							builtin = "builtin"
						}
						fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, t.Enforce, builtin)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newTemplateShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a template's rules and skeleton",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.ShowTemplate(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return renderTemplateShow(t) })
			})
		},
	}
}

func newTemplateNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name>",
		Short: "Create a minimal template: enforce: warn, no rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.NewTemplate(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return "wrote " + t.Path })
			})
		},
	}
}

func newTemplateEditCmd() *cobra.Command {
	var body TextValue
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Replace a template's whole file: frontmatter and body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !body.Changed() {
				return core.ErrUsage("missing_body", "--body replaces the whole template file",
					"trellis knowledge template edit "+args[0]+" --body -   # then paste and Ctrl-D")
			}
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.EditTemplate(cmd.Context(), args[0], body.String())
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return "wrote " + t.Path })
			})
		},
	}
	cmd.Flags().Var(&body, "body", "the complete template file: frontmatter and skeleton")
	return cmd
}

func newTemplateRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a template file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				if err := c.DeleteTemplate(cmd.Context(), args[0]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": args[0]},
					func() string { return "deleted " + args[0] })
			})
		},
	}
}

func newTemplateReinstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reinstall <name>",
		Short: "Overwrite a template with its shipped version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.ReinstallTemplate(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return "reinstalled " + t.Path })
			})
		},
	}
}

func newTemplateCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <name> <slug>",
		Short: "Report a document's violations of a template, without blocking",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				violations, err := app.Core.CheckTemplate(cmd.Context(), app.Project.ID, args[0], args[1])
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"violations": violations}, func() string {
					if len(violations) == 0 {
						return "no violations"
					}
					return strings.Join(violations, "\n")
				})
			})
		},
	}
}
```

- [ ] **Step 5: Wire `--set` and register the `template` subcommand**

In `internal/cli/knowledge.go`, `newKnowledgeCmd`'s `cmd.AddCommand(...)`
call currently ends `newKnowledgeUptakeCmd())`. Change it to:

```go
	cmd.AddCommand(
		newKnowledgeNewCmd(), newKnowledgeShowCmd(), newKnowledgeLsCmd(), newKnowledgeEditCmd(),
		newKnowledgeRmCmd(), newKnowledgePinCmd(), newKnowledgePinsCmd(), newKnowledgeLintCmd(),
		newKnowledgeNominateCmd(), newKnowledgeNominationsCmd(), newKnowledgeEscalateCmd(),
		newKnowledgeDemoteCmd(), newKnowledgeVerifyCmd(), newKnowledgeHealthCmd(),
		newKnowledgeUptakeCmd(), newKnowledgeTemplateCmd())
```

Replace `newKnowledgeNewCmd` entirely:

```go
func newKnowledgeNewCmd() *cobra.Command {
	var title, body, summary TextValue
	var template, board, provenance string
	var tags, labels, setFlags []string
	var private bool

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a knowledge entry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !title.Changed() {
				return core.ErrUsage("missing_title", "a knowledge entry needs a title",
					`trellis knowledge new --title "Concurrency model" --template decision`)
			}
			fields, err := parseSetFlags(setFlags)
			if err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				doc, err := app.Core.CreateKnowledge(cmd.Context(), app.Project.ID, core.NewKnowledge{
					Title: title.String(), Body: body.String(), Template: template,
					Provenance: provenance,
					Summary:    summary.String(), Board: board, Tags: tags, Labels: labels,
					Private: private, Set: fields,
				})
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string {
					out := doc.Ref + "\n" + doc.Path
					for _, w := range doc.Warnings {
						out += "\nwarning: " + w
					}
					return out
				})
			})
		},
	}
	cmd.Flags().Var(&title, "title", "entry title")
	cmd.Flags().Var(&body, "body", "markdown body (default: the template)")
	cmd.Flags().Var(&summary, "summary", "one line, used as the pinned recap when none is written")
	cmd.Flags().StringVar(&template, "template", "note", strings.Join(core.Templates(), "|"))
	cmd.Flags().StringVar(&provenance, "provenance", "", "ingestion path: "+strings.Join(core.Provenances(), "|")+" (default authored)")
	cmd.Flags().StringVar(&board, "board", "", "associate with a board (association, never ownership)")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "free-form tags")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "labels from the project vocabulary")
	cmd.Flags().StringSliceVar(&setFlags, "set", nil, "name=value, repeatable; supplies a field the template asks for")
	cmd.Flags().BoolVar(&private, "private", false,
		"do not transmit this body automatically: no vector index, no recap, no content in the event log, pointer-only injection")
	return cmd
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestTemplate|TestKnowledgeNewSetRefuses' -v`
Expected: PASS, all seven.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

Run: `GOOS=windows go build ./...`
Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
git add internal/core/template.go internal/cli/knowledge.go \
        internal/cli/knowledge_template.go internal/cli/knowledge_template_test.go
git commit -m "$(cat <<'EOF'
feat(cli): knowledge template commands and --set

template ls/show/new/edit/rm/reinstall/check give an agent a place to read
a template's rules before writing to it (show is the one that matters:
trellis knowledge template show runbook) and a human a place to change
them. knowledge new --set name=value (repeatable) supplies the fields a
template asks for.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Lint reports an unrecognised frontmatter key

**Files:**
- Modify: `internal/core/lint.go`
- Create: `internal/core/template_lint_test.go`

**Interfaces:**
- Consumes: `c.templatesDir()`, `loadTemplate` (Tasks 2–3);
  `splitDocFile` (`internal/core/markdown.go`); existing `Lint`,
  `LintFinding`, `findingsFor` (`internal/core/artifact_lint_test.go`).
- Produces: `LintFinding` kind `unknown_field`, `Ref` set to the key name;
  `func (c *Core) knownExtraFields(ctx context.Context) (map[string]bool, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/template_lint_test.go`:

```go
package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintReportsAnUnknownFrontmatterKey(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Typo'd"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "title: Typo'd", "title: Typo'd\nprovenence: extracted", 1)
	if edited == string(raw) {
		t.Fatal("setup: expected the title line to be found and rewritten")
	}
	if err := os.WriteFile(doc.Path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	found := findingsFor(t, c, p.ID, doc.Slug)
	var got *LintFinding
	for i := range found {
		if found[i].Kind == "unknown_field" {
			got = &found[i]
		}
	}
	if got == nil || got.Ref != "provenence" {
		t.Fatalf("findings = %+v, want an unknown_field naming provenence", found)
	}
}

func TestLintDoesNotReportAFieldATemplateNames(t *testing.T) {
	c, p, _ := kbCore(t)
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\nenforce: warn\nrequired: [owner]\n---\n# {{title}}\n"
	if err := os.WriteFile(filepath.Join(dir, "owned.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Has an owner", Template: "owned", Set: map[string]string{"owner": "alice"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	for _, f := range findingsFor(t, c, p.ID, doc.Slug) {
		if f.Kind == "unknown_field" {
			t.Errorf("owner was flagged as unknown, but the owned template names it: %+v", f)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestLintReportsAnUnknownFrontmatterKey|TestLintDoesNotReportAFieldATemplateNames' -v`
Expected: both FAIL — no `unknown_field` finding is produced yet.

- [ ] **Step 3: Add `knownExtraFields` and wire it into `Lint`**

Append to `internal/core/lint.go`:

```go
// knownExtraFields is every field name any template on disk currently
// names, in required or choices. A frontmatter key outside this set, and
// outside the Frontmatter struct's own fields, is unrecognised no matter
// which template, if any, produced the document — a document does not
// remember which template created it. A template that fails to parse
// names nothing here; Lint reports the document's key regardless, which is
// the safer default when a template is broken.
func (c *Core) knownExtraFields(ctx context.Context) (map[string]bool, error) {
	dir, err := c.templatesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		t, err := loadTemplate(dir, name)
		if err != nil {
			continue
		}
		for _, f := range t.Required {
			known[f] = true
		}
		for f := range t.Choices {
			known[f] = true
		}
	}
	return known, nil
}
```

Change the `LintFinding.Kind` comment:

```go
	Kind string `json:"kind"` // stub, broken_anchor, orphan, missing_artifact, unknown_field
```

In `Lint`, add the lookup right after `docs, err := c.ListKnowledge(...)`:

```go
	docs, err := c.ListKnowledge(ctx, projectID, KnowledgeFilter{})
	if err != nil {
		return nil, err
	}
	knownFields, err := c.knownExtraFields(ctx)
	if err != nil {
		return nil, err
	}
```

Inside the per-document loop in `Lint`'s `c.Tx(...)` closure, directly after
the artifact-stub `for _, name := range artifactStubs { ... }` block and
before the `var inbound int` orphan check, insert:

```go
			raw, err := os.ReadFile(d.Path)
			if err != nil {
				return err
			}
			fm, _, err := splitDocFile(d.Path, raw)
			if err != nil {
				return err
			}
			var extraKeys []string
			for k := range fm.Extra {
				extraKeys = append(extraKeys, k)
			}
			slices.Sort(extraKeys)
			for _, k := range extraKeys {
				if !knownFields[k] {
					out = append(out, LintFinding{Kind: "unknown_field", Doc: d.Slug, Ref: k,
						Fix: "trellis knowledge template ls   # " + k + " is not in any template's required or choices"})
				}
			}
```

Add `"os"` to `internal/core/lint.go`'s imports (it currently imports
`"cmp"`, `"context"`, `"database/sql"`, `"slices"`, `"strconv"`,
`"strings"`, `"github.com/jmoiron/sqlx"`).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestLintReportsAnUnknownFrontmatterKey|TestLintDoesNotReportAFieldATemplateNames' -v`
Expected: PASS.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

Run: `GOOS=windows go build ./...`
Expected: exit 0.

This is the end of the base plan. Everything above stands on its own —
Tasks 1–6 fully implement
`docs/superpowers/specs/2026-09-16-knowledge-templates-design.md`. Tasks 7–11
below are TRELLIS-35 (source citations for `decision` and `finding`),
approved as an addition to this same plan.

- [ ] **Step 6: Commit**

```bash
git add internal/core/lint.go internal/core/template_lint_test.go
git commit -m "$(cat <<'EOF'
feat(core): lint reports a frontmatter key no template names

A key neither the Frontmatter struct nor any template's required or
choices recognises is now a lint finding, unknown_field, so a typo like
"provenence:" is surfaced rather than silently kept.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## TRELLIS-35: decision and finding entries must cite sources

The remaining five tasks implement TRELLIS-35's approved decisions: a
first-class `Sources` field, a `verify` rule that resolves internal
references while leaving URLs and prose alone, and the `decision` and
`finding` built-ins tightened to `enforce: reject`.

### Task 7: `Frontmatter.Sources`, filled from the file

**Files:**
- Modify: `internal/core/markdown.go`
- Modify: `internal/core/markdown_test.go`
- Modify: `internal/core/template.go`
- Modify: `internal/core/knowledge.go`
- Modify: `internal/core/knowledge_test.go`

**Interfaces:**
- Consumes: `Frontmatter`, `RenderDoc`, `SplitFrontmatter` (Task 1);
  `Knowledge`, `NewKnowledge`, `KnowledgeEdit`, `CreateKnowledge`,
  `refreshFromFile`, `EditKnowledgeFields`, `dedupeNames` (existing;
  `internal/core/doc_relations.go`).
- Produces:
  - `Frontmatter.Sources []string` (`yaml:"sources,omitempty"`)
  - `Knowledge.Sources []string` (`db:"-" json:"sources,omitempty"`)
  - `NewKnowledge.Sources []string`
  - `KnowledgeEdit.Sources *[]string`
  - `func cleanSources(in []string) []string`
  - `reservedFrontmatterFields["sources"] = true`

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/markdown_test.go`:

```go
func TestFrontmatterSourcesRoundTrip(t *testing.T) {
	fm := Frontmatter{Title: "X", Sources: []string{"https://example.com", "[[design-doc]]"}}
	raw := RenderDoc(fm, "body\n")
	back, _, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Sources) != 2 || back.Sources[0] != "https://example.com" || back.Sources[1] != "[[design-doc]]" {
		t.Errorf("Sources = %v", back.Sources)
	}
}
```

Append to `internal/core/knowledge_test.go`:

```go
func TestCreateKnowledgeRecordsSourcesAndReadsThemBack(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Cited", Sources: []string{"https://example.com", "  "},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if len(doc.Sources) != 1 || doc.Sources[0] != "https://example.com" {
		t.Fatalf("Sources = %v, want the blank entry dropped", doc.Sources)
	}

	got, err := c.LoadKnowledge(t.Context(), p.ID, doc.Slug)
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(got.Sources) != 1 || got.Sources[0] != "https://example.com" {
		t.Errorf("Sources after reload = %v", got.Sources)
	}
}

func TestEditKnowledgeReplacesSources(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Backfilled", Sources: []string{"https://example.com"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}

	next := []string{"https://example.com", "https://example.org"}
	got, err := c.EditKnowledgeFields(t.Context(), p.ID, doc.Slug,
		KnowledgeEdit{Sources: &next, IfVersion: &doc.Version})
	if err != nil {
		t.Fatalf("EditKnowledgeFields: %v", err)
	}
	if len(got.Sources) != 2 {
		t.Errorf("Sources = %v, want two", got.Sources)
	}
	raw, err := os.ReadFile(doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := SplitFrontmatter(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(fm.Sources) != 2 {
		t.Errorf("file lists %v, want two sources", fm.Sources)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestFrontmatterSources|TestCreateKnowledgeRecordsSources|TestEditKnowledgeReplacesSources' -v`
Expected: the package does not compile — `fm.Sources undefined`.

- [ ] **Step 3: Add the field, and reserve its name**

In `internal/core/markdown.go`, inside `type Frontmatter struct`, directly
after `Artifacts []string` and before `Created`:

```go
	// Sources cites what a claim in this entry is based on: a URL, a
	// path:lines pointer, a card ref, a wikilink, an absolute address, or
	// free prose. Free-form by design — recording that a claim was checked
	// against something, not that the something is true. A template's
	// verify rule (TRELLIS-35) checks only the internal-reference forms
	// (wikilinks and absolute addresses); everything else passes unchecked.
	Sources []string `yaml:"sources,omitempty"`
```

In `internal/core/template.go`, add `"sources": true,` to
`reservedFrontmatterFields`'s literal (any position in the map).

- [ ] **Step 4: Plumb `Sources` through `Knowledge`, `NewKnowledge` and `KnowledgeEdit`**

In `internal/core/knowledge.go`, inside `type Knowledge struct`'s
`// Computed for display.` block, directly after `Artifacts []ArtifactRef`:

```go
	Sources []string `db:"-" json:"sources,omitempty"`
```

Inside `type NewKnowledge struct`, directly after `Labels []string`:

```go
	// Sources cites what this entry's claims are based on. See
	// Frontmatter.Sources.
	Sources []string
```

Inside `type KnowledgeEdit struct`, directly after `Artifacts *[]string`:

```go
	// Sources, when non-nil, replaces the entry's source list.
	Sources *[]string
```

Add `cleanSources` next to `cmpOr` (near the bottom of the file):

```go
// cleanSources trims each source and drops blank ones, but keeps
// duplicates: citing the same source twice is redundant, not wrong, and
// unlike an artifact name a source is never resolved by identity.
func cleanSources(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
```

- [ ] **Step 5: Fill `Sources` on create and on every read**

In `CreateKnowledge`, inside the `fm := Frontmatter{...}` literal, add
`Sources: cleanSources(in.Sources),` (any position among the fields). Inside
the `doc = Knowledge{...}` literal that follows it, add
`Sources: cleanSources(in.Sources),`.

In `refreshFromFile`, directly after `doc.Provenance = fm.Provenance`, add:

```go
	doc.Sources = fm.Sources
```

(Unconditional, before the `if !changed` early return: Sources has no DB
column and no change-detection of its own — every read simply reflects
whatever the file currently says.)

- [ ] **Step 6: Wire `Sources` into `EditKnowledgeFields`**

Directly after the existing
`if in.Artifacts != nil { fields["artifacts"] = strings.Join(*in.Artifacts, "\n") }`,
add:

```go
		if in.Sources != nil {
			fields["sources"] = strings.Join(*in.Sources, "\n")
		}
```

Directly after the existing `if in.Artifacts != nil { fm.Artifacts = dedupeNames(*in.Artifacts) }`,
add:

```go
		if in.Sources != nil {
			fm.Sources = cleanSources(*in.Sources)
		}
```

Directly after the existing `doc.ContentHash = written` assignment (in the
block that updates `doc` after a successful write), add:

```go
		doc.Sources = fm.Sources
```

(`fields["sources"]` exists only so `checkWrite` sees the change, exactly
like `fields["artifacts"]` — the `edited` event loop iterates only `body`,
`title` and `summary`, so this key never produces an `edited` event.
Sources gets no dedicated event either: unlike artifacts, a source is never
resolved to a row, so there is no stub to track.)

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestFrontmatterSources|TestCreateKnowledgeRecordsSources|TestEditKnowledgeReplacesSources' -v`
Expected: PASS, all three.

- [ ] **Step 8: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/core/markdown.go internal/core/markdown_test.go \
        internal/core/template.go internal/core/knowledge.go internal/core/knowledge_test.go
git commit -m "$(cat <<'EOF'
feat(core): Frontmatter.Sources, filled from the file

sources: is a first-class list field, like artifacts, with no database
column: it is filled straight from the file on every create and every
read. EditKnowledgeFields can replace the list, for backfilling. Free-form
by design — a URL, a wikilink, an absolute address, a path:lines pointer or
prose all belong here; TRELLIS-35's verify rule (Task 9) is what checks the
internal-reference forms.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: `internal/vpath`, a minimal address parser

**Files:**
- Create: `internal/vpath/vpath.go`
- Create: `internal/vpath/vpath_test.go`

**Interfaces:**
- Consumes: nothing from this repository (a standalone package).
- Produces:
  - `type Collection string`
  - `const CollectionCards, CollectionKnowledge, CollectionArtifacts Collection`
  - `type Path struct { Project string; Collection Collection; Name string }`
  - `func Parse(s string) (Path, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/vpath/vpath_test.go`:

```go
package vpath

import "testing"

func TestParseEachCollection(t *testing.T) {
	cases := []struct {
		in   string
		want Path
	}{
		{"/TRELLIS/cards/TRELLIS-12", Path{"TRELLIS", CollectionCards, "TRELLIS-12"}},
		{"/trellis/cards/trellis-12", Path{"TRELLIS", CollectionCards, "TRELLIS-12"}},
		{"/TRELLIS/knowledge/concurrency-model", Path{"TRELLIS", CollectionKnowledge, "concurrency-model"}},
		{"/GLOBAL/knowledge/pain-point-analysis", Path{"GLOBAL", CollectionKnowledge, "pain-point-analysis"}},
		{"/global/knowledge/pain-point-analysis", Path{"GLOBAL", CollectionKnowledge, "pain-point-analysis"}},
		{"/TRELLIS/artifacts/photo.PNG", Path{"TRELLIS", CollectionArtifacts, "photo.PNG"}},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseRejectsMalformedAddresses(t *testing.T) {
	bad := []string{
		"/trellis/cards/not-a-ref",
		"/1BAD/cards/1BAD-1",
		"/TRELLIS/boards/main",
		"/GLOBAL/cards/GLOBAL-1",
		"/TRELLIS/artifacts/..",
		"/TRELLIS/knowledge/Not_A_Slug",
		"not/absolute/at/all",
		"/TRELLIS/knowledge/",
		"/TRELLIS/knowledge",
	}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): want an error", s)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vpath -v`
Expected: build failure — the package does not exist.

- [ ] **Step 3: Create `internal/vpath/vpath.go`**

```go
// Package vpath parses absolute Trellis addresses: /KEY/cards/<ref>,
// /KEY/knowledge/<slug>, /GLOBAL/knowledge/<slug> and
// /KEY/artifacts/<name>.
//
// This is a deliberately small subset of the grammar in
// docs/superpowers/specs/2026-09-16-virtual-paths-design.md — cards,
// knowledge and artifacts only, no boards, no anchors, no cross-project
// argument handling. That spec's own layer is not built yet; TRELLIS-35
// pulls this much of it forward because a template's verify rule needs to
// recognise an address without waiting on it. When the full layer lands,
// it extends this package — Parse's signature and Path's fields come from
// that spec, not from this one — rather than replacing it.
package vpath

import (
	"fmt"
	"regexp"
	"strings"
)

// Collection names which kind of object a Path addresses.
type Collection string

const (
	CollectionCards     Collection = "cards"
	CollectionKnowledge Collection = "knowledge"
	CollectionArtifacts Collection = "artifacts"
)

// Path is a parsed absolute address, /Project/Collection/Name. Project is
// the project key, upper-cased, or GLOBAL for a vault knowledge entry.
// Name is collection-specific: a card's full ref, a knowledge slug, or an
// artifact's file name — never further parsed here.
type Path struct {
	Project    string
	Collection Collection
	Name       string
}

var (
	keyRE     = regexp.MustCompile(`(?i)^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)
	cardRefRE = regexp.MustCompile(`(?i)^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*-[0-9]+$`)
	slugRE    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Parse reads an absolute address. It checks shape only: whether the
// object it names actually exists is for the caller to check against the
// database.
func Parse(s string) (Path, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return Path{}, fmt.Errorf("%q is not an absolute address", s)
	}
	project, collection, name := parts[1], parts[2], parts[3]

	if strings.EqualFold(project, "GLOBAL") && collection != "knowledge" {
		return Path{}, fmt.Errorf("GLOBAL is only valid with knowledge, not %s", collection)
	}

	switch collection {
	case "cards":
		if !keyRE.MatchString(project) {
			return Path{}, fmt.Errorf("%q is not a valid project key", project)
		}
		if !cardRefRE.MatchString(name) {
			return Path{}, fmt.Errorf("%q is not a valid card ref", name)
		}
		return Path{Project: strings.ToUpper(project), Collection: CollectionCards, Name: strings.ToUpper(name)}, nil
	case "knowledge":
		project = strings.ToUpper(project)
		if project != "GLOBAL" && !keyRE.MatchString(project) {
			return Path{}, fmt.Errorf("%q is not a valid project key", project)
		}
		if !slugRE.MatchString(name) {
			return Path{}, fmt.Errorf("%q is not a valid knowledge slug", name)
		}
		return Path{Project: project, Collection: CollectionKnowledge, Name: name}, nil
	case "artifacts":
		if !keyRE.MatchString(project) {
			return Path{}, fmt.Errorf("%q is not a valid project key", project)
		}
		if name == "." || name == ".." || strings.ContainsRune(name, 0) {
			return Path{}, fmt.Errorf("%q is not a valid artifact name", name)
		}
		return Path{Project: strings.ToUpper(project), Collection: CollectionArtifacts, Name: name}, nil
	default:
		return Path{}, fmt.Errorf("%q is not a known collection", collection)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/vpath -v`
Expected: PASS, both tests.

- [ ] **Step 5: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/vpath/vpath.go internal/vpath/vpath_test.go
git commit -m "$(cat <<'EOF'
feat(vpath): a minimal absolute-address parser

Parse recognises /KEY/cards/<ref>, /KEY/knowledge/<slug>,
/GLOBAL/knowledge/<slug> and /KEY/artifacts/<name> — the subset of
docs/superpowers/specs/2026-09-16-virtual-paths-design.md that a
template's verify rule needs. That spec's own layer (boards, anchors,
cross-project arguments) is not built here; this package is meant to be
extended by it, not replaced.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: The `verify` rule

**Files:**
- Modify: `internal/core/template.go`
- Modify: `internal/core/knowledge.go`
- Create: `internal/core/template_verify.go`
- Create: `internal/core/template_verify_test.go`

**Interfaces:**
- Consumes: `Template`, `TemplateRules`, `loadTemplate`, `templateViolations`
  (Tasks 2, 4); `vpath.Parse`, `vpath.Path`, `vpath.CollectionCards`,
  `vpath.CollectionKnowledge`, `vpath.CollectionArtifacts` (Task 8);
  `Frontmatter.Sources`, `cleanSources` (Task 7); existing `resolveDocRef`,
  `ParseWikilinks`, `ParseReference`, `ParseCardRef`, `resolveArtifactName`
  (`internal/core/doc_relations.go`, `internal/core/markdown.go`,
  `internal/core/ids.go`).
- Produces:
  - `TemplateRules.Verify []string` (`yaml:"verify,omitempty"`)
  - `Template.Verify []string`
  - `func (c *Core) resolvesInternalReference(tx *sqlx.Tx, projectID, s string) (ok, isRef bool, err error)`
  - `func (c *Core) verifyFieldValues(tx *sqlx.Tx, projectID string, values []string) ([]string, error)`
  - `func (c *Core) verifyBody(tx *sqlx.Tx, projectID, body string) ([]string, error)`
  - `func (c *Core) templateVerifyViolations(ctx context.Context, projectID string, t Template, fields map[string][]string, body string) ([]string, error)`
  - `CreateKnowledge` now builds `fields["sources"]` and calls
    `templateVerifyViolations`, appending its result to `violations`
    before the reject/warn decision.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/template_verify_test.go`:

```go
package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCustomTemplate(t *testing.T, c *Core, name, raw string) {
	t.Helper()
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatalf("templatesDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsWhenSourcesIsMissing(t *testing.T) {
	c, p, _ := kbCore(t)
	writeCustomTemplate(t, c, "cited",
		"---\nenforce: reject\nrequired: [sources]\nverify: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Claim", Template: "cited"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "sources") {
		t.Fatalf("err = %v, want template_violation naming sources", err)
	}
}

func TestVerifyRejectsAnUnresolvedCardAddress(t *testing.T) {
	c, p, _ := kbCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nverify: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Claim", Template: "cited", Sources: []string{"/XPSCTL/cards/XPSCTL-999"},
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "XPSCTL-999") {
		t.Fatalf("err = %v, want template_violation naming the unresolved card", err)
	}
}

func TestVerifyRejectsAnUnresolvedWikilink(t *testing.T) {
	c, p, _ := kbCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nverify: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Claim", Template: "cited", Sources: []string{"[[missing]]"},
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "[[missing]]") {
		t.Fatalf("err = %v, want template_violation naming the dangling wikilink", err)
	}
}

func TestVerifyAcceptsAURLAndProse(t *testing.T) {
	c, p, _ := kbCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nverify: [sources]\n---\n# {{title}}\n")

	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Claim", Template: "cited",
		Sources: []string{"https://example.com/paper", "discussed in standup on Tuesday"},
	})
	if err != nil {
		t.Fatalf("a URL and prose must pass unchecked: %v", err)
	}
}

func TestVerifyAcceptsAResolvedCardEntryAndArtifact(t *testing.T) {
	c, p, b := kbCore(t)
	writeCustomTemplate(t, c, "cited", "---\nenforce: reject\nverify: [sources]\n---\n# {{title}}\n")
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Evidence"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	entry, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Referenced entry"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	art := addArtifact(t, c, p.ID, "evidence.png", "\x89PNG\r\n\x1a\nx")

	_, err = c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Claim", Template: "cited",
		Sources: []string{
			"/XPSCTL/cards/" + card.Ref,
			"[[" + entry.Slug + "]]",
			"/XPSCTL/artifacts/" + art.Name,
		},
	})
	if err != nil {
		t.Fatalf("resolved references must pass: %v", err)
	}
}

func TestVerifyBodyRejectsADanglingLink(t *testing.T) {
	c, p, _ := kbCore(t)
	writeCustomTemplate(t, c, "linked", "---\nenforce: reject\nverify: [body]\n---\n# {{title}}\n")

	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Claim", Template: "linked", Body: "# Claim\n\nSee [[missing]].\n",
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "[[missing]]") {
		t.Fatalf("err = %v, want template_violation naming the dangling body link", err)
	}
}

func TestNoteTemplateStillAcceptsADanglingBodyLink(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Notes", Body: "# Notes\n\nSee [[not-written-yet]].\n",
	})
	if err != nil {
		t.Fatalf("a template with no verify rule must not block a dangling link: %v", err)
	}
	if doc.Slug != "notes" {
		t.Errorf("slug = %q", doc.Slug)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestVerify|TestNoteTemplateStillAccepts' -v`
Expected: the package does not compile — `t.Verify undefined` and
`c.templateVerifyViolations undefined`.

- [ ] **Step 3: Add `Verify` to the rules and to `loadTemplate`**

In `internal/core/template.go`, inside `type TemplateRules struct`, after
`Choices map[string][]string`:

```go
	// Verify names fields (or the literal "body") whose internal
	// references must resolve. Anything that is not a wikilink or an
	// absolute Trellis address passes unchecked: a URL, a path:lines
	// pointer and prose all cite without being verifiable.
	Verify []string `yaml:"verify,omitempty"`
```

Inside `type Template struct`, after `Choices map[string][]string`:

```go
	Verify []string
```

In `loadTemplate`'s return statement, add `Verify: rules.Verify,` to the
`Template{...}` literal.

- [ ] **Step 4: Create `internal/core/template_verify.go`**

```go
package core

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/vpath"
)

// wikilinkTarget reports whether s is written as a wikilink, [[target]] or
// [[target|alias]], and if so returns target.
func wikilinkTarget(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[[") || !strings.HasSuffix(s, "]]") || len(s) < 4 {
		return "", false
	}
	inner := s[2 : len(s)-2]
	inner, _, _ = strings.Cut(inner, "|")
	return strings.TrimSpace(inner), true
}

// absoluteAddressRE finds an address-shaped substring in free text: a
// slash, a key, one of the three known collections, and a name. It is
// deliberately loose — vpath.Parse is what actually validates the shape —
// so this only decides which substrings of a document body are worth
// checking at all.
var absoluteAddressRE = regexp.MustCompile(`/[A-Za-z][A-Za-z0-9-]*/(?:cards|knowledge|artifacts)/\S+`)

// bodyAbsoluteAddresses finds every substring of body shaped like an
// absolute address, skipping code spans and fences the same way
// ParseWikilinks does, and trimming trailing punctuation a sentence would
// leave attached ("... see /KEY/cards/KEY-12.").
func bodyAbsoluteAddresses(body string) []string {
	clean := fenceRE.ReplaceAllString(body, "")
	seen := map[string]bool{}
	var out []string
	for _, m := range absoluteAddressRE.FindAllString(clean, -1) {
		m = strings.TrimRight(m, ".,;:)]")
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// resolvesInternalReference reports whether s is recognised as an internal
// reference — a wikilink or an absolute Trellis address — and, when it is,
// whether the object it names exists. A value that is neither form is not
// a reference at all: isRef is false and ok means nothing. Every accepted
// form is resolved here, in one place, so a later layer that widens what
// resolves (cross-project wikilinks, once virtual paths ship) changes only
// this function.
func (c *Core) resolvesInternalReference(tx *sqlx.Tx, projectID, s string) (ok, isRef bool, err error) {
	if target, is := wikilinkTarget(s); is {
		toID, err := c.resolveDocRef(tx, projectID, ParseReference(target))
		return toID != nil, true, err
	}
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "/") {
		return false, false, nil
	}
	p, perr := vpath.Parse(trimmed)
	if perr != nil {
		// It was written as an address and does not even parse: an
		// unresolved reference, not prose that happens to start with "/".
		return false, true, nil
	}
	if p.Collection == vpath.CollectionKnowledge && p.Project == "GLOBAL" {
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM knowledge WHERE slug = ? AND global = 1`, p.Name); err != nil {
			return false, true, err
		}
		return n > 0, true, nil
	}
	var destProjectID string
	if err := tx.Get(&destProjectID, `SELECT id FROM project WHERE key = ?`, p.Project); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, true, nil
		}
		return false, true, err
	}
	switch p.Collection {
	case vpath.CollectionCards:
		ref := ParseCardRef(p.Name)
		if ref.Seq <= 0 {
			return false, true, nil
		}
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM card WHERE seq = ? AND project_id = ?`, ref.Seq, destProjectID); err != nil {
			return false, true, err
		}
		return n > 0, true, nil
	case vpath.CollectionKnowledge:
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM knowledge WHERE slug = ? AND project_id = ?`, p.Name, destProjectID); err != nil {
			return false, true, err
		}
		return n > 0, true, nil
	case vpath.CollectionArtifacts:
		toID, err := c.resolveArtifactName(tx, destProjectID, p.Name)
		if err != nil {
			return false, true, err
		}
		return toID != nil, true, nil
	}
	return false, true, nil
}

// verifyFieldValues checks every value of a verified field, returning the
// ones that do not resolve. A value that is not itself a wikilink or an
// absolute address is not an internal reference and is never flagged: a
// URL, a path:lines pointer, and prose all pass unchecked, which is the
// point of keeping sources free-form.
func (c *Core) verifyFieldValues(tx *sqlx.Tx, projectID string, values []string) ([]string, error) {
	var unresolved []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		ok, isRef, err := c.resolvesInternalReference(tx, projectID, v)
		if err != nil {
			return nil, err
		}
		if isRef && !ok {
			unresolved = append(unresolved, v)
		}
	}
	return unresolved, nil
}

// verifyBody checks every wikilink and absolute address written in a
// document body. Wikilinks resolve exactly as resolveDocRef does today —
// today's project-and-vault scope, not the cross-project resolution a
// later virtual-paths layer adds.
func (c *Core) verifyBody(tx *sqlx.Tx, projectID, body string) ([]string, error) {
	var unresolved []string
	for _, ref := range ParseWikilinks(body) {
		toID, err := c.resolveDocRef(tx, projectID, ref)
		if err != nil {
			return nil, err
		}
		if toID == nil {
			unresolved = append(unresolved, "[["+ref.Raw+"]]")
		}
	}
	for _, addr := range bodyAbsoluteAddresses(body) {
		ok, isRef, err := c.resolvesInternalReference(tx, projectID, addr)
		if err != nil {
			return nil, err
		}
		if isRef && !ok {
			unresolved = append(unresolved, addr)
		}
	}
	return unresolved, nil
}

// templateVerifyViolations runs a template's verify rule: every value of
// each named field, or every reference in the body when the field named is
// "body", must resolve. It opens its own read-only transaction rather than
// sharing the write transaction CreateKnowledge opens later, so a reject
// template still writes nothing when a reference fails to resolve.
func (c *Core) templateVerifyViolations(ctx context.Context, projectID string, t Template,
	fields map[string][]string, body string) ([]string, error) {
	if len(t.Verify) == 0 {
		return nil, nil
	}
	var out []string
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		for _, name := range t.Verify {
			var refs []string
			var err error
			if name == "body" {
				refs, err = c.verifyBody(tx, projectID, body)
			} else {
				refs, err = c.verifyFieldValues(tx, projectID, fields[name])
			}
			if err != nil {
				return err
			}
			for _, v := range refs {
				out = append(out, name+" cites "+v+", which does not resolve")
			}
		}
		return nil
	})
	return out, err
}
```

- [ ] **Step 5: Wire it into `CreateKnowledge`**

In `internal/core/knowledge.go`'s `CreateKnowledge`, the field-collection
line from Task 4 currently reads:

```go
	fields := map[string][]string{}
	for k, v := range in.Set {
		fields[k] = []string{v}
	}
```

Replace it with:

```go
	fields := map[string][]string{"sources": cleanSources(in.Sources)}
	for k, v := range in.Set {
		fields[k] = []string{v}
	}
```

Directly after the `violations := templateViolations(tmpl, fields, body, checkSections)`
line and before the `if len(violations) > 0 && tmpl.Enforce == "reject"`
check, insert:

```go
	verifyProblems, err := c.templateVerifyViolations(ctx, projectID, tmpl, fields, body)
	if err != nil {
		return Knowledge{}, err
	}
	violations = append(violations, verifyProblems...)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestVerify|TestNoteTemplateStillAccepts' -v`
Expected: PASS, all seven.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/core/template.go internal/core/knowledge.go \
        internal/core/template_verify.go internal/core/template_verify_test.go
git commit -m "$(cat <<'EOF'
feat(core): a template's verify rule resolves internal references

verify: [<field>...] checks every value of a field — or every wikilink and
absolute address in the body, when the field named is "body" — against
the database. A wikilink resolves exactly as resolveDocRef does today; an
absolute address is parsed by the new internal/vpath package. Everything
else — a URL, a path:lines pointer, prose — is not an internal reference
and passes unchecked, which is the whole point of keeping sources
free-form.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: `decision` and `finding` require and verify sources

**Files:**
- Modify: `internal/core/templates/decision.md`
- Modify: `internal/core/templates/finding.md`
- Modify: `internal/core/template.go`
- Modify: `internal/core/knowledge.go`
- Modify: `internal/core/template_test.go`
- Modify: `internal/core/knowledge_test.go`
- Create: `internal/core/template_citations_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2–9.
- Produces: `func templateViolationFix(tmplName string, violations []string) string`,
  used by `CreateKnowledge`'s `template_violation` error in place of the
  Task 4 fix string.

- [ ] **Step 1: Rewrite the two templates**

`internal/core/templates/decision.md` (keeps its sections; gains
frontmatter rules; no section is marked optional):

```markdown
---
enforce: reject
required: [sources]
verify: [sources, body]
---
# {{title}}

## Context

<!-- What is the situation? Why does this need a decision? -->

## Options considered

### Option A — <name>

- **What**:
- **Pros**:
- **Cons**:

### Option B — <name>

- **What**:
- **Pros**:
- **Cons**:

## Decision

<!-- What was chosen, in one sentence. -->

## Consequences

<!-- What this makes easy, and what it makes hard. -->
```

`internal/core/templates/finding.md` (rewritten as Fact / Evidence /
Scope, matching writing-knowledge's definition of a finding; no section is
marked optional):

```markdown
---
enforce: reject
required: [sources]
verify: [sources, body]
---
# {{title}}

## Fact

<!-- What is true, stated as a claim. -->

## Evidence

<!-- What supports the claim. Cite it in `sources`. -->

## Scope

<!-- Where this holds, and where it does not. -->
```

- [ ] **Step 2: Write the failing tests**

Update `TestSeededTemplatesAllParse` in `internal/core/template_test.go`
(from Task 3) — replace it entirely, since `decision` and `finding` are no
longer `warn`:

```go
func TestSeededTemplatesAllParse(t *testing.T) {
	c := testCore(t)
	c.WithKBRoot(t.TempDir())
	dir, err := c.templatesDir()
	if err != nil {
		t.Fatal(err)
	}
	wantEnforce := map[string]string{
		"decision": "reject", "finding": "reject",
		"note": "warn", "reference": "warn", "research": "warn", "runbook": "warn",
	}
	for _, name := range Templates() {
		tmpl, err := loadTemplate(dir, name)
		if err != nil {
			t.Fatalf("loadTemplate(%s): %v", name, err)
		}
		if tmpl.Enforce != wantEnforce[name] {
			t.Errorf("%s: enforce = %q, want %q", name, tmpl.Enforce, wantEnforce[name])
		}
	}
}
```

In `internal/core/knowledge_test.go`, `TestCreateKnowledgeWritesFileAndRow`
(existing, pre-plan test) creates a `decision` entry with no `Sources`,
which the now-`reject` template refuses. Change:

```go
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Concurrency model", Template: "decision", Summary: "Leases, not locks",
	})
```

to:

```go
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Concurrency model", Template: "decision", Summary: "Leases, not locks",
		Sources: []string{"https://example.com/design-notes"},
	})
```

In the same file, `TestListKnowledgeFiltersByTypeAndProvenance`'s `mk`
helper creates a `decision` and a `finding` entry with no `Sources` either.
Change:

```go
	mk := func(title, template, prov string) {
		t.Helper()
		if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
			Title: title, Template: template, Provenance: prov,
		}); err != nil {
			t.Fatalf("CreateKnowledge %s: %v", title, err)
		}
	}
	mk("Chosen storage", "decision", "authored")
	mk("Measured latency", "finding", "authored")
	mk("Overheard preference", "note", "extracted")
```

to:

```go
	mk := func(title, template, prov string, sources ...string) {
		t.Helper()
		if _, err := c.CreateKnowledge(ctx, p.ID, NewKnowledge{
			Title: title, Template: template, Provenance: prov, Sources: sources,
		}); err != nil {
			t.Fatalf("CreateKnowledge %s: %v", title, err)
		}
	}
	mk("Chosen storage", "decision", "authored", "https://example.com/storage-comparison")
	mk("Measured latency", "finding", "authored", "https://example.com/latency-numbers")
	mk("Overheard preference", "note", "extracted")
```

Create `internal/core/template_citations_test.go`:

```go
package core

import (
	"errors"
	"strings"
	"testing"
)

func TestDecisionTemplateRejectsWithNoSources(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Pick a queue", Template: "decision"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "sources") {
		t.Fatalf("err = %v, want template_violation naming sources", err)
	}
	if !strings.Contains(e.Fix, "template show decision") {
		t.Errorf("Fix = %q, want it to point at the template", e.Fix)
	}
}

func TestMissingSourcesFixIsARunnableSourceExample(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "X", Template: "decision"})
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("err = %v, want *Error", err)
	}
	if !strings.Contains(e.Fix, "--source") || !strings.Contains(e.Fix, "template show decision") {
		t.Errorf("Fix = %q, want a --source example and a pointer to template show", e.Fix)
	}
}

// A filled-in decision — every section replaced with real content, sources
// supplied — must pass. decision.md's own placeholders, "### Option A —
// <name>", are level-3 headings and must never be mistaken for a missing
// required section.
func TestFilledInDecisionIsAccepted(t *testing.T) {
	c, p, _ := kbCore(t)
	body := `# Use SQLite over Postgres

## Context

We need embedded storage with no server to run.

## Options considered

### Option A — SQLite

- **What**: an embedded, file-based database.
- **Pros**: no server, single file, battle-tested.
- **Cons**: one writer at a time.

### Option B — Postgres

- **What**: a client-server database.
- **Pros**: concurrent writers.
- **Cons**: another process to run and back up.

## Decision

SQLite: Trellis is single-writer by design already.

## Consequences

Backups are a file copy. A future multi-writer feature would need a rethink.
`
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Use SQLite over Postgres", Template: "decision", Body: body,
		Sources: []string{"https://sqlite.org/whentouse.html"},
	})
	if err != nil {
		t.Fatalf("a filled-in decision must be accepted: %v", err)
	}
	if len(doc.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", doc.Warnings)
	}
}

func TestFindingTemplateRejectsWithNoSources(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Flaky test", Template: "finding"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "sources") {
		t.Fatalf("err = %v, want template_violation naming sources", err)
	}
}

func TestFindingTemplateSkeletonHasFactEvidenceScope(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Flaky test", Template: "finding", Sources: []string{"https://ci.example.com/run/482"},
	})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	for _, heading := range []string{"## Fact", "## Evidence", "## Scope"} {
		if !strings.Contains(doc.BodyMD, heading) {
			t.Errorf("body missing %q:\n%s", heading, doc.BodyMD)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/core -run 'TestSeededTemplatesAllParse|TestCreateKnowledgeWritesFileAndRow|TestListKnowledgeFiltersByTypeAndProvenance|TestDecisionTemplate|TestFindingTemplate|TestMissingSourcesFix|TestFilledInDecision' -v`
Expected: the two templates already changed content in Step 1, so
`TestCreateKnowledgeWritesFileAndRow` and
`TestListKnowledgeFiltersByTypeAndProvenance` FAIL right now with
`template_violation` (this proves Step 1 took effect before the test edits
in Step 2 fully compile — reconcile by applying Step 2's edits together
with Step 1, then this run should show only `TestDecisionTemplate*` /
`TestFindingTemplate*` / `TestMissingSourcesFix` failing, since
`templateViolationFix` does not exist yet).

- [ ] **Step 4: Add `templateViolationFix`**

Append to `internal/core/template.go`:

```go
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
```

- [ ] **Step 5: Use it in `CreateKnowledge`**

In `internal/core/knowledge.go`'s `CreateKnowledge`, change:

```go
	if len(violations) > 0 && tmpl.Enforce == "reject" {
		return Knowledge{}, ErrUsage("template_violation",
			tmpl.Name+" does not meet its template:\n  - "+strings.Join(violations, "\n  - "),
			"trellis knowledge template show "+tmpl.Name)
	}
```

to:

```go
	if len(violations) > 0 && tmpl.Enforce == "reject" {
		return Knowledge{}, ErrUsage("template_violation",
			tmpl.Name+" does not meet its template:\n  - "+strings.Join(violations, "\n  - "),
			templateViolationFix(tmpl.Name, violations))
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core -run 'TestSeededTemplatesAllParse|TestCreateKnowledgeWritesFileAndRow|TestListKnowledgeFiltersByTypeAndProvenance|TestDecisionTemplate|TestFindingTemplate|TestMissingSourcesFix|TestFilledInDecision' -v`
Expected: PASS.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass. (`TestListKnowledgeFiltersByTypeAndProvenance/both_dimensions`
may fail about one run in five per the known flake — if so, re-run once to
confirm, note it, and move on.)

- [ ] **Step 8: Commit**

```bash
git add internal/core/templates/decision.md internal/core/templates/finding.md \
        internal/core/template.go internal/core/knowledge.go \
        internal/core/template_test.go internal/core/knowledge_test.go \
        internal/core/template_citations_test.go
git commit -m "$(cat <<'EOF'
feat(core): decision and finding require and verify sources

Both built-ins move to enforce: reject, required: [sources],
verify: [sources, body]. finding.md is rewritten as Fact / Evidence /
Scope, matching writing-knowledge's definition of a finding; decision.md
keeps its sections. Neither marks a section optional. The missing-sources
error's fix line is a runnable --source example, not just a pointer to
`template show` — an agent hitting reject for the first time has no other
way to learn --source exists yet.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 11: `--source` on `knowledge new` and `knowledge edit`

**Files:**
- Modify: `internal/core/template.go`
- Modify: `internal/core/knowledge.go`
- Modify: `internal/cli/knowledge.go`
- Create: `internal/cli/knowledge_source_test.go`

**Interfaces:**
- Consumes: `NewKnowledge.Sources`, `KnowledgeEdit.Sources` (Task 7);
  `EditKnowledgeFields` (existing); `parseSetFlags`, `withBoard`, `Emit`,
  `TextValue` (`internal/cli`).
- Produces: CLI flags `knowledge new --source` (repeatable) and
  `knowledge edit --source` (repeatable); `reservedFrontmatterFields`
  becomes `map[string]string` (name → the flag that actually sets it).

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/knowledge_source_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestKnowledgeNewSourceIsRepeatable(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "new", "--title", "Cited note",
		"--source", "https://example.com/a", "--source", "https://example.com/b", "--json")
	if !strings.Contains(out, "example.com/a") || !strings.Contains(out, "example.com/b") {
		t.Errorf("both sources were not recorded:\n%s", out)
	}
}

func TestKnowledgeEditSourceReplacesTheList(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Backfill me")

	runCmd(t, "knowledge", "edit", "backfill-me", "--source", "https://example.com/a", "--if-version", "1")

	out := runCmd(t, "knowledge", "show", "backfill-me", "--json")
	if !strings.Contains(out, "example.com/a") {
		t.Errorf("source was not backfilled:\n%s", out)
	}
}

func TestKnowledgeEditRequiresBodyOrSource(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Untouched")
	if _, err := runCmdErr(t, "knowledge", "edit", "untouched", "--if-version", "1"); cliErrCode(err) != "missing_body" {
		t.Errorf("err = %v, want missing_body", err)
	}
}

func TestKnowledgeNewSetRefusesSourcesByName(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "knowledge", "new", "--title", "X", "--set", "sources=https://example.com"); cliErrCode(err) != "reserved_field" {
		t.Errorf("err = %v, want reserved_field", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestKnowledgeNewSource|TestKnowledgeEditSource|TestKnowledgeEditRequiresBodyOrSource|TestKnowledgeNewSetRefusesSourcesByName' -v`
Expected: `TestKnowledgeNewSetRefusesSourcesByName` already PASSes (Task 7
already reserved the name); the other three FAIL — `unknown flag: --source`.

- [ ] **Step 3: Name the flag in the reserved-field error**

In `internal/core/template.go`, replace the Task 4/7 map:

```go
var reservedFrontmatterFields = map[string]bool{
	"title": true, "type": true, "status": true, "summary": true,
	"provenance": true, "private": true, "board": true, "tags": true,
	"labels": true, "artifacts": true, "created": true, "updated": true,
	"sources": true,
}
```

with:

```go
// reservedFrontmatterFields maps a Frontmatter struct field's YAML key to
// the flag that sets it. A --set value using one of these would collide
// with Extra's inline map, which yaml.v3 panics on rather than returning
// an error, so it is refused up front instead — and pointed at the flag
// that actually sets it, not just told no.
var reservedFrontmatterFields = map[string]string{
	"title": "--title", "type": "--template", "status": "(not yet settable)",
	"summary": "--summary", "provenance": "--provenance", "private": "--private",
	"board": "--board", "tags": "--tag", "labels": "--label",
	"artifacts": "`trellis artifact link`", "created": "(set automatically)",
	"updated": "(set automatically)", "sources": "--source",
}
```

In `internal/core/knowledge.go`'s `CreateKnowledge`, change:

```go
	for name := range in.Set {
		if reservedFrontmatterFields[name] {
			return Knowledge{}, ErrUsage("reserved_field",
				`"`+name+`" is a built-in frontmatter field and cannot be set with --set`,
				"trellis knowledge new --title ...   # use the matching flag instead")
		}
	}
```

to:

```go
	for name := range in.Set {
		if flag, reserved := reservedFrontmatterFields[name]; reserved {
			return Knowledge{}, ErrUsage("reserved_field",
				`"`+name+`" is a built-in frontmatter field and cannot be set with --set`,
				"use "+flag+" instead")
		}
	}
```

- [ ] **Step 4: Add `--source` to `knowledge new`**

In `internal/cli/knowledge.go`'s `newKnowledgeNewCmd`, add `sources []string`
to the `var` block (`var tags, labels, setFlags, sources []string`), pass
`Sources: sources` inside the `core.NewKnowledge{...}` literal, and add a
flag:

```go
	cmd.Flags().StringSliceVar(&sources, "source", nil,
		"cite what a claim is based on: a URL, path:lines, card ref, wikilink or absolute address; repeatable")
```

- [ ] **Step 5: Add `--source` to `knowledge edit`**

Replace `newKnowledgeEditCmd` entirely:

```go
func newKnowledgeEditCmd() *cobra.Command {
	var body TextValue
	var sources []string
	var ifVersion int64
	cmd := &cobra.Command{
		Use:   "edit <slug>",
		Short: "Replace an entry's body, or its sources, or both",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setSources := cmd.Flags().Changed("source")
			if !body.Changed() && !setSources {
				return core.ErrUsage("missing_body",
					"--body replaces the whole body; --source replaces the source list",
					"trellis knowledge edit "+args[0]+" --body @notes.md")
			}
			return withBoard(func(app *appCtx) error {
				edit := core.KnowledgeEdit{}
				if body.Changed() {
					b := body.String()
					edit.Body = &b
				}
				if setSources {
					edit.Sources = &sources
				}
				if ifVersion > 0 {
					edit.IfVersion = &ifVersion
				}
				doc, err := app.Core.EditKnowledgeFields(cmd.Context(), app.Project.ID, args[0], edit)
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string { return "wrote " + doc.Path })
			})
		},
	}
	cmd.Flags().Var(&body, "body", "new markdown body")
	cmd.Flags().StringSliceVar(&sources, "source", nil, "replace the source list; repeatable")
	cmd.Flags().Int64Var(&ifVersion, "if-version", 0, "the version you read; required (knowledge show --json)")
	return cmd
}
```

(This switches from the `EditKnowledge` convenience wrapper to
`EditKnowledgeFields` directly. For the body-only case the two are
equivalent: `EditKnowledge` is defined as
`c.EditKnowledgeFields(ctx, projectID, slug, KnowledgeEdit{Body: &body, IfVersion: ifVersion})`.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/cli -run 'TestKnowledgeNewSource|TestKnowledgeEditSource|TestKnowledgeEditRequiresBodyOrSource|TestKnowledgeNewSetRefusesSourcesByName|TestKnowledgeNewSetRefusesAReservedField' -v`
Expected: PASS.

- [ ] **Step 7: Run the full gates**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all pass.

Run: `GOOS=windows go build ./...`
Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
git add internal/core/template.go internal/core/knowledge.go \
        internal/cli/knowledge.go internal/cli/knowledge_source_test.go
git commit -m "$(cat <<'EOF'
feat(cli): --source on knowledge new and edit

--source is repeatable and separate from --set: sources is a list field,
and a --set value naming it would collide with Extra and is refused,
pointed at --source by name. knowledge edit gains --source to backfill an
entry created before this existed, using EditKnowledgeFields directly so
--body and --source can each be supplied without the other.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

**Spec coverage (base plan, `2026-09-16-knowledge-templates-design.md`):**
seeding and its once-only rule (Task 3); `reinstall` recreate/overwrite/
refuse-non-builtin (Task 5); `new --template` reject/warn on missing
field, bad choice, and — with `--body` — a missing section (Task 4); the
same under warn returning warnings (Task 4); an optional heading and
marker stripping (Task 2); `{{name}}`/`{{title}}` substitution (Task 2);
`template check` reporting without blocking (Task 5); a `--set` value
surviving `knowledge edit` (implicit: `Extra` round-trips through
`RenderDoc` on every write, Task 1, and `EditKnowledgeFields` never
touches `fm.Extra`, so a `--set` key is untouched by any edit — no
dedicated test was in the spec's list beyond the round trip already
covered); `lint` reporting an unrecognised key (Task 6); edit/hand-edit/
lint never checking a template (Task 4/6 wire checking only into
`CreateKnowledge` and `CheckTemplate`; nothing touches
`EditKnowledgeFields`'s template awareness because it has none); `template
new`/`edit` refusing a broken template and writing nothing (Task 5,
`NewTemplate`/`EditTemplate` call `validateTemplateRules`/`yaml.Unmarshal`
before `writeAtomic`); a template broken by hand failing `new --template`
with its path (Task 2's `loadTemplate`, consumed by Task 4). All covered.

**Spec coverage (TRELLIS-35):** `Frontmatter.Sources` filled from the file,
no DB column (Task 7); required-on-a-list-field semantics (Task 4's
`templateViolations`, generalized from the start to `map[string][]string`
so Task 9 needed no interface change); the `verify` rule, wikilink and
absolute-address forms, URL/prose passing unchecked (Task 9); the built-ins'
`enforce: reject`/`required: [sources]`/`verify: [sources, body]`, finding
rewritten as Fact/Evidence/Scope, decision unchanged sections, neither
optional (Task 10); `--source` repeatable, `--set` refusing a named field
and pointing at its flag, `edit --source` backfilling (Task 11); the
missing-sources fix as a runnable command (Task 10); required sections
matching `## ` exactly, with a filled-in decision test (Tasks 2 and 10);
all eight listed test scenarios (reject-no-sources, reject-unresolved-card,
reject-unresolved-wikilink, accept-URL-and-prose, accept-resolved-card/
entry/artifact, verify-body-rejects-dangling-link, note-template-still-
accepts-dangling-link) present in Task 9. All covered.

**Placeholder scan:** every step above carries complete code, not a
description of code; every commit stages explicit paths; no step says
"similar to Task N" without repeating the actual text.

**Type consistency, checked across tasks:**
- `templateViolations(t Template, fields map[string][]string, body string, checkSections bool) []string`
  is defined once, in Task 4, with the `map[string][]string` shape TRELLIS-35
  needs — Task 9 only adds a `"sources"` key to callers' maps, never changes
  the signature.
- `reservedFrontmatterFields` changes shape once, from `map[string]bool`
  (Task 4) to `map[string]string` (Task 11), with the one call site
  (`CreateKnowledge`) updated in the same task that changes the type — no
  task after 11 touches it.
- `Knowledge.Sources`, `NewKnowledge.Sources`, `KnowledgeEdit.Sources`,
  `Frontmatter.Sources` are introduced together in Task 7 and never
  renamed.
- `vpath.Path{Project, Collection, Name}` matches its use in
  `resolvesInternalReference` (Task 9) field-for-field.
- The two pre-existing tests that break under Task 10's stricter
  `decision`/`finding` (`TestCreateKnowledgeWritesFileAndRow`,
  `TestListKnowledgeFiltersByTypeAndProvenance`) are updated in that same
  task, with exact before/after text, rather than left to fail.

**Ambiguities ruled on (not specified verbatim by the spec or the card):**
1. A value starting with `/` that fails `vpath.Parse` (malformed, not just
   nonexistent) is treated as an *unresolved* internal reference, not as
   passing prose — otherwise a broken address would silently pass.
2. `required`/`choices` in this plan apply only to `Extra` (via `--set`)
   and, after TRELLIS-35, the named `Sources` field — not to any of
   `Frontmatter`'s other named fields (title, status, ...). Nothing in the
   spec or the card asks for the latter, and `reservedFrontmatterFields`
   already forbids `--set` from targeting them.
3. `verify: [body]` checks both `ParseWikilinks` output and a regex-found
   set of absolute-address-shaped substrings in the body, per the card's
   "every wikilink and absolute address in the body must resolve" — the
   card's own test list only asks for the wikilink case, so the absolute-
   address half is exercised only indirectly (via `resolvesInternalReference`,
   already tested against `sources`); no additional body-address test was
   added beyond what Task 9 lists, to avoid over-fitting a regex that the
   card does not specify.
4. CLI flags (`--set`, `--source`) use `StringSliceVar` (comma-splitting),
   matching every other repeatable flag in this codebase (`--tag`,
   `--label`, `--type`), rather than `StringArrayVar`. A value containing a
   literal comma would be mis-split; no existing flag in the codebase
   avoids this either, so consistency was chosen over a one-off exception.
