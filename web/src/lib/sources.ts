/**
 * Sources are free text by design: a URL, a file pointer, prose, or a
 * reference Trellis can check. Only two forms are checked, a wikilink and an
 * absolute address such as `/KEY/cards/KEY-12`, so these helpers read and
 * write exactly those.
 */

const ADDRESS = /^\/([A-Za-z][A-Za-z0-9-]*)\/(cards|vault|artifacts)\/(\S+)$/
const WIKILINK = /^\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|[^\]]*)?\]\]$/
const CARD_REF = /^([A-Za-z][A-Za-z0-9]*)-(\d+)$/

export type SourceTarget =
  | { kind: 'route'; to: string; label: string }
  | { kind: 'url'; href: string; label: string }
  | { kind: 'text' }

/** Where a source leads, if anywhere this app can open. */
export function sourceTarget(source: string, projectKey: string): SourceTarget {
  const text = source.trim()
  const link = WIKILINK.exec(text)
  if (link) {
    // A wikilink names an entry by slug, or by its whole address.
    const target = link[1].trim()
    const inside = ADDRESS.exec(target)
    if (inside) return { ...addressTarget(inside, projectKey), label: text }
    return { kind: 'route', to: `/p/${projectKey}/vault/${encodeURIComponent(target)}`, label: text }
  }
  const address = ADDRESS.exec(text)
  if (address) return { ...addressTarget(address, projectKey), label: text }
  if (/^https?:\/\/\S+$/i.test(text)) return { kind: 'url', href: text, label: text.replace(/^https?:\/\//i, '') }
  return { kind: 'text' }
}

/** Where an absolute address leads, but for the words shown. */
function addressTarget(address: RegExpExecArray, projectKey: string) {
  const [, key, collection, name] = address
  // The global vault is read from inside a project, so it opens in this one.
  const project = key === 'GLOBAL' ? projectKey : key
  if (collection === 'cards') return { kind: 'route' as const, to: `/p/${project}/card/${encodeURIComponent(name)}` }
  if (collection === 'vault') return { kind: 'route' as const, to: `/p/${project}/vault/${encodeURIComponent(name)}` }
  return { kind: 'url' as const, href: `/api/p/${project}/artifacts/${encodeURIComponent(name)}` }
}

/**
 * A source as it should be stored. A bare ref to a card of this project,
 * `KEY-12`, is how people name cards, but stored like that it is prose and
 * never checked; written as its address it is checked and opens the card.
 */
export function normalizeSource(source: string, projectKey: string) {
  const text = source.trim()
  const ref = CARD_REF.exec(text)
  if (ref && ref[1].toUpperCase() === projectKey.toUpperCase()) {
    return `/${projectKey}/cards/${projectKey}-${ref[2]}`
  }
  return text
}
