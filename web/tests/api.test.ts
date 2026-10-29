import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api, ApiError, boardColumns, loginPath, setCsrf, vaultPath, type Card } from '../src/lib/api.ts'

const realFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = realFetch
})

function capture(status: number, body: unknown) {
  const calls: { url: string; init: RequestInit }[] = []
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    calls.push({ url, init })
    return new Response(body === undefined ? null : JSON.stringify(body), { status })
  }) as typeof fetch
  return calls
}

function header(init: RequestInit, name: string) {
  return (init.headers as Record<string, string>)[name]
}

test('a write sends the CSRF header that /api/me gave', async () => {
  setCsrf('csrf-1')
  const calls = capture(201, { card: { ref: 'APP-1' } })
  const card = await api.newCard('APP', 'ann', 'Write the login page', '')
  assert.equal(card.ref, 'APP-1')
  assert.equal(calls[0].url, '/api/p/APP/b/ann/cards')
  assert.equal(calls[0].init.method, 'POST')
  assert.equal(header(calls[0].init, 'X-Trellis-CSRF'), 'csrf-1')
})

test('a read sends no CSRF header', async () => {
  setCsrf('csrf-1')
  const calls = capture(200, { entries: [] })
  await api.entries('APP')
  assert.equal(calls[0].init.method, 'GET')
  assert.equal(header(calls[0].init, 'X-Trellis-CSRF'), undefined)
})

test('a slug with slashes is one path segment on the API', async () => {
  const calls = capture(200, { entry: { slug: 'specs/web/web' } })
  await api.entry('APP', 'specs/web/web')
  assert.equal(calls[0].url, '/api/p/APP/vault/specs%2Fweb%2Fweb')
  assert.equal(vaultPath('APP', 'specs/web/web'), '/p/APP/vault/specs/web/web')
})

test('promote sends its reason to the review route', async () => {
  const calls = capture(200, { slug: 'notes', scope: 'global', labels: [] })
  const result = await api.promote('APP', 'notes', 'every team needs it')
  assert.equal(result.scope, 'global')
  assert.equal(calls[0].url, '/api/p/APP/vault/notes/promote')
  assert.deepEqual(JSON.parse(calls[0].init.body as string), { reason: 'every team needs it' })
})

test('an error answer becomes an ApiError with the code, kind and fix', async () => {
  capture(403, { code: 'not_owner', kind: 'policy', message: 'only the owner promotes it', fix: 'nominate it' })
  await assert.rejects(api.promote('APP', 'notes', 'why'), (err: unknown) => {
    assert.ok(err instanceof ApiError)
    assert.equal(err.status, 403)
    assert.equal(err.code, 'not_owner')
    assert.equal(err.kind, 'policy')
    assert.equal(err.fix, 'nominate it')
    return true
  })
})

test('a board shows the default columns first, then any other column its cards use', () => {
  const cards = [{ column: 'blocked' }, { column: 'done' }] as Card[]
  assert.deepEqual(boardColumns(cards), ['backlog', 'in-progress', 'review', 'done', 'blocked'])
})

test('the login page returns to the page that needed a session', () => {
  assert.equal(loginPath('/p/APP/vault'), '/login?next=%2Fp%2FAPP%2Fvault')
  assert.equal(loginPath('/'), '/login')
  assert.equal(loginPath('/login?next=x'), '/login')
})
