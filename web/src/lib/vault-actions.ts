/**
 * Every write an entry supports, in one place, the way `card-actions` holds a
 * card's. A change to an entry's facts applies at once and carries the version
 * on screen; the reload that follows advances it, so an edit already open
 * saves over the new version without a false conflict.
 */
import { toast } from '@/components/ui/toast'
import { readRefusal, refusalText, type Refusal } from '@/lib/api'

/**
 * A change to one entry's facts, as PATCH takes it: the fields it names are
 * replaced, and what it leaves out keeps its value.
 */
export interface EntryPatch {
  title?: string
  summary?: string
  body?: string
  template?: string
  private?: boolean
  tags?: string[]
  labels?: string[]
  board?: string
  sources?: string[]
  set?: Record<string, string>
}

export interface VaultActions {
  /** Applies a change to the entry's facts. Resolves null on success, or the refusal. */
  patch: (slug: string, version: number, change: EntryPatch, done: string) => Promise<Refusal | null>
  remove: (slug: string) => Promise<boolean>
  pin: (slug: string, recap: string) => Promise<Refusal | null>
  unpin: (slug: string) => Promise<boolean>
  promote: (slug: string, confirm: string, reason: string) => Promise<Refusal | null>
  demote: (slug: string, confirm: string, reason: string) => Promise<Refusal | null>
  verify: (slug: string, confirm: string) => Promise<Refusal | null>
}

const json = { 'Content-Type': 'application/json' }

function unreachable(err: unknown): Refusal {
  return { message: err instanceof Error ? err.message : 'Could not reach the daemon.', problems: [] }
}

/** A template that only warns lets the save through and says why. */
function warningText(warnings: string[]) {
  return warnings.map((warning) => `${warning.trim().replace(/\.$/, '')}.`).join(' ')
}

/**
 * Builds the actions. `projectKey` and `board` place the routes; `refresh`
 * brings the page's list and open entry up to date once a write lands.
 */
export function vaultActions({ projectKey, board, refresh }: {
  projectKey: string
  /** The board the entry routes hang off. Writes wait until it is known. */
  board: string | null
  refresh: () => Promise<void>
}): VaultActions {
  const entry = (slug: string, suffix = '') =>
    `/api/p/${projectKey}/b/${board}/vault/${encodeURIComponent(slug)}${suffix}`
  const projectEntry = (slug: string, suffix = '') =>
    `/api/p/${projectKey}/vault/${encodeURIComponent(slug)}${suffix}`
  const notLoaded: Refusal = { message: 'The vault is not loaded yet.', problems: [] }

  /** A lifecycle act: it lands, or it says why and nothing changed. */
  const act = async (request: () => Promise<Response>, done: string): Promise<Refusal | null> => {
    try {
      const response = await request()
      if (!response.ok) return await readRefusal(response)
      await refresh()
      toast.add({ title: done, type: 'success' })
      return null
    } catch (err) {
      return unreachable(err)
    }
  }

  return {
    patch: async (slug, version, change, done) => {
      if (!board) return notLoaded
      try {
        const response = await fetch(entry(slug), {
          method: 'PATCH',
          headers: json,
          body: JSON.stringify({ ...change, version }),
        })
        if (!response.ok) return await readRefusal(response)
        const saved = (await response.json()) as { warnings?: string[] }
        await refresh()
        if (saved.warnings?.length) {
          toast.add({ title: `${done}, with warnings`, description: warningText(saved.warnings), type: 'warning' })
        } else {
          toast.add({ title: done, type: 'success' })
        }
        return null
      } catch (err) {
        return unreachable(err)
      }
    },

    remove: async (slug) => {
      if (!board) return false
      try {
        const response = await fetch(entry(slug), { method: 'DELETE', headers: json, body: '{}' })
        if (!response.ok) throw new Error(refusalText(await readRefusal(response)))
        toast.add({ title: `Deleted ${slug}`, type: 'success' })
        await refresh()
        return true
      } catch (err) {
        toast.add({ title: `Could not delete ${slug}`, description: unreachable(err).message, type: 'error' })
        return false
      }
    },

    // A pin carries the recap a session reads before the body. Trellis never
    // writes one: with none given the entry's summary stands in.
    pin: (slug, recap) => act(
      () => fetch(projectEntry(slug, '/pin'), { method: 'POST', headers: json, body: JSON.stringify({ recap }) }),
      `Pinned ${slug}`,
    ),

    unpin: async (slug) => {
      const refusal = await act(
        () => fetch(projectEntry(slug, '/pin'), { method: 'DELETE', headers: json, body: '{}' }),
        `Unpinned ${slug}`,
      )
      if (refusal) toast.add({ title: `Could not unpin ${slug}`, description: refusalText(refusal), type: 'error' })
      return refusal === null
    },

    promote: (slug, confirm, reason) => act(
      () => fetch(projectEntry(slug, '/promote'), { method: 'POST', headers: json, body: JSON.stringify({ confirm, reason }) }),
      `Promoted ${slug} to the global vault`,
    ),

    demote: (slug, confirm, reason) => act(
      () => fetch(`/api/global/vault/${encodeURIComponent(slug)}/demote`, {
        method: 'POST', headers: json, body: JSON.stringify({ confirm, reason }),
      }),
      `Demoted ${slug} to its project`,
    ),

    verify: (slug, confirm) => act(
      () => fetch(`/api/global/vault/${encodeURIComponent(slug)}/verify`, {
        method: 'POST', headers: json, body: JSON.stringify({ confirm }),
      }),
      `${slug} verified`,
    ),
  }
}
