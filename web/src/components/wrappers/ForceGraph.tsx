import { forwardRef, useCallback, useEffect, useImperativeHandle, useLayoutEffect, useMemo, useRef } from 'react'
import { drag } from 'd3-drag'
import {
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type Simulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force'
import { select } from 'd3-selection'
import 'd3-transition'
import { zoom, zoomIdentity, zoomTransform, type ZoomBehavior, type ZoomTransform } from 'd3-zoom'
import { cn } from '@/lib/utils'
import type { GraphNode, KnowledgeGraph } from '@/lib/knowledge-graph'

export interface ForceGraphHandle {
  zoomBy: (factor: number) => void
  fit: () => void
}

interface SimNode extends SimulationNodeDatum {
  id: string
  r: number
}

type SimLink = SimulationLinkDatum<SimNode> & { key: string }

const DENSITY = {
  // The sidebar dock: tighter springs, names only where they matter.
  compact: { distance: 60, charge: -160, labelsFrom: 2.6, maxFit: 1.6 },
  full: { distance: 90, charge: -300, labelsFrom: 0.8, maxFit: 2.2 },
} as const

const DURATION = 420
const easeOutQuart = (t: number) => 1 - (1 - t) ** 4

function radius(node: GraphNode) {
  if (node.stub) return 3
  return Math.min(3.5 + Math.sqrt(node.degree) * 1.25, 8)
}

function linkKey(source: string, target: string) {
  return `${source}|${target}`
}

function short(text: string, max: number) {
  return text.length > max ? `${text.slice(0, max - 1).trimEnd()}…` : text
}

function reducedMotion() {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * A force-directed picture of the knowledge graph that you can pan, zoom and
 * pull apart.
 *
 * d3 owns the physics, the zoom and the drag; React owns the elements. The
 * simulation writes positions straight to the DOM and hover writes data
 * attributes, so neither re-renders the tree. d3-force jiggles with a seeded
 * generator and places new nodes on a fixed spiral, so the same vault settles
 * into the same picture every time. The layout settles before the first
 * paint; the simulation only runs live while a node is being pulled.
 */
export const ForceGraph = forwardRef<ForceGraphHandle, {
  graph: KnowledgeGraph
  activeId?: string
  variant?: 'compact' | 'full'
  onOpen: (node: GraphNode) => void
  className?: string
}>(function ForceGraph({ graph, activeId, variant = 'full', onOpen, className }, handle) {
  const density = DENSITY[variant]
  const svgRef = useRef<SVGSVGElement>(null)
  const viewportRef = useRef<SVGGElement>(null)
  const nodeEls = useRef(new Map<string, SVGGElement>())
  const linkEls = useRef(new Map<string, SVGLineElement>())
  const zoomRef = useRef<ZoomBehavior<SVGSVGElement, unknown> | null>(null)
  const simRef = useRef<Simulation<SimNode, SimLink> | null>(null)
  const positions = useRef(new Map<string, { x: number; y: number }>())
  const laidOut = useRef('')
  const fitted = useRef(false)
  // Once the reader pans or zooms, the view is theirs: no automatic refit.
  const touched = useRef(false)
  const activeRef = useRef(activeId)
  const dragging = useRef(false)
  const onOpenRef = useRef(onOpen)
  useEffect(() => { onOpenRef.current = onOpen })

  // Sorted, so the seeded layout depends on the vault and not on list order.
  const nodes = useMemo(() => [...graph.nodes].sort((a, b) => a.id.localeCompare(b.id)), [graph])
  const byId = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes])

  const paint = useCallback(() => {
    const sim = simRef.current
    if (!sim) return
    for (const node of sim.nodes()) {
      nodeEls.current.get(node.id)?.setAttribute('transform', `translate(${node.x ?? 0},${node.y ?? 0})`)
      positions.current.set(node.id, { x: node.x ?? 0, y: node.y ?? 0 })
    }
    const links = sim.force<ReturnType<typeof forceLink<SimNode, SimLink>>>('link')?.links() ?? []
    for (const link of links) {
      const el = linkEls.current.get(link.key)
      const source = link.source as SimNode
      const target = link.target as SimNode
      if (!el) continue
      el.setAttribute('x1', String(source.x ?? 0))
      el.setAttribute('y1', String(source.y ?? 0))
      el.setAttribute('x2', String(target.x ?? 0))
      el.setAttribute('y2', String(target.y ?? 0))
    }
  }, [])

  // Layout size, not the painted box: the explorer scales in, and a measure
  // taken mid-animation would fit the graph to a frame 5% too small.
  const size = () => ({
    width: svgRef.current?.clientWidth || 1,
    height: svgRef.current?.clientHeight || 1,
  })

  const apply = useCallback((transform: ZoomTransform, animate: boolean) => {
    const svg = svgRef.current
    const behaviour = zoomRef.current
    if (!svg || !behaviour) return
    const selection = select(svg)
    if (animate && !reducedMotion()) {
      selection.transition().duration(DURATION).ease(easeOutQuart).call(behaviour.transform, transform)
    } else {
      selection.interrupt().call(behaviour.transform, transform)
    }
  }, [])

  const fitTransform = useCallback(() => {
    const { width, height } = size()
    const points = [...positions.current.values()]
    if (points.length === 0) return zoomIdentity.translate(width / 2, height / 2)
    const xs = points.map((point) => point.x)
    const ys = points.map((point) => point.y)
    const [minX, maxX, minY, maxY] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)]
    const pad = variant === 'compact' ? 28 : 64
    const k = Math.max(
      0.2,
      Math.min((width - pad * 2) / Math.max(maxX - minX, 1), (height - pad * 2) / Math.max(maxY - minY, 1), density.maxFit),
    )
    return zoomIdentity.translate(width / 2 - k * ((minX + maxX) / 2), height / 2 - k * ((minY + maxY) / 2)).scale(k)
  }, [density.maxFit, variant])

  const light = useCallback((id: string | null) => {
    const svg = svgRef.current
    if (!svg) return
    if (id === null) svg.removeAttribute('data-lit')
    else svg.setAttribute('data-lit', '')
    const near = id ? graph.neighbours.get(id) : undefined
    for (const [nodeId, el] of nodeEls.current) {
      if (id !== null && (nodeId === id || near?.has(nodeId))) el.setAttribute('data-lit', '')
      else el.removeAttribute('data-lit')
    }
    for (const [key, el] of linkEls.current) {
      const [source, target] = key.split('|')
      if (id !== null && (source === id || target === id)) el.setAttribute('data-lit', '')
      else el.removeAttribute('data-lit')
    }
  }, [graph])

  useImperativeHandle(handle, () => ({
    zoomBy: (factor) => {
      const svg = svgRef.current
      if (!svg || !zoomRef.current) return
      const selection = select(svg)
      if (reducedMotion()) zoomRef.current.scaleBy(selection, factor)
      else zoomRef.current.scaleBy(selection.transition().duration(220).ease(easeOutQuart), factor)
    },
    fit: () => apply(fitTransform(), true),
  }), [apply, fitTransform])

  // Pan and zoom, once per mount.
  useLayoutEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    const behaviour = zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.2, 4])
      .on('zoom', (event: { transform: ZoomTransform; sourceEvent: Event | null }) => {
        const { transform } = event
        if (event.sourceEvent) touched.current = true
        viewportRef.current?.setAttribute('transform', transform.toString())
        svg.style.setProperty('--graph-k', String(transform.k))
        svg.setAttribute('data-labels', transform.k >= density.labelsFrom ? 'all' : 'quiet')
      })
    zoomRef.current = behaviour
    const selection = select(svg)
    selection.call(behaviour).on('dblclick.zoom', null)
    return () => { selection.on('.zoom', null) }
  }, [density.labelsFrom])

  // The simulation. Rebuilt when the graph changes, keeping every position
  // it already knew so a save does not scramble the picture.
  useLayoutEffect(() => {
    const known = positions.current
    const simNodes: SimNode[] = nodes.map((node) => ({
      id: node.id,
      r: radius(node),
      ...(known.get(node.id) ?? {}),
    }))
    const simLinks: SimLink[] = graph.links.map((link) => ({
      source: link.source,
      target: link.target,
      key: linkKey(link.source, link.target),
    }))
    // Read before the simulation exists: constructing it places every node.
    const fresh = simNodes.some((node) => node.x === undefined)

    const sim = forceSimulation<SimNode, SimLink>(simNodes)
      .force('link', forceLink<SimNode, SimLink>(simLinks).id((node) => node.id).distance(density.distance))
      .force('charge', forceManyBody<SimNode>().strength(density.charge).distanceMax(400))
      // A gentle pull to the origin keeps unlinked entries from drifting off.
      .force('x', forceX<SimNode>(0).strength(0.07))
      .force('y', forceY<SimNode>(0).strength(0.07))
      .force('collide', forceCollide<SimNode>((node) => node.r + 6))
      .stop()
    simRef.current = sim

    // A new layout settles completely before it is painted, so it arrives
    // still and nothing shifts after the reader has started looking. The
    // arrival is the nodes fading in. A graph that changed under an existing
    // picture eases its new nodes into place from where the old ones were.
    const shape = `${nodes.map((node) => node.id).join()}#${simLinks.map((link) => link.key).join()}`
    const changed = laidOut.current !== '' && laidOut.current !== shape
    laidOut.current = shape
    if (fresh || reducedMotion()) sim.tick(300)
    else if (changed) sim.alpha(0.3).restart()
    sim.on('tick', paint)
    paint()

    if (!fitted.current) {
      fitted.current = true
      apply(fitTransform(), false)
    }
    return () => { sim.stop() }
  }, [nodes, graph.links, density.distance, density.charge, paint, apply, fitTransform])

  // The dock animates open and the explorer scales in, so the first measure
  // can be stale. Until the reader takes over, a resize refits.
  useEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    const observer = new ResizeObserver(() => {
      if (!touched.current) apply(fitTransform(), false)
    })
    observer.observe(svg)
    return () => observer.disconnect()
  }, [apply, fitTransform])

  // Dragging a node pins it while held and lets it go on release.
  useEffect(() => {
    const sim = simRef.current
    if (!sim) return
    const simById = new Map(sim.nodes().map((node) => [node.id, node]))
    const bound: SVGGElement[] = []
    for (const [id, el] of nodeEls.current) {
      const subject = simById.get(id)
      if (!subject) continue
      // d3-drag starts on press, before any movement. The physics wakes only
      // once the node actually moves, so a plain click opens the entry
      // without stirring the picture first.
      let moved = false
      const behaviour = drag<SVGGElement, unknown, SimNode>()
        .subject(() => subject)
        .clickDistance(4)
        .on('start', () => {
          moved = false
          dragging.current = true
          light(id)
        })
        .on('drag', (event) => {
          if (!moved) {
            moved = true
            touched.current = true
            if (!reducedMotion()) sim.alphaTarget(0.25).restart()
          }
          subject.fx = event.x
          subject.fy = event.y
          if (reducedMotion()) { subject.x = event.x; subject.y = event.y; paint() }
        })
        .on('end', () => {
          dragging.current = false
          if (moved) sim.alphaTarget(0)
          subject.fx = null
          subject.fy = null
          if (!el.matches(':hover')) light(null)
        })
      select(el).call(behaviour)
      bound.push(el)
    }
    return () => { for (const el of bound) select(el).on('.drag', null) }
  }, [nodes, graph.links, light, paint])

  // Follow the reader: when the open entry changes and its node is outside
  // the comfortable middle of the view, glide to it.
  useEffect(() => {
    const previous = activeRef.current
    activeRef.current = activeId
    if (!activeId || activeId === previous || !fitted.current) return
    const svg = svgRef.current
    const point = positions.current.get(activeId)
    if (!svg || !point || !zoomRef.current) return
    const { width, height } = size()
    const current = zoomTransform(svg)
    const [sx, sy] = current.apply([point.x, point.y])
    const inside = sx > width * 0.2 && sx < width * 0.8 && sy > height * 0.2 && sy < height * 0.8
    if (inside) return
    apply(zoomIdentity.translate(width / 2 - current.k * point.x, height / 2 - current.k * point.y).scale(current.k), true)
  }, [activeId, apply])

  const labelMax = variant === 'compact' ? 24 : 34

  return (
    <svg
      ref={svgRef}
      className={cn('force-graph block size-full select-none', className)}
      role="group"
      aria-label={`Knowledge graph: ${graph.nodes.length - graph.stubs.length} entries, ${graph.links.length} links, ${graph.stubs.length} stubs`}
      data-labels="quiet"
    >
      <g ref={viewportRef}>
        <g>
          {graph.links.map((link) => {
            const key = linkKey(link.source, link.target)
            const stub = byId.get(link.target)?.stub || byId.get(link.source)?.stub
            const touchesActive = link.source === activeId || link.target === activeId
            return (
              <line
                key={key}
                ref={(el) => { if (el) linkEls.current.set(key, el); else linkEls.current.delete(key) }}
                className="graph-edge"
                data-stub={stub ? '' : undefined}
                data-active={touchesActive ? '' : undefined}
              />
            )
          })}
        </g>
        <g>
          {nodes.map((node, index) => {
            const r = radius(node)
            const active = node.id === activeId
            const name = node.stub ? node.slug : node.title
            return (
              <g
                key={node.id}
                ref={(el) => { if (el) nodeEls.current.set(node.id, el); else nodeEls.current.delete(node.id) }}
                className="graph-node"
                data-enter=""
                style={{ animationDelay: `${Math.min(index * 18, 360)}ms` }}
                data-stub={node.stub ? '' : undefined}
                data-active={active ? '' : undefined}
                role={node.stub ? 'img' : 'link'}
                tabIndex={node.stub ? undefined : 0}
                aria-current={active ? 'page' : undefined}
                aria-label={node.stub ? `Stub: ${node.slug}` : `${node.title}, ${node.kind}, ${node.degree} links`}
                onPointerEnter={() => { if (!dragging.current) light(node.id) }}
                onPointerLeave={() => { if (!dragging.current) light(null) }}
                onFocus={() => light(node.id)}
                onBlur={() => light(null)}
                onClick={() => { if (!node.stub) onOpenRef.current(node) }}
                onKeyDown={(event) => {
                  if (!node.stub && (event.key === 'Enter' || event.key === ' ')) {
                    event.preventDefault()
                    onOpenRef.current(node)
                  }
                }}
              >
                {/* Held at a constant size on screen: zoom spreads the layout, not the glyphs. */}
                <g className="graph-glyph">
                  <rect className="graph-halo" x={-r - 4} y={-r - 4} width={(r + 4) * 2} height={(r + 4) * 2} />
                  <rect className="graph-shape" x={-r} y={-r} width={r * 2} height={r * 2} />
                  <text className="graph-label" y={r} dy="1.3em" textAnchor="middle">
                    {short(name, labelMax)}
                  </text>
                </g>
                <title>{name}</title>
              </g>
            )
          })}
        </g>
      </g>
    </svg>
  )
})
