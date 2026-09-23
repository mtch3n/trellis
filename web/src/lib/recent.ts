/**
 * The entries this browser opened last, per project, newest first. Kept in
 * the browser because it is one reader's trail, not a fact about the vault.
 */
const LIMIT = 8

const key = (projectKey: string) => `trellis.vault-opened.${projectKey}`

export interface Opened {
  slug: string
  at: number
}

export function recentlyOpened(projectKey: string): Opened[] {
  try {
    const raw = localStorage.getItem(key(projectKey))
    return raw ? (JSON.parse(raw) as Opened[]) : []
  } catch {
    return []
  }
}

export function rememberOpened(projectKey: string, slug: string) {
  const next = [{ slug, at: Date.now() }, ...recentlyOpened(projectKey).filter((item) => item.slug !== slug)].slice(0, LIMIT)
  try { localStorage.setItem(key(projectKey), JSON.stringify(next)) } catch { /* private mode */ }
}
