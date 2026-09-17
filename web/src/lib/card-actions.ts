/**
 * Every write a card supports, in one place. The board and the card page both
 * offer the same actions, and each used to carry its own copy of the fetch,
 * the toast and the reload; they now share this.
 *
 * Each action reports what happened where the caller has a decision to make
 * (a save that hit a conflict, a claim someone else holds) and otherwise
 * resolves once the change is in and the caller's `refresh` has run.
 */
import { toast } from '@/components/ui/toast'
import { readError, readRefusal, refusalText, whoClaims } from '@/lib/api'
import { PRIORITIES, PRIORITY_NUMBERS } from '@/lib/cards'

/** One label or tag added or removed. */
export interface ChipChange {
  add?: string
  remove?: string
}

export interface CardActions {
  save: (card: { ref: string; title: string; body: string; version: number }, edit: { title: string; body: string }) => Promise<boolean>
  move: (ref: string, column: string, before?: string) => Promise<void>
  priority: (ref: string, priority: string) => Promise<void>
  chip: (ref: string, field: 'labels' | 'tags', change: ChipChange) => Promise<void>
  comment: (ref: string, body: string) => Promise<boolean>
  relate: (ref: string, relation: { rel: string; ref: string }) => Promise<boolean>
  unrelate: (ref: string, relation: { rel: string; ref: string }) => Promise<void>
  claim: (ref: string) => Promise<void>
  release: (ref: string) => Promise<void>
  steal: (ref: string, reason: string) => Promise<void>
  archive: (ref: string, archived: boolean) => Promise<boolean>
  remove: (ref: string) => Promise<boolean>
  link: (ref: string, target: string) => Promise<boolean>
  unlink: (ref: string, target: string) => Promise<void>
  upload: (ref: string, file: File) => Promise<boolean>
  linkArtifact: (ref: string, name: string) => Promise<boolean>
  unlinkArtifact: (ref: string, name: string) => Promise<void>
  importCards: (cards: unknown[]) => Promise<boolean>
}

function message(err: unknown) {
  return err instanceof Error ? err.message : 'Unknown error'
}

const json = { 'Content-Type': 'application/json' }

/**
 * Builds the actions. `base` is the board's API path
 * (`/api/p/KEY/b/board`), and `refresh` brings the caller's own view up to
 * date once a write lands — the board reloads its columns, the card page its
 * detail.
 */
export function cardActions({ base, refresh }: {
  base: string
  refresh: (ref: string) => Promise<void>
}): CardActions {
  const card = (ref: string, suffix = '') => `${base}/cards/${encodeURIComponent(ref)}${suffix}`

  /** A write with nothing to decide: it lands, or it says why and changes nothing. */
  const write = async (
    ref: string,
    request: () => Promise<Response>,
    failed: string,
  ): Promise<boolean> => {
    try {
      const response = await request()
      if (!response.ok) throw new Error(await readError(response))
      return true
    } catch (err) {
      toast.add({ title: failed, description: message(err), type: 'error' })
      return false
    } finally {
      await refresh(ref)
    }
  }

  return {
    // Only what changed is sent, so the timeline records edits, not saves. A
    // conflict means the card moved on: the caller's view is reloaded and the
    // edit stays open.
    save: async (current, edit) => {
      const changes: { title?: string; body?: string } = {}
      if (edit.title !== current.title) changes.title = edit.title
      if (edit.body !== current.body) changes.body = edit.body
      if (Object.keys(changes).length === 0) {
        await refresh(current.ref)
        return true
      }
      try {
        const response = await fetch(card(current.ref), {
          method: 'PATCH',
          headers: json,
          body: JSON.stringify({ ...changes, if_version: current.version }),
        })
        if (!response.ok) {
          const text = await readError(response)
          await refresh(current.ref)
          toast.add({ title: 'Card changed underneath you', description: `${text} It has been reloaded.`, type: 'error' })
          return false
        }
        await refresh(current.ref)
        return true
      } catch (err) {
        toast.add({ title: 'Could not save card', description: message(err), type: 'error' })
        return false
      }
    },

    move: async (ref, column, before = '') => {
      await write(ref, () => fetch(card(ref, '/move'), {
        method: 'POST', headers: json, body: JSON.stringify({ column, before }),
      }), `Could not move ${ref}`)
    },

    // A single field, so no version: the server asks for one only when a
    // title or body is replaced wholesale.
    priority: async (ref, priority) => {
      await write(ref, () => fetch(card(ref), {
        method: 'PATCH',
        headers: json,
        body: JSON.stringify({ priority: PRIORITY_NUMBERS[priority as (typeof PRIORITIES)[number]] }),
      }), `Could not change the priority of ${ref}`)
    },

    chip: async (ref, field, change) => {
      const patch = change.add ? { [`add_${field}`]: [change.add] } : { [`remove_${field}`]: [change.remove] }
      await write(ref, () => fetch(card(ref), { method: 'PATCH', headers: json, body: JSON.stringify(patch) }),
        `Could not change the ${field} of ${ref}`)
    },

    comment: (ref, body) => write(ref, () => fetch(card(ref, '/comments'), {
      method: 'POST', headers: json, body: JSON.stringify({ body }),
    }), `Could not comment on ${ref}`),

    relate: (ref, relation) => write(ref, () => fetch(card(ref, '/relations'), {
      method: 'POST', headers: json, body: JSON.stringify(relation),
    }), `Could not relate ${ref} to ${relation.ref}`),

    unrelate: async (ref, relation) => {
      await write(ref, () => fetch(
        card(ref, `/relations/${encodeURIComponent(relation.rel)}/${encodeURIComponent(relation.ref)}`),
        { method: 'DELETE', headers: json, body: '{}' },
      ), `Could not remove the relation to ${relation.ref}`)
    },

    // Claiming a card someone else holds is refused with who holds it, so the
    // refusal says the one thing the reader needs rather than "409".
    claim: async (ref) => {
      try {
        const response = await fetch(card(ref, '/claim'), { method: 'POST', headers: json, body: '{}' })
        if (!response.ok) {
          const refusal = await readRefusal(response)
          const claimant = whoClaims(refusal)
          toast.add({
            title: claimant ? `${ref} is claimed by ${claimant.handle}` : `Could not claim ${ref}`,
            description: claimant
              ? `Last seen ${claimant.seen}. Steal the claim if the work has stopped.`
              : refusalText(refusal),
            type: 'error',
          })
        }
      } catch (err) {
        toast.add({ title: `Could not claim ${ref}`, description: message(err), type: 'error' })
      } finally {
        await refresh(ref)
      }
    },

    release: async (ref) => {
      await write(ref, () => fetch(card(ref, '/release'), { method: 'POST', headers: json, body: '{}' }),
        `Could not release ${ref}`)
    },

    steal: async (ref, reason) => {
      await write(ref, () => fetch(card(ref, '/steal'), {
        method: 'POST', headers: json, body: JSON.stringify({ reason }),
      }), 'Could not steal the claim')
    },

    archive: async (ref, archived) => {
      const done = await write(ref, () => fetch(card(ref, archived ? '/archive' : '/restore'), {
        method: 'POST', headers: json, body: '{}',
      }), archived ? `Could not archive ${ref}` : `Could not restore ${ref}`)
      if (done) toast.add({ title: archived ? `Archived ${ref}` : `Restored ${ref}`, type: 'success' })
      return done
    },

    remove: async (ref) => {
      try {
        const response = await fetch(card(ref), { method: 'DELETE', headers: json, body: '{}' })
        if (!response.ok) throw new Error(await readError(response))
        toast.add({ title: `Deleted ${ref}`, type: 'success' })
        return true
      } catch (err) {
        toast.add({ title: `Could not delete ${ref}`, description: message(err), type: 'error' })
        return false
      }
    },

    link: (ref, target) => write(ref, () => fetch(card(ref, '/links'), {
      method: 'POST', headers: json, body: JSON.stringify({ target }),
    }), `Could not link ${ref} to ${target}`),

    unlink: async (ref, target) => {
      await write(ref, () => fetch(card(ref, '/links'), {
        method: 'DELETE', headers: json, body: JSON.stringify({ target }),
      }), `Could not unlink ${target}`)
    },

    // An artifact is a file, so this one request is multipart. The card rides
    // along in the form, so the upload is linked in the same request.
    upload: async (ref, file) => {
      const form = new FormData()
      form.append('file', file)
      form.append('card', ref)
      const done = await write(ref, () => fetch(`${base.replace(/\/b\/[^/]+$/, '')}/artifacts`, {
        method: 'POST', body: form,
      }), `Could not upload ${file.name}`)
      if (done) toast.add({ title: `Added ${file.name}`, type: 'success' })
      return done
    },

    linkArtifact: (ref, name) => write(ref, () => fetch(card(ref, '/artifacts'), {
      method: 'POST', headers: json, body: JSON.stringify({ name }),
    }), `Could not link ${name}`),

    unlinkArtifact: async (ref, name) => {
      await write(ref, () => fetch(card(ref, `/artifacts/${encodeURIComponent(name)}`), {
        method: 'DELETE', headers: json, body: '{}',
      }), `Could not remove ${name}`)
    },

    // A plan lands whole or not at all, so a refusal leaves the board alone.
    importCards: async (cards) => {
      try {
        const response = await fetch(`${base}/cards/import`, {
          method: 'POST', headers: json, body: JSON.stringify(cards),
        })
        if (!response.ok) throw new Error(await readError(response))
        const written = (await response.json()) as unknown[]
        toast.add({ title: `Imported ${written.length} ${written.length === 1 ? 'card' : 'cards'}`, type: 'success' })
        return true
      } catch (err) {
        toast.add({ title: 'Could not import the cards', description: message(err), type: 'error' })
        return false
      } finally {
        await refresh('')
      }
    },
  }
}
