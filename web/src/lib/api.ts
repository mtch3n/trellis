export interface Me {
  handle: string
  org: string
  csrf: string
}

export interface Project {
  id: string
  key: string
  name: string
}

export interface Board {
  id: string
  owner: string
  name: string
  scope: string
}

export interface Comment {
  id: string
  actor: string
  body: string
  created_at: string
}

export interface CardEvent {
  id: number
  kind: string
  actor: string
  created_at: string
}

export interface Card {
  id: string
  board: string
  ref: string
  title: string
  body: string
  column: string
  done: boolean
  version: number
  claimed_by?: string
  claim_until?: string
  created_by: string
  created_at: string
  updated_at: string
  blocked_by?: string[]
  comments?: Comment[]
  events?: CardEvent[]
}

export interface Entry {
  id: string
  owner: string
  project: string
  scope: string
  slug: string
  title: string
  summary?: string
  body?: string
  template?: string
  provenance: string
  private?: boolean
  tags?: string[]
  labels?: string[]
  version: number
  verify_by?: string
  created_by: string
  created_at: string
  updated_at: string
  warnings?: string[]
}

export interface EntryHit {
  slug: string
  title: string
  scope: string
  owner: string
  template?: string
  summary?: string
}

export interface Nomination {
  slug: string
  title: string
  actor: string
  reason: string
  nominations: number
  cited: number
  pinned: number
  created_at: string
}

export interface ShareResult {
  slug: string
  scope: string
  labels: string[]
  verify_by?: string
}

export class ApiError extends Error {
  status: number
  code: string
  kind: string
  fix: string

  constructor(status: number, body: { code?: string; kind?: string; message?: string; fix?: string }) {
    super(body.message || `The server answered ${status}.`)
    this.status = status
    this.code = body.code ?? ''
    this.kind = body.kind ?? ''
    this.fix = body.fix ?? ''
  }
}

export function errorText(err: unknown) {
  if (err instanceof ApiError) return err.fix ? `${err.message} (${err.fix})` : err.message
  return err instanceof Error ? err.message : String(err)
}

let csrf = ''

export function setCsrf(value: string) {
  csrf = value
}

export function loginPath(next: string) {
  return next && next !== '/' && !next.startsWith('/login') ? `/login?next=${encodeURIComponent(next)}` : '/login'
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET') headers['X-Trellis-CSRF'] = csrf
  const response = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  })
  if (response.status === 204) return undefined as T
  const text = await response.text()
  let parsed: unknown = undefined
  try {
    parsed = text ? JSON.parse(text) : undefined
  } catch {
    parsed = { message: text.trim() }
  }
  if (!response.ok) {
    if (response.status === 401 && path.startsWith('/api/') && path !== '/api/me') {
      window.location.assign(loginPath(window.location.pathname + window.location.search))
    }
    throw new ApiError(response.status, (parsed ?? {}) as Record<string, string>)
  }
  return parsed as T
}

const seg = encodeURIComponent

function project(key: string) {
  return `/api/p/${seg(key)}`
}

export const api = {
  login: (token: string) => request<void>('POST', '/login', { token }),
  logout: () => request<object>('POST', '/api/logout'),
  me: () => request<Me>('GET', '/api/me'),
  projects: () => request<{ projects: Project[] }>('GET', '/api/projects').then((r) => r.projects ?? []),
  boards: (key: string) => request<{ boards: Board[] }>('GET', `${project(key)}/boards`).then((r) => r.boards ?? []),
  newBoard: (key: string, name: string) =>
    request<{ board: Board }>('POST', `${project(key)}/boards`, { name }).then((r) => r.board),
  cards: (key: string, board: string) =>
    request<{ cards: Card[] }>('GET', `${project(key)}/b/${seg(board)}/cards`).then((r) => r.cards ?? []),
  newCard: (key: string, board: string, title: string, body: string, column?: string) =>
    request<{ card: Card }>('POST', `${project(key)}/b/${seg(board)}/cards`, { title, body, column }).then((r) => r.card),
  card: (key: string, ref: string) =>
    request<{ card: Card }>('GET', `${project(key)}/cards/${seg(ref)}`).then((r) => r.card),
  editCard: (key: string, ref: string, edit: { title?: string; body?: string; if_version?: number }) =>
    request<{ card: Card }>('PATCH', `${project(key)}/cards/${seg(ref)}`, edit).then((r) => r.card),
  moveCard: (key: string, ref: string, column: string) =>
    request<{ card: Card }>('POST', `${project(key)}/cards/${seg(ref)}/move`, { column }).then((r) => r.card),
  commentCard: (key: string, ref: string, body: string) =>
    request<{ card: Card }>('POST', `${project(key)}/cards/${seg(ref)}/comments`, { body }).then((r) => r.card),
  claimCard: (key: string, ref: string) =>
    request<{ card: Card }>('POST', `${project(key)}/cards/${seg(ref)}/claim`, {}).then((r) => r.card),
  releaseCard: (key: string, ref: string) =>
    request<{ card: Card }>('POST', `${project(key)}/cards/${seg(ref)}/release`).then((r) => r.card),
  entries: (key: string) =>
    request<{ entries: Entry[] }>('GET', `${project(key)}/vault`).then((r) => r.entries ?? []),
  newEntry: (key: string, entry: { slug?: string; title: string; summary?: string; body?: string }) =>
    request<{ entry: Entry }>('POST', `${project(key)}/vault`, entry).then((r) => r.entry),
  entry: (key: string, slug: string) =>
    request<{ entry: Entry }>('GET', `${project(key)}/vault/${seg(slug)}`).then((r) => r.entry),
  editEntry: (key: string, slug: string, edit: { title?: string; summary?: string; body?: string; if_version?: number }) =>
    request<{ entry: Entry }>('PATCH', `${project(key)}/vault/${seg(slug)}`, edit).then((r) => r.entry),
  publish: (key: string, slug: string) => request<ShareResult>('POST', `${project(key)}/vault/${seg(slug)}/publish`),
  demote: (key: string, slug: string) => request<ShareResult>('POST', `${project(key)}/vault/${seg(slug)}/demote`),
  verify: (key: string, slug: string) => request<ShareResult>('POST', `${project(key)}/vault/${seg(slug)}/verify`),
  promote: (key: string, slug: string, reason: string) =>
    request<ShareResult>('POST', `${project(key)}/vault/${seg(slug)}/promote`, { reason }),
  search: (key: string, q: string) =>
    request<{ hits: EntryHit[] }>('GET', `${project(key)}/search?q=${seg(q)}`).then((r) => r.hits ?? []),
  nominations: (key: string) =>
    request<{ nominations: Nomination[] }>('GET', `${project(key)}/nominations`).then((r) => r.nominations ?? []),
  unverified: (key: string) =>
    request<{ entries: Entry[] }>('GET', `${project(key)}/unverified`).then((r) => r.entries ?? []),
}

export const DEFAULT_COLUMNS = ['backlog', 'in-progress', 'review', 'done']

export function boardColumns(cards: Card[]) {
  const extra = cards.map((card) => card.column).filter((column) => !DEFAULT_COLUMNS.includes(column))
  return [...DEFAULT_COLUMNS, ...new Set(extra)]
}

export function vaultPath(key: string, slug: string) {
  return `/p/${seg(key)}/vault/${slug.split('/').map(seg).join('/')}`
}
