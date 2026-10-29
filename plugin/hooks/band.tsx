import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Gate } from '../types'

const gate = atom({ plugin: 'trellis', key: 'gate' } as const, null)

type Reply = Record<string, unknown>

async function call($: EngineInterface, tool: string, args: Reply): Promise<Reply | null> {
  try {
    const { exitCode, stdout } = await $.process.run(['trellis', 'call', tool, JSON.stringify(args)])
    return exitCode === 0 ? (JSON.parse(stdout) as Reply) : null
  } catch {
    return null
  }
}

function list(reply: Reply | null, field: string): unknown[] | null {
  const items = reply?.[field]
  return Array.isArray(items) ? items : null
}

async function reviewPage($: EngineInterface): Promise<string | null> {
  const server = await $.env.get('TRELLIS_URL')
  if (server === undefined || server === '') {
    return null
  }
  const brief = await call($, 'board_brief', {})
  if (typeof brief?.project !== 'string' || brief.project === '') {
    return null
  }
  try {
    const page = new URL(server)
    page.pathname = `${page.pathname.replace(/\/$/, '')}/p/${encodeURIComponent(brief.project)}/review`
    return page.toString()
  } catch {
    return null
  }
}

async function readGate($: EngineInterface): Promise<Gate | null> {
  const nominations = list(await call($, 'vault_check', { action: 'nominations' }), 'nominations')
  const unverified = list(await call($, 'vault_list', { unverified: true }), 'entries')
  if (nominations === null || unverified === null || nominations.length + unverified.length === 0) {
    return null
  }
  return { nominations: nominations.length, unverified: unverified.length, review: await reviewPage($) }
}

async function refresh($: EngineInterface): Promise<void> {
  const next = await readGate($)
  await update($, gate, () => next)
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

function summary({ nominations, unverified }: Gate): string {
  const parts = []
  if (nominations > 0) parts.push(plural(nominations, 'nomination'))
  if (unverified > 0) parts.push(`${unverified} unverified`)
  return `Trellis: ${parts.join(' · ')}`
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    await refresh($)
    return started
  })

  on('turn.complete', async ($, e, next) => {
    const completed = await next(e)
    await refresh($)
    return completed
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const waiting = await read($, gate)
    if (e.props.hasSurvey || waiting === null) {
      return next(e)
    }

    const { Box, Link, Text } = $.ui.resolve(e)
    const review =
      waiting.review !== null && (e.surface === 'terminal' || waiting.review.startsWith('https:')) ? waiting.review : null

    return (
      <Box gap={1}>
        <Text>{summary(waiting)}</Text>
        {review === null ? null : <Link href={review} label="Review" />}
      </Box>
    )
  })
}
