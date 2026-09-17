import { sentence, wordList } from '@/lib/format'

/** A vault template and its rules, as `GET /api/templates` lists them. */
export interface TemplateInfo {
  name: string
  /** `reject` refuses an entry that breaks a rule; `warn` saves it and says so. */
  enforce: 'reject' | 'warn'
  builtin: boolean
  /** Frontmatter fields an entry must have. */
  required: string[]
  /** Fields whose value must be one of a fixed set. */
  choices: Record<string, string[]>
  /** The `## ` headings an entry must keep. */
  sections: string[]
}

/** The value a template picker holds for an entry that follows none. */
export const NO_TEMPLATE = ''

/**
 * An entry's frontmatter beyond what Trellis names itself: what templates
 * and `set` write. A YAML list arrives as an array.
 */
export type FieldValues = Record<string, string | string[]>

/** A field's values as a list, blanks left out. */
export function valuesOf(fields: FieldValues | undefined, name: string) {
  const value = fields?.[name]
  const list = Array.isArray(value) ? value : value === undefined ? [] : [value]
  return list.filter((item) => item.trim() !== '')
}

/** Fields a create form never asks for: the title has its own field, and the board is the one open. */
const IMPLIED = new Set(['title', 'board'])

/**
 * What an entry under this template still has to be given, sorted by how a
 * form asks for it: the summary and sources have fields of their own, a field
 * with fixed choices is a select, and anything else is a line of text sent as
 * `set`. A field the entry already `has` is not asked for again, unless its
 * value is not one of the choices.
 */
export function templateAsks(template: TemplateInfo | undefined, has: FieldValues = {}) {
  const required = new Set(template?.required ?? [])
  const choiceFields = Object.keys(template?.choices ?? {})
  const choices = Object.entries(template?.choices ?? {})
    .filter(([field, options]) => {
      const values = valuesOf(has, field)
      return values.length === 0 || values.some((value) => !options.includes(value))
    })
    .map(([field, options]) => ({ field, options, required: required.has(field) }))
    .sort((a, b) => a.field.localeCompare(b.field))
  return {
    summary: required.has('summary'),
    sources: required.has('sources'),
    choices,
    fields: [...required]
      .filter((field) => !IMPLIED.has(field) && field !== 'summary' && field !== 'sources')
      .filter((field) => !choiceFields.includes(field) && valuesOf(has, field).length === 0)
      .sort(),
  }
}

/** One row of an entry's fields: its value, and what its template says about it. */
export interface FieldRow {
  name: string
  value: string | string[] | undefined
  /** The values the template allows, when it fixes them. */
  options?: string[]
  required: boolean
  /** The template names this field. A field it does not name can be removed. */
  known: boolean
}

/**
 * The fields to show for an entry: every field its template names, set or
 * not, then any other field the entry carries. Title, summary, sources and
 * the like have places of their own and are not repeated here.
 */
export function fieldRows(template: TemplateInfo | undefined, fields: FieldValues = {}): FieldRow[] {
  const required = new Set(template?.required ?? [])
  const choices = template?.choices ?? {}
  const named = [...new Set([...Object.keys(choices), ...required])]
    .filter((name) => !IMPLIED.has(name) && name !== 'summary' && name !== 'sources')
    .sort()
  const others = Object.keys(fields).filter((name) => !named.includes(name)).sort()
  return [
    ...named.map((name) => ({ name, value: fields[name], options: choices[name], required: required.has(name), known: true })),
    ...others.map((name) => ({ name, value: fields[name], required: false, known: false })),
  ]
}

/** What choosing this template means for the entry, in a sentence or two. */
export function describeTemplate(template: TemplateInfo | undefined) {
  if (!template) return 'A blank entry, with no rules to meet.'
  const asks = templateAsks(template)
  const needs = [
    asks.sources && 'sources',
    asks.summary && 'a summary',
    ...asks.choices.filter((choice) => choice.required).map((choice) => sentence(choice.field).toLowerCase()),
    ...asks.fields.map((field) => sentence(field).toLowerCase()),
  ].filter((need): need is string => Boolean(need))
  const rule = template.enforce === 'reject'
    ? 'Strict: an entry that breaks a rule is refused.'
    : 'Advisory: an entry that breaks a rule is saved with a warning.'
  const parts = [rule]
  if (needs.length) parts.push(`It needs ${wordList(needs)}.`)
  if (template.sections.length) {
    const noun = template.sections.length === 1 ? 'section' : 'sections'
    parts.push(`Its body keeps the ${noun} ${wordList(template.sections)}.`)
  }
  return parts.join(' ')
}

// The server skips code the same way: fenced blocks and inline spans.
const CODE = /```[\s\S]*?```|`[^`\n]*`/g
const SECTION = /^## (.+?)[ \t]*$/gm

/** The sections a template keeps that this body lacks, matched the way the server matches them. */
export function missingSections(body: string, sections: string[]) {
  const present = new Set([...body.replace(CODE, '').matchAll(SECTION)].map((match) => match[1].trim()))
  return sections.filter((section) => !present.has(section))
}

/** The body with empty sections added at its end, one per heading. */
export function withSections(body: string, sections: string[]) {
  if (sections.length === 0) return body
  const added = sections.map((section) => `## ${section}\n`).join('\n')
  return body.trim() ? `${body.trimEnd()}\n\n${added}` : added
}

/** What the rule fields of a form add to a request: the choice and other fields as `set`, blanks left out. */
export function templateSet(template: TemplateInfo | undefined, values: Record<string, string>, has: FieldValues = {}) {
  const asks = templateAsks(template, has)
  const set = Object.fromEntries(
    [...asks.choices.map((choice) => choice.field), ...asks.fields]
      .map((field) => [field, (values[field] ?? '').trim()])
      .filter(([, value]) => value !== ''),
  )
  return Object.keys(set).length ? set : undefined
}

/** A required choice with nothing chosen. */
export function unchosenFields(template: TemplateInfo | undefined, values: Record<string, string>, has: FieldValues = {}) {
  return templateAsks(template, has).choices.filter((choice) => choice.required && !values[choice.field]).map((choice) => choice.field)
}

/** What an entry has now, as far as a template's rules look at it. */
export interface SwitchedEntry {
  summary?: string
  sources?: string[]
  body?: string
  fields?: FieldValues
}

/**
 * Whether moving this entry onto a template needs more than the switch
 * itself. Only a strict template does: an advisory one takes the entry as it
 * is and warns.
 */
export function switchNeedsDialog(template: TemplateInfo, entry: SwitchedEntry) {
  if (template.enforce !== 'reject') return false
  const asks = templateAsks(template, entry.fields)
  return (
    (asks.sources && !entry.sources?.length) ||
    (asks.summary && !entry.summary?.trim()) ||
    // An optional choice left unset is no reason to ask; a value outside
    // the choices is.
    asks.choices.some((choice) => choice.required || valuesOf(entry.fields, choice.field).length > 0) ||
    asks.fields.length > 0 ||
    missingSections(entry.body ?? '', template.sections).length > 0
  )
}
