/**
 * The vault graph, built from the links the server resolved
 * (`GET /api/p/{key}/links/vault`), so the picture is exactly what the
 * backend stored and no body has to travel to draw it. Entries and links meet
 * on the entry's address (`/KEY/vault/<slug>`). A link whose target no
 * entry answers is a stub, labelled with what its author wrote. Links are
 * drawn undirected, because "what is this connected to" is the question a
 * reader asks.
 */

/** One resolved wikilink, as the links route returns it. */
export interface EntryLink {
  from: string
  /** The target's address, or null for a stub. */
  to: string | null
  /** The text inside the brackets, anchor included. */
  raw: string
  anchor: string
}

export interface GraphSource {
  id: string
  /** The entry's address, which links name it by. */
  ref: string
  slug: string
  title: string
  template?: string
  global?: boolean
}

export interface GraphNode {
  id: string
  slug: string
  title: string
  /** The entry's template, or "" for none and for a stub. */
  template: string
  vault: boolean
  /** A wikilink target that no entry answers yet. */
  stub: boolean
  degree: number
}

export interface GraphLink {
  source: string
  target: string
}

export interface VaultGraph {
  nodes: GraphNode[]
  links: GraphLink[]
  /** Stub slug to the slugs of the entries that link to it. */
  stubs: { node: GraphNode; from: string[] }[]
  /** Entries with no link in either direction. */
  orphans: GraphNode[]
  /** Node id to the ids it shares a link with. */
  neighbours: Map<string, Set<string>>
}

function nodeId(vault: boolean, slug: string) {
  return `${vault ? 'vault' : 'project'}:${slug}`
}

export function buildGraph(entries: GraphSource[], resolved: EntryLink[]): VaultGraph {
  const nodes = new Map<string, GraphNode>()
  const byAddress = new Map<string, string>()
  const slugOf = new Map<string, string>()
  for (const entry of entries) {
    const vault = Boolean(entry.global)
    const id = nodeId(vault, entry.slug)
    byAddress.set(entry.ref, id)
    slugOf.set(id, entry.slug)
    if (nodes.has(id)) continue
    nodes.set(id, {
      id, slug: entry.slug, title: entry.title, template: entry.template ?? '', vault, stub: false, degree: 0,
    })
  }

  const seen = new Set<string>()
  const links: GraphLink[] = []
  const stubSources = new Map<string, Set<string>>()

  for (const link of resolved) {
    const source = byAddress.get(link.from)
    // A link out of an entry this page does not list has nowhere to start.
    if (!source) continue
    let target = link.to ? byAddress.get(link.to) : undefined
    if (!target) {
      const label = link.raw.split('#')[0].trim() || link.raw
      target = `stub:${label}`
      if (!nodes.has(target)) {
        nodes.set(target, { id: target, slug: label, title: label, template: '', vault: false, stub: true, degree: 0 })
      }
      stubSources.set(target, (stubSources.get(target) ?? new Set()).add(slugOf.get(source) ?? source))
    }
    if (target === source) continue
    const pair = source < target ? `${source}|${target}` : `${target}|${source}`
    if (seen.has(pair)) continue
    seen.add(pair)
    links.push({ source, target })
  }

  const neighbours = new Map<string, Set<string>>()
  for (const id of nodes.keys()) neighbours.set(id, new Set())
  for (const link of links) {
    neighbours.get(link.source)!.add(link.target)
    neighbours.get(link.target)!.add(link.source)
  }
  for (const node of nodes.values()) node.degree = neighbours.get(node.id)!.size

  const all = [...nodes.values()]
  return {
    nodes: all,
    links,
    neighbours,
    stubs: all
      .filter((node) => node.stub)
      .sort((a, b) => a.slug.localeCompare(b.slug))
      .map((node) => ({ node, from: [...(stubSources.get(node.id) ?? [])].sort() })),
    orphans: all.filter((node) => !node.stub && node.degree === 0),
  }
}

export function entryNodeId(entry: { slug: string; global?: boolean }) {
  return nodeId(Boolean(entry.global), entry.slug)
}
