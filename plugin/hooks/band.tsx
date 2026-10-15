import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Gate } from '../types'

const gate = atom({ plugin: 'trellis', key: 'gate' } as const, null)

type Reply = Record<string, unknown>

async function trellis($: EngineInterface, args: string[]): Promise<Reply | null> {
  try {
    const { exitCode, stdout } = await $.process.run(['trellis', ...args, '--json'])
    return exitCode === 0 ? (JSON.parse(stdout) as Reply) : null
  } catch {
    return null
  }
}

function count(reply: Reply | null, field: string): number | null {
  const list = reply?.[field]
  return Array.isArray(list) ? list.length : null
}

async function reviewPage($: EngineInterface): Promise<string | null> {
  const daemon = await trellis($, ['daemon', 'status'])
  if (daemon?.running !== true || typeof daemon.url !== 'string' || daemon.url === '') {
    return null
  }
  const board = await trellis($, ['board', 'show'])
  if (typeof board?.project !== 'string') {
    return null
  }
  // The daemon's URL carries the UI's token as its query, which the page needs.
  const page = new URL(daemon.url)
  page.pathname = `/p/${encodeURIComponent(board.project)}/health`
  return page.toString()
}

async function readGate($: EngineInterface): Promise<Gate | null> {
  const nominations = count(await trellis($, ['vault', 'nominations']), 'nominations')
  const unverified = count(await trellis($, ['vault', 'ls', '--unverified']), 'entries')
  if (nominations === null || unverified === null || nominations + unverified === 0) {
    return null
  }
  return { nominations, unverified, review: await reviewPage($) }
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
    // A remote surface draws only an https: link, and the daemon serves plain
    // http on this machine, so only the terminal can open the review page.
    const review = e.surface === 'terminal' ? waiting.review : null

    return (
      <Box gap={1}>
        <Text>{summary(waiting)}</Text>
        {review === null ? null : <Link href={review} label="Review" />}
      </Box>
    )
  })
}
