/**
 * The knowledge graph, built from the bodies the list endpoints already return.
 *
 * Parsing and resolution mirror `internal/core/markdown.go` (ParseWikilinks,
 * Slugify) and `resolveDocRef`, so the picture agrees with what the backend
 * stored: code spans and fences are skipped, `[[KEY/slug]]` is qualified,
 * `[[GLOBAL/slug]]` reaches the vault, and a link into another project is a
 * stub on purpose. Links are drawn undirected, because "what is this connected
 * to" is the question a reader asks, and the backend's traversal only follows
 * outbound edges.
 */

export const GLOBAL_KEY = 'GLOBAL'

export interface GraphSource {
  id: string
  slug: string
  title: string
  type?: string
  global?: boolean
}

export interface GraphNode {
  id: string
  slug: string
  title: string
  kind: string
  vault: boolean
  /** A wikilink target that no entry answers yet. */
  stub: boolean
  degree: number
}

export interface GraphLink {
  source: string
  target: string
}

export interface KnowledgeGraph {
  nodes: GraphNode[]
  links: GraphLink[]
  /** Stub slug to the slugs of the entries that link to it. */
  stubs: { node: GraphNode; from: string[] }[]
  /** Entries with no link in either direction. */
  orphans: GraphNode[]
  /** Node id to the ids it shares a link with. */
  neighbours: Map<string, Set<string>>
}

interface Reference {
  key: string
  slug: string
}

const FENCE = /```[\s\S]*?```|`[^`\n]*`/g
const WIKILINK = /\[\[([^\]|#]+)(#[^\]|]+)?(\|[^\]]+)?\]\]/g

export function slugify(text: string) {
  return text.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
}

export function parseWikilinks(body: string): Reference[] {
  const seen = new Set<string>()
  const refs: Reference[] = []
  for (const match of body.replace(FENCE, '').matchAll(WIKILINK)) {
    let target = match[1].trim()
    let key = ''
    const slash = target.indexOf('/')
    if (slash >= 0) {
      key = target.slice(0, slash).toUpperCase()
      target = target.slice(slash + 1)
    }
    const slug = slugify(target)
    const raw = match[1].trim() + (match[2] ?? '')
    if (!slug || seen.has(raw)) continue
    seen.add(raw)
    refs.push({ key, slug })
  }
  return refs
}

function nodeId(vault: boolean, slug: string) {
  return `${vault ? 'vault' : 'project'}:${slug}`
}

export function buildGraph(
  entries: (GraphSource & { body?: string })[],
  projectKey: string | undefined,
): KnowledgeGraph {
  const nodes = new Map<string, GraphNode>()
  for (const entry of entries) {
    const vault = Boolean(entry.global)
    const id = nodeId(vault, entry.slug)
    if (nodes.has(id)) continue
    nodes.set(id, {
      id, slug: entry.slug, title: entry.title, kind: entry.type ?? 'note', vault, stub: false, degree: 0,
    })
  }

  const seen = new Set<string>()
  const links: GraphLink[] = []
  const stubSources = new Map<string, Set<string>>()

  for (const entry of entries) {
    const vault = Boolean(entry.global)
    const source = nodeId(vault, entry.slug)
    for (const ref of parseWikilinks(entry.body ?? '')) {
      let target: string | null
      if (ref.key === '') target = nodeId(vault, ref.slug)
      else if (ref.key === GLOBAL_KEY) target = nodeId(true, ref.slug)
      else if (projectKey && ref.key === projectKey.toUpperCase()) target = nodeId(false, ref.slug)
      else target = null

      if (!target || !nodes.has(target)) {
        const label = ref.key ? `${ref.key}/${ref.slug}` : ref.slug
        const id = `stub:${label}`
        if (!nodes.has(id)) {
          nodes.set(id, { id, slug: label, title: label, kind: 'stub', vault: false, stub: true, degree: 0 })
        }
        stubSources.set(id, (stubSources.get(id) ?? new Set()).add(entry.slug))
        target = id
      }
      if (target === source) continue
      const pair = source < target ? `${source}|${target}` : `${target}|${source}`
      if (seen.has(pair)) continue
      seen.add(pair)
      links.push({ source, target })
    }
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
