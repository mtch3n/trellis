import { sentence } from '@/lib/format'

/** A machine-wide setting, as `GET /api/settings` lists it. */
export type SettingType = 'int' | 'number' | 'bool' | 'string' | 'duration' | 'enum' | 'list'
export type SettingValue = number | boolean | string | string[]

export interface Setting {
  key: string
  type: SettingType
  value: SettingValue
  default: SettingValue
  /** Where the value comes from: the built-in default, or `config.yaml`. */
  source: 'default' | 'config'
  /** False for keys the web may not change: `ui.*` and `search.vector.*`. */
  editable: boolean
  /** The running daemon keeps the old value until it restarts. */
  restart: boolean
  description: string
  choices?: string[]
  min?: number
}

export interface SettingsResponse {
  file: string
  settings: Setting[]
  /** After a save: the changed keys that wait for a daemon restart. */
  restart?: string[]
}

/** What a control holds while it is edited: a switch's state, or the text in a box. */
export type SettingInput = string | boolean

/**
 * Settings grouped by the first segment of the key, in the order they are
 * listed, except that a group the web cannot change at all goes last: the
 * page opens on what can be changed.
 */
export function settingGroups(settings: Setting[]) {
  const groups: { name: string; settings: Setting[] }[] = []
  for (const setting of settings) {
    const name = setting.key.split('.')[0]
    const group = groups.find((item) => item.name === name)
    if (group) group.settings.push(setting)
    else groups.push({ name, settings: [setting] })
  }
  const locked = (group: { settings: Setting[] }) => group.settings.every((setting) => !setting.editable)
  return [...groups.filter((group) => !locked(group)), ...groups.filter(locked)]
}

/** Words a key abbreviates, written the way a person reads them. */
const WORDS: Record<string, string> = {
  api: 'API', fts: 'FTS', http: 'HTTP', id: 'ID', llm: 'LLM', ls: 'list', mcp: 'MCP',
  ttl: 'TTL', ui: 'UI', url: 'URL', wal: 'WAL',
}

/** Part of a key as words: `default_columns` reads "Default columns", `ttl` "TTL". */
function humanize(text: string) {
  const words = text.split(/[_.\s-]+/).filter(Boolean).map((word) => WORDS[word.toLowerCase()] ?? word)
  return sentence(words.join(' '))
}

/** A group's heading: `search` reads "Search", `ui` "UI". */
export function groupTitle(name: string) {
  return humanize(name)
}

/** A setting's label, without its group: `board.default_columns` reads "Default columns". */
export function settingLabel(key: string) {
  return humanize(key.split('.').slice(1).join(' ') || key)
}

/** A value as its control shows it. A list is one line, comma separated. */
export function toInput(type: SettingType, value: SettingValue): SettingInput {
  if (type === 'bool') return Boolean(value)
  if (Array.isArray(value)) return value.join(', ')
  return String(value ?? '')
}

/**
 * A control's contents as the typed value the server takes. A number that
 * does not parse is sent as typed, so the server's problem names it.
 */
export function fromInput(type: SettingType, input: SettingInput): SettingValue {
  if (type === 'bool') return Boolean(input)
  const text = String(input).trim()
  if (type === 'list') return text.split(',').map((item) => item.trim()).filter(Boolean)
  if (type === 'int' || type === 'number') {
    const number = Number(text)
    return text !== '' && Number.isFinite(number) ? number : text
  }
  return text
}

/** Where a value comes from, in words. */
export function sourceLabel(setting: Setting) {
  return setting.source === 'config' ? 'config.yaml' : 'Default'
}

/**
 * The server's problems, sorted to the settings they name. Each reads
 * `key: what is wrong`; one that names no listed key is general.
 */
export function problemsByKey(problems: string[], keys: string[]) {
  const byKey: Record<string, string> = {}
  const general: string[] = []
  for (const problem of problems) {
    const at = problem.indexOf(': ')
    const key = at > 0 ? problem.slice(0, at) : ''
    if (keys.includes(key)) byKey[key] = sentence(problem.slice(at + 2))
    else general.push(problem)
  }
  return { byKey, general }
}
