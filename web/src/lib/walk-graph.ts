/**
 * A server walk as the force graph draws it. `Traverse` answers with the
 * entities it reached and the links it followed; the picture component speaks
 * the vault graph's shape, so the two meet here rather than in either of them.
 */
import type { VaultGraph } from '@/lib/entry-graph'

/** One entity a walk reached. */
export interface WalkNode {
  type: string
  id: string
  ref: string
  title: string
  depth: number
  done?: boolean
}

/** A walk, as `GET /api/p/{key}/graph/{entity}` answers. */
export interface Walk {
  root: string
  nodes: WalkNode[]
  edges: { from: string; to: string; rel: string }[]
}

/**
 * The walk as a graph to draw. A node's kind stands where an entry's template
 * would, because "card" or "entry" is what a reader needs to tell two nodes
 * apart here; nothing in a walk is a stub, since the server only reports what
 * it actually reached.
 */
export function walkGraph(walk: Walk): VaultGraph {
  const degree = new Map<string, number>()
  for (const edge of walk.edges) {
    degree.set(edge.from, (degree.get(edge.from) ?? 0) + 1)
    degree.set(edge.to, (degree.get(edge.to) ?? 0) + 1)
  }
  const nodes = walk.nodes.map((node) => ({
    id: node.id,
    slug: node.ref,
    title: node.title,
    template: node.type,
    vault: node.type === 'entry',
    stub: false,
    degree: degree.get(node.id) ?? 0,
  }))
  const neighbours = new Map<string, Set<string>>()
  for (const edge of walk.edges) {
    if (!neighbours.has(edge.from)) neighbours.set(edge.from, new Set())
    if (!neighbours.has(edge.to)) neighbours.set(edge.to, new Set())
    neighbours.get(edge.from)?.add(edge.to)
    neighbours.get(edge.to)?.add(edge.from)
  }
  return {
    nodes,
    links: walk.edges.map((edge) => ({ source: edge.from, target: edge.to })),
    // A walk reports only what it reached, so it has no stubs, and a node
    // with no edge is the walk's own root rather than an orphan.
    stubs: [],
    orphans: [],
    neighbours,
  }
}
