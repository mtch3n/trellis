import { useRef } from 'react'
import { Link } from 'react-router-dom'
import { Minus, Plus, Scan, X } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { ForceGraph, type ForceGraphHandle } from '@/components/wrappers/ForceGraph'
import { IconButton } from '@/components/wrappers/IconButton'
import type { GraphNode, VaultGraph } from '@/lib/entry-graph'

/**
 * The graph at full size. The canvas takes the screen; the rail beside it
 * answers the two questions a graph of a vault is actually asked:
 * what is connected to nothing, and which links point at entries nobody has
 * written yet. Choosing a node or a name opens that entry, and because the
 * explorer's open state lives in the URL, leaving the URL closes it.
 */
export function GraphExplorer({
  open,
  graph,
  activeId,
  projectKey,
  titleOf,
  onOpen,
  onOpenChange,
}: {
  open: boolean
  graph: VaultGraph
  activeId?: string
  projectKey?: string
  titleOf: (slug: string) => string
  onOpen: (node: GraphNode) => void
  onOpenChange: (open: boolean) => void
}) {
  const view = useRef<ForceGraphHandle>(null)
  // Focus lands on the canvas, so Tab walks the nodes before the toolbar.
  const canvas = useRef<HTMLDivElement>(null)
  const entries = graph.nodes.length - graph.stubs.length
  const href = (slug: string) => `/p/${projectKey}/vault/${encodeURIComponent(slug)}`

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        initialFocus={canvas}
        className="flex h-[94dvh] w-[96vw] max-w-none flex-col gap-0 p-0 duration-200 sm:max-w-none"
      >
        <header className="flex h-14 shrink-0 items-center gap-6 border-b border-border px-5">
          <DialogTitle>Graph</DialogTitle>
          <DialogDescription className="flex gap-4 text-xs">
            <span>{entries} {entries === 1 ? 'entry' : 'entries'}</span>
            <span>{graph.links.length} {graph.links.length === 1 ? 'link' : 'links'}</span>
            {graph.stubs.length > 0 && <span>{graph.stubs.length} {graph.stubs.length === 1 ? 'stub' : 'stubs'}</span>}
          </DialogDescription>
          <div className="ml-auto flex items-center gap-1">
            <IconButton label="Zoom out" onClick={() => view.current?.zoomBy(1 / 1.4)}>
              <Minus />
            </IconButton>
            <IconButton label="Zoom in" onClick={() => view.current?.zoomBy(1.4)}>
              <Plus />
            </IconButton>
            <IconButton label="Fit to view" onClick={() => view.current?.fit()}>
              <Scan />
            </IconButton>
            <IconButton label="Close" className="ml-3" onClick={() => onOpenChange(false)}>
              <X />
            </IconButton>
          </div>
        </header>

        <div className="flex min-h-0 flex-1">
          <div ref={canvas} tabIndex={-1} className="relative min-w-0 flex-1 bg-card outline-none">
            {graph.nodes.length === 0 ? (
              <p className="flex h-full items-center justify-center text-sm text-muted-foreground">
                Nothing to draw. Entries appear here as agents write them.
              </p>
            ) : (
              <ForceGraph ref={view} graph={graph} activeId={activeId} variant="full" onOpen={onOpen} />
            )}
            <p className="pointer-events-none absolute bottom-4 left-5 text-xs text-muted-foreground">
              Scroll to zoom, drag to pan, drag a node to pull it.
            </p>
          </div>

          <aside className="hidden w-80 shrink-0 flex-col gap-8 overflow-y-auto border-l border-border p-5 md:flex">
            <section>
              <h3 className="text-label text-muted-foreground">Orphans</h3>
              {graph.orphans.length === 0 ? (
                <p className="mt-2 text-sm text-muted-foreground">Every entry links somewhere.</p>
              ) : (
                <ul className="mt-2 flex flex-col">
                  {graph.orphans.map((node) => (
                    <li key={node.id}>
                      <Link
                        to={href(node.slug)}
                        className="-mx-2 block px-2 py-1.5 text-sm transition-colors hover:bg-accent"
                      >
                        {node.title}
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </section>

            <section>
              <h3 className="text-label text-muted-foreground">Stubs</h3>
              <p className="mt-2 text-xs text-muted-foreground">
                Links written before their target. Each one resolves when an entry with that slug is created.
              </p>
              {graph.stubs.length === 0 ? (
                <p className="mt-3 text-sm text-muted-foreground">No stubs.</p>
              ) : (
                <ul className="mt-3 flex flex-col gap-3">
                  {graph.stubs.map(({ node, from }) => (
                    <li key={node.id}>
                      <p className="text-meta">{node.slug}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        from{' '}
                        {from.map((slug, index) => (
                          <span key={slug}>
                            {index > 0 && ', '}
                            <Link
                              to={href(slug)}
                              className="underline decoration-border underline-offset-2 transition-colors hover:text-foreground hover:decoration-current"
                            >
                              {titleOf(slug)}
                            </Link>
                          </span>
                        ))}
                      </p>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </aside>
        </div>
      </DialogContent>
    </Dialog>
  )
}
