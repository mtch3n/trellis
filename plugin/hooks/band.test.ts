import type { On, RenderSurface } from 'claude-code'
import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'

const SURFACES = ['terminal', 'desktop'] as const

const PROMPT = {
  hasSurvey: false,
  isWorking: false,
  maxRows: 10,
  bodyColumns: 100,
  scroll: { offset: 0, bodyRows: 10 },
  view: {},
}

const DAEMON = 'http://127.0.0.1:7788/?token=T'

type World = {
  nominations: number
  unverified: number
  isServing: boolean
  cli: 'present' | 'missing' | 'failing'
}

// Stands for the engine beneath the plugin: the session and turn events, and
// $.process.run answered as the trellis CLI would, every argv recorded.
function fakeTrellis(on: On, world: World): string[][] {
  const ran: string[][] = []
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('turn.complete', (_$, e) => ({ text: e.answer }))
  on('ui.render', { component: 'AbovePrompt' }, ($, e) => $.ui.resolve(e).Box({ key: 'engine' }))
  on('process.run', async (_$, e) => {
    ran.push([...e.argv])
    if (e.argv[0] !== 'trellis' || world.cli === 'missing') {
      return { deny: `spawn ${e.argv[0]} ENOENT` }
    }
    if (world.cli === 'failing') {
      return { value: { exitCode: 1, stdout: '', stderr: 'error: database is locked' } }
    }
    const args = e.argv.slice(1).join(' ')
    const out = (reply: unknown) => ({ value: { exitCode: 0, stdout: JSON.stringify(reply), stderr: '' } })
    if (args === 'vault nominations --json') {
      return out({ nominations: Array.from({ length: world.nominations }, (_, i) => ({ slug: `n${i}` })) })
    }
    if (args === 'vault ls --unverified --json') {
      return out({ entries: Array.from({ length: world.unverified }, (_, i) => ({ slug: `u${i}` })) })
    }
    if (args === 'daemon status --json') {
      return out(world.isServing ? { running: true, url: DAEMON } : { running: false })
    }
    if (args === 'board show --json') {
      return out({ project: 'TRELLIS', board: 'trellis', columns: [] })
    }
    return { value: { exitCode: 2, stdout: '', stderr: `unknown command: ${args}` } }
  })
  return ran
}

function band($: Engine, surface: RenderSurface) {
  return $.ui.mount({ plugin: 'trellis', surface, component: 'AbovePrompt', props: PROMPT })
}

function startSession($: Engine, surface: RenderSurface) {
  return $.session.start({ cwd: '/work/repo', surface, isInteractive: true })
}

test('GATE-C15 one nomination reads "1 nomination" with a Review link; no daemon, no link', async ($, on) => {
  const world: World = { nominations: 1, unverified: 0, isServing: true, cli: 'present' }
  fakeTrellis(on, world)
  await startSession($, 'terminal')
  const served = await band($, 'terminal')
  expect((await served.find({ type: 'Text' }))?.text).toBe('Trellis: 1 nomination')
  const link = await served.find({ type: 'Link' })
  expect(link?.props.href).toBe('http://127.0.0.1:7788/p/TRELLIS/health?token=T')
  expect(link?.text).toBe('Review')

  const remote = await band($, 'desktop')
  expect((await remote.find({ type: 'Text' }))?.text).toBe('Trellis: 1 nomination')
  expect(await remote.find({ type: 'Link' })).toBeUndefined()

  world.isServing = false
  await startSession($, 'terminal')
  const unserved = await band($, 'terminal')
  expect((await unserved.find({ type: 'Text' }))?.text).toBe('Trellis: 1 nomination')
  expect(await unserved.find({ type: 'Link' })).toBeUndefined()
})

test('GATE-C16 nothing waiting, a missing CLI or a failing CLI draws nothing and toasts nothing', async ($, on) => {
  const world: World = { nominations: 0, unverified: 0, isServing: true, cli: 'present' }
  fakeTrellis(on, world)
  const toasts: string[] = []
  on('ui.toast', (_$, e) => {
    toasts.push(e.text)
  })
  const worlds: Partial<World>[] = [{ cli: 'present' }, { cli: 'missing' }, { cli: 'failing' }]
  for (const variant of worlds) {
    Object.assign(world, variant)
    for (const surface of SURFACES) {
      await startSession($, surface)
      const drawn = await band($, surface)
      expect(await drawn.find({ key: 'engine' })).toBeDefined()
      expect(await drawn.find({ type: 'Text' })).toBeUndefined()
    }
  }
  expect(toasts).toEqual([])
})

test('GATE-C17 a nomination added during a turn shows once the turn completes', async ($, on) => {
  const world: World = { nominations: 1, unverified: 2, isServing: false, cli: 'present' }
  fakeTrellis(on, world)
  await startSession($, 'terminal')
  const drawn = await band($, 'terminal')
  expect((await drawn.find({ type: 'Text' }))?.text).toBe('Trellis: 1 nomination · 2 unverified')

  world.nominations = 2
  await $.turn.complete({ answer: 'done', durationMs: 10, isAborted: false, turnId: 't1', reason: 'answer' })
  expect((await drawn.find({ type: 'Text' }))?.text).toBe('Trellis: 2 nominations · 2 unverified')
})
