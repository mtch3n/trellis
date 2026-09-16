# Knowledge templates

Date: 2026-09-16
Status: design, not yet planned
Ships after: `2026-09-16-knowledge-disclosure-policy-design.md` and
`2026-09-16-knowledge-paths-design.md`

## Problem

Trellis ships six knowledge templates — decision, finding, note, reference,
research, runbook — as markdown skeletons embedded in the binary
(`internal/core/templates/`, read through `templateFS`). Three things are
missing.

1. **Nobody can add or change one.** They are compiled in.
2. **A template carries structure but no rules.** `runbook.md` lays out
   Preconditions, Steps, Verification and Rollback, and nothing checks that a
   runbook written from it still has them.
3. **Fields a template would want cannot survive.** `Frontmatter`
   (`internal/core/markdown.go`) is a fixed struct, and `yaml.Unmarshal` drops
   keys it does not know. A template that asks for `owner:` would see that key
   erased the first time Trellis rewrote the file.

## The model: render once

A template is a scaffold used at the moment a document is created. It fills in
the skeleton, substitutes values, and checks what it was given. After that the
document is an ordinary document. It does not remember which template produced
it, and later edits are never checked against one.

Rendering once keeps the feature small: no versioning, no link from a document
back to its template, no enforcement on edit.

## A template file

A template is a markdown file with YAML frontmatter — the same shape as the
documents it produces, and the format `SplitFrontmatter` already parses. YAML is
a superset of JSON, so JSON-style values parse too, but frontmatter is YAML.

```markdown
---
enforce: reject
required: [owner, severity]
choices:
  severity: [low, medium, high]
---
# {{title}}

**Owner:** {{owner}}

## Preconditions

## Steps

## Verification

## Rollback <!-- optional -->
```

The whole rule language is four things:

| Key | Meaning |
|---|---|
| `enforce` | `reject` or `warn`. Absent means `warn`. |
| `required` | Field names that must be supplied. |
| `choices` | For a field, the only values it may take. A field in `choices` but not in `required` may be omitted; if supplied, it must be one of the choices. |
| body `##` headings | The sections a document must have. A heading ending in `<!-- optional -->` is exempt, and the marker is removed when the template renders. |

Required sections are read from the skeleton rather than listed separately, so
the rule and the skeleton cannot drift apart.

`{{name}}` in the body is replaced by the value of field `name`. `{{title}}` is
always available. A placeholder with no value renders as empty.

Any other frontmatter key in a template is ignored, so a template stays valid if
a later version of this design adds keys.

## Where templates live

`<root>/templates/<name>.md`. One set, shared by every project.

There are no per-project templates. Nothing today needs a project to have a
different runbook than its neighbours, and a second layer would mean a lookup
order to explain and a second place to look when a template behaves oddly.

## Built-in templates

The six built-ins stay embedded in the binary, but only as the source for
seeding.

- **Seeding.** When `<root>/templates/` does not exist, it is created and the
  six built-ins are written into it. Seeding happens only when the directory is
  absent. Once it exists, Trellis never writes into it on its own, so a built-in
  the user deleted stays deleted.
- **After seeding, the files are the templates.** A built-in is edited and
  deleted exactly like one the user created. Upgrading Trellis never touches
  them.
- **`template reinstall <name>`** overwrites `<root>/templates/<name>.md` with
  the shipped version. It recreates a deleted built-in, and it discards any
  edits to an existing one. It is refused for a name that is not a built-in.

There is no versioning. A template is used at creation and forgotten, so there
is nothing downstream that needs to know which revision produced a document.

The six shipped templates are rewritten into this format. The initial rules are
deliberately permissive — `enforce: warn`, no required fields — so that
existing workflows are unchanged until a user tightens them.

## Commands

```
trellis knowledge template ls
trellis knowledge template show <name>
trellis knowledge template new <name>
trellis knowledge template edit <name>
trellis knowledge template rm <name>
trellis knowledge template reinstall <name>
trellis knowledge template check <name> <slug>
```

`show` is the one an agent needs. It returns the rules and the skeleton, so an
agent can read what a runbook requires before writing one and get it right the
first time:

```
trellis knowledge template show runbook
trellis knowledge new --template runbook --set owner=alice --set severity=high
```

`ls` shows each template's name, `enforce` setting, and whether it is a
built-in.

`new <name>` writes a minimal template — `enforce: warn`, no rules, a
`# {{title}}` heading — and refuses a name that already exists. `edit <name>`
takes the complete template, frontmatter and body, through `--body` or stdin,
and replaces the file. `rm <name>` deletes the file.

A template is checked before `new` or `edit` writes it: its frontmatter must
parse, `enforce` must be `reject` or `warn`, and every field named in `choices`
must have a non-empty list. A template that fails is not written. A template
that was hand-edited into a broken state is reported when it is next used, and
the error names its file path — a broken template must never be silently
treated as having no rules.

## Supplying fields: `--set`

`knowledge new` gains a repeatable `--set name=value`.

This is not a way around `enforce: reject`. It is how a value gets supplied.
An interactive tool could stop and ask a person for each value; a command line
used by an agent cannot, so the values are passed up front. A required field
that is not supplied is missing, and a `reject` template refuses the document.

Supplied fields are written into the new document's frontmatter.

## When a template is checked

A template is checked when it is selected, and only then.

| Situation | Checked |
|---|---|
| `new --template X`, no `--body` | Fields. Sections are present by construction, because the skeleton produced them. |
| `new --template X --body "..."` | Fields **and** sections. The caller wrote the body, so its headings are what count. |
| `template check X <slug>` | Fields and sections of an existing document, against a template the caller names. |
| `knowledge edit`, a hand edit, `lint` | Nothing. After creation the document has no template. |

Under `enforce: reject`, `new` refuses the document and writes nothing. The
error lists every missing field, every value outside its `choices`, and every
missing section, and its fix is `trellis knowledge template show X`.

Under `enforce: warn`, `new` writes the document and returns the same list as
warnings.

`template check` never blocks. It reports, whatever the template's `enforce`
setting, because the document already exists.

A section counts as present when its heading exists. An empty section is not a
violation — a document created from a skeleton is empty by design.

`new` without `--template` keeps its current behaviour of using `note`.

## Prerequisite: frontmatter keeps every key

Fields a template supplies live in the document's frontmatter:

```yaml
---
title: Rollback the API
type: runbook
owner: alice
severity: high
---
```

`Frontmatter` is a fixed struct today, so `knowledge edit` — which re-renders
the frontmatter from the struct — would drop `owner` and `severity`. That breaks
even in the render-once model: the fields a template wrote disappear on the
first edit.

So `Frontmatter` gains an inline map that captures every key the struct does
not name, and `RenderDoc` writes them back:

```go
Extra map[string]any `yaml:",inline"`
```

Trellis stops destroying keys a file contains. That is the source-of-truth
invariant applied to frontmatter, and it also removes the silent drop that
TRELLIS-19 objected to.

It does not loosen the vocabulary TRELLIS-19 wanted kept tight. A template names
the fields it expects, which is a deliberate extension. `knowledge lint` reports
a frontmatter key that neither the struct nor any template names, so a typo like
`provenence:` is surfaced rather than silently kept or silently lost.

## Non-goals

- Versioning, and any Doctor check that compares an installed template against
  the shipped one.
- Per-project templates.
- Checking documents on edit, on hand edit, or during `lint`.
- A document recording which template created it.
- Rules beyond `required`, `choices` and sections — no patterns, no defaults, no
  types. Each is easy to add later as one more frontmatter key.
- Scripting inside templates. A template substitutes values and nothing else.

## Testing

- Seeding writes the six built-ins into an absent directory and never writes
  into an existing one, including after a built-in is deleted.
- `reinstall` restores a deleted built-in, overwrites an edited one, and refuses
  a name that is not a built-in.
- `new --template` under `reject` refuses a missing required field, a value
  outside `choices`, and — when `--body` is given — a missing section, and
  writes no file in each case.
- The same three cases under `warn` write the file and return warnings.
- A heading marked `<!-- optional -->` is not required, and the marker does not
  appear in the rendered document.
- `{{name}}` substitution, including a placeholder with no value.
- `template check` reports violations and never refuses, under either setting.
- A key supplied with `--set` survives a `knowledge edit`.
- `lint` reports a frontmatter key that neither the struct nor any template
  names.
- `edit` never checks against a template.
- `template new` and `template edit` refuse a template whose frontmatter does not
  parse, whose `enforce` is neither `reject` nor `warn`, or whose `choices` entry
  is empty — and write nothing.
- A template broken by hand makes `new --template` fail with the template's file
  path in the error, rather than proceeding as if it had no rules.
