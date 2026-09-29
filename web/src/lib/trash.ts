import { readError } from '@/lib/api'

/** One trashed thing, as `/api/p/{key}/trash` and `/api/trash/projects` list it. */
export interface TrashItem {
  /** Empty for a project: a project is restored by its key. */
  id: string
  kind: 'card' | 'entry' | 'artifact' | 'board' | 'project'
  name: string
  title?: string
  trashed_at: number
  trashed_by?: string
  /** When the purge takes it, given the retention now configured. */
  purge_at: number
}

/** Reads a list of trashed items, throwing the server's own words on failure. */
export async function readTrash(url: string, signal?: AbortSignal): Promise<TrashItem[]> {
  const response = await fetch(url, { signal })
  if (!response.ok) throw new Error(await readError(response))
  return ((await response.json()) as TrashItem[] | null) ?? []
}

/** Restores one item; a project by its key, anything else by its trash id. */
export async function restoreTrash(projectKey: string | null, item: TrashItem) {
  const url = item.kind === 'project'
    ? `/api/trash/projects/${encodeURIComponent(item.name)}/restore`
    : `/api/p/${projectKey}/trash/${encodeURIComponent(item.id)}/restore`
  const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' } })
  if (!response.ok) throw new Error(await readError(response))
}
