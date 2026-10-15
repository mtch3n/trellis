import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  activityView,
  CREATE_OVERVIEW,
  declaredView,
  descriptionLine,
  NO_ACTIVITY,
} from '../src/lib/overview.ts'

test('PPL-C3 a project with no events in 30 days says no activity in 30 days', () => {
  assert.deepEqual(activityView({ actors: [], sessions: 0, session_events: 0 }), { empty: NO_ACTIVITY })
  assert.match(NO_ACTIVITY, /no activity in the last 30 days/i)
})

test('PPL-C5 an overview entry renders its body with a link to the entry', () => {
  const view = declaredView('TRELLIS', { slug: 'overview', body: 'Owned by **a**.' })
  assert.deepEqual(view, { body: 'Owned by **a**.', href: '/p/TRELLIS/vault/overview' })
})

test('PPL-C6 no overview entry shows the create command and an empty description shows no line', () => {
  assert.deepEqual(declaredView('TRELLIS', null), { command: CREATE_OVERVIEW })
  assert.equal(CREATE_OVERVIEW, 'trellis vault new --title Overview')
  assert.equal(descriptionLine(''), undefined)
  assert.equal(descriptionLine('   '), undefined)
})

test('PPL-C7 a description is shown under the title', () => {
  assert.equal(descriptionLine('Agent-driven kanban and vault.'), 'Agent-driven kanban and vault.')
})
