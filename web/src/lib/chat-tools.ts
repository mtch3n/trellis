import { tool } from 'ai'
import { z } from 'zod'
import { readError } from '@/lib/api'

/** How long a card or entry body may run before a listing cuts it short. */
const PREVIEW = 280

function preview(text: unknown) {
  if (typeof text !== 'string') return text
  return text.length > PREVIEW ? `${text.slice(0, PREVIEW)}…` : text
}

/**
 * The board and vault as tools for a model the browser drives. Each calls the
 * same API the page does. A write carries the provider in X-Trellis-Chat, so
 * the event log records the chat agent as its actor, not the person here.
 */
export function boardTools(projectKey: string, providerId: string) {
  const base = `/api/p/${encodeURIComponent(projectKey)}`
  const call = async (path: string, init?: RequestInit) => {
    const response = await fetch(path, {
      ...init,
      headers: { 'Content-Type': 'application/json', 'X-Trellis-Chat': providerId, ...init?.headers },
    })
    if (!response.ok) throw new Error(await readError(response))
    return response.status === 204 ? { ok: true } : response.json()
  }
  const write = (path: string, method: string, body: unknown) =>
    call(path, { method, body: JSON.stringify(body) })
  const board = (slug: string) => `${base}/b/${encodeURIComponent(slug)}`
  const card = (slug: string, ref: string) => `${board(slug)}/cards/${encodeURIComponent(ref)}`

  return {
    list_boards: tool({
      description: 'List the boards in this project, with their slugs and columns.',
      inputSchema: z.object({}),
      execute: () => call(`${base}/boards`),
    }),
    list_cards: tool({
      description: "List a board's cards, column by column. Bodies are cut short; use show_card for the whole card.",
      inputSchema: z.object({
        board: z.string().describe('The board slug, from list_boards.'),
        archived: z.boolean().optional().describe('List archived cards instead of live ones.'),
      }),
      execute: async ({ board: slug, archived }) => {
        const columns = await call(`${board(slug)}/cards${archived ? '?archived=1' : ''}`) as { name: string; cards: Record<string, unknown>[] }[]
        return columns.map((column) => ({
          ...column,
          cards: column.cards.map((item) => ({ ...item, body: preview(item.body) })),
        }))
      },
    }),
    show_card: tool({
      description: 'Read one card in full: body, comments, events, claim, links.',
      inputSchema: z.object({ ref: z.string().describe(`The card ref, such as ${projectKey}-12.`) }),
      execute: ({ ref }) => call(`${base}/cards/${encodeURIComponent(ref)}`),
    }),
    search: tool({
      description: 'Search the cards and vault entries of this project.',
      inputSchema: z.object({
        query: z.string(),
        limit: z.number().int().min(1).max(50).optional(),
      }),
      execute: ({ query, limit }) => {
        const params = new URLSearchParams({ q: query, project: projectKey, limit: String(limit ?? 10) })
        return call(`/api/search?${params}`)
      },
    }),
    list_entries: tool({
      description: "List the project vault's entries, with slug, title and summary.",
      inputSchema: z.object({}),
      execute: () => call(`${base}/vault`),
    }),
    read_entry: tool({
      description: 'Read one vault entry in full.',
      inputSchema: z.object({ slug: z.string().describe('The entry slug, such as deployment/rollback.') }),
      execute: ({ slug }) => call(`${base}/vault/${encodeURIComponent(slug)}`),
    }),
    create_card: tool({
      description: 'Create a card on a board.',
      inputSchema: z.object({
        board: z.string(),
        title: z.string(),
        body: z.string().optional().describe('Markdown.'),
        column: z.string().optional().describe('Defaults to the first column.'),
        priority: z.enum(['urgent', 'high', 'normal', 'low']).optional(),
      }),
      execute: ({ board: slug, ...fields }) => write(`${board(slug)}/cards`, 'POST', fields),
    }),
    edit_card: tool({
      description: "Change a card's title, body or priority.",
      inputSchema: z.object({
        board: z.string(),
        ref: z.string(),
        title: z.string().optional(),
        body: z.string().optional().describe('Replaces the whole body. Markdown.'),
        priority: z.enum(['urgent', 'high', 'normal', 'low']).optional(),
      }),
      execute: ({ board: slug, ref, ...fields }) => write(card(slug, ref), 'PATCH', fields),
    }),
    move_card: tool({
      description: 'Move a card to another column.',
      inputSchema: z.object({ board: z.string(), ref: z.string(), column: z.string() }),
      execute: ({ board: slug, ref, column }) => write(`${card(slug, ref)}/move`, 'POST', { column }),
    }),
    comment_on_card: tool({
      description: 'Add a comment to a card.',
      inputSchema: z.object({ board: z.string(), ref: z.string(), body: z.string().describe('Markdown.') }),
      execute: ({ board: slug, ref, body }) => write(`${card(slug, ref)}/comments`, 'POST', { body }),
    }),
  }
}

export type BoardTools = ReturnType<typeof boardTools>

/** The instructions an API model reads before the conversation. */
export function instructions(projectKey: string) {
  return `You are the assistant inside the Trellis web dashboard, talking with the person who owns project ${projectKey}.
Trellis is a kanban board and a vault of markdown entries that AI agents write while they work. The person reads it to learn what is happening.
Use the tools to look things up before answering; never guess a card's state or an entry's content. Change cards only when asked to.
Answer in concise Markdown. Cite cards by ref (${projectKey}-12) and entries by slug.`
}
