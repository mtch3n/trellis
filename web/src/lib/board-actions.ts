/**
 * Every write a board's own shape supports: the board, its columns, and the
 * project's labels. Cards live in `card-actions`; this is the furniture they
 * sit in.
 *
 * Each action answers with the server's refusal rather than a toast, because
 * every one of them is started from a form or a menu that has to show why it
 * was refused — a column with cards in it, a label another card still uses.
 */
import { readRefusal, type Refusal } from '@/lib/api'

export interface BoardActions {
  createBoard: (name: string, seedColumns: boolean) => Promise<Refusal | null>
  renameBoard: (name: string) => Promise<Refusal | null>
  setDefaultBoard: () => Promise<Refusal | null>
  deleteBoard: (force: boolean) => Promise<Refusal | null>
  addColumn: (name: string, after: string, done: boolean) => Promise<Refusal | null>
  renameColumn: (column: string, name: string) => Promise<Refusal | null>
  /** Puts a column after another; "" makes it the first. */
  moveColumn: (column: string, after: string) => Promise<Refusal | null>
  deleteColumn: (column: string, moveCardsTo: string) => Promise<Refusal | null>
  createLabel: (name: string, description: string) => Promise<Refusal | null>
  deleteLabel: (name: string) => Promise<Refusal | null>
  mergeLabels: (from: string, into: string) => Promise<Refusal | null>
}

const json = { 'Content-Type': 'application/json' }

/**
 * Builds the actions. `projectKey` and `boardSlug` place the routes; the slug
 * survives a rename, since a `.trellis` marker names it. `refresh` brings the
 * caller up to date once a write lands.
 */
export function boardActions({ projectKey, boardSlug, refresh }: {
  projectKey: string
  boardSlug: string
  refresh: () => Promise<void>
}): BoardActions {
  const project = `/api/p/${projectKey}`
  const board = `${project}/b/${boardSlug}`

  const act = async (request: () => Promise<Response>): Promise<Refusal | null> => {
    try {
      const response = await request()
      if (!response.ok) return await readRefusal(response)
      await refresh()
      return null
    } catch (err) {
      return { message: err instanceof Error ? err.message : 'Could not reach the daemon.', problems: [] }
    }
  }

  const patchBoard = (change: unknown) => act(() => fetch(board, { method: 'PATCH', headers: json, body: JSON.stringify(change) }))
  const column = (name: string) => `${board}/columns/${encodeURIComponent(name)}`

  return {
    createBoard: (name, seedColumns) => act(() => fetch(`${project}/boards`, {
      method: 'POST', headers: json, body: JSON.stringify({ name, no_columns: !seedColumns }),
    })),
    renameBoard: (name) => patchBoard({ name }),
    setDefaultBoard: () => patchBoard({ is_default: true }),
    deleteBoard: (force) => act(() => fetch(`${board}${force ? '?force=1' : ''}`, {
      method: 'DELETE', headers: json, body: '{}',
    })),

    addColumn: (name, after, done) => act(() => fetch(`${board}/columns`, {
      method: 'POST', headers: json, body: JSON.stringify({ name, after, done }),
    })),
    renameColumn: (name, to) => act(() => fetch(column(name), {
      method: 'PATCH', headers: json, body: JSON.stringify({ name: to }),
    })),
    moveColumn: (name, after) => act(() => fetch(column(name), {
      method: 'PATCH', headers: json, body: JSON.stringify({ after }),
    })),
    deleteColumn: (name, moveCardsTo) => act(() => fetch(
      `${column(name)}${moveCardsTo ? `?move_cards_to=${encodeURIComponent(moveCardsTo)}` : ''}`,
      { method: 'DELETE', headers: json, body: '{}' },
    )),

    createLabel: (name, description) => act(() => fetch(`${project}/labels`, {
      method: 'POST', headers: json, body: JSON.stringify({ name, description }),
    })),
    deleteLabel: (name) => act(() => fetch(`${project}/labels/${encodeURIComponent(name)}`, {
      method: 'DELETE', headers: json, body: '{}',
    })),
    mergeLabels: (from, into) => act(() => fetch(`${project}/labels/merge`, {
      method: 'POST', headers: json, body: JSON.stringify({ from, into }),
    })),
  }
}
