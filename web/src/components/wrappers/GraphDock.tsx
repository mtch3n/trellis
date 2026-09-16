import { useRef, useState } from 'react'
import { ChevronRight, Maximize2, Scan } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ForceGraph, type ForceGraphHandle } from '@/components/wrappers/ForceGraph'
import { IconButton } from '@/components/wrappers/IconButton'
import type { GraphNode, KnowledgeGraph } from '@/lib/knowledge-graph'

const STORAGE_KEY = 'trellis.graph-dock'

function initiallyOpen() {
  try {
    return localStorage.getItem(STORAGE_KEY) !== 'closed'
  } catch {
    return true
  }
}

/**
 * The graph, docked at the foot of the knowledge navigator and open by
 * default. It shows the whole vault with the open entry marked, so the reader
 * always sees where they are and what the entry touches. Expanding hands the
 * same graph to the explorer rather than growing the dock.
 */
export function GraphDock({
  graph,
  activeId,
  onOpen,
  onExpand,
}: {
  graph: KnowledgeGraph
  activeId?: string
  onOpen: (node: GraphNode) => void
  onExpand: () => void
}) {
  const [open, setOpen] = useState(initiallyOpen)
  const view = useRef<ForceGraphHandle>(null)
  const empty = graph.nodes.length === 0

  const toggle = (next: boolean) => {
    setOpen(next)
    try { localStorage.setItem(STORAGE_KEY, next ? 'open' : 'closed') } catch { /* private mode */ }
  }

  return (
    <Collapsible open={open} onOpenChange={toggle} className="shrink-0 border-t border-border">
      <div className="flex h-11 items-center gap-1 pr-2 pl-2">
        <CollapsibleTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5 px-2 text-label text-muted-foreground hover:text-foreground aria-expanded:bg-transparent aria-expanded:text-muted-foreground aria-expanded:hover:bg-muted aria-expanded:hover:text-foreground"
            />
          }
        >
          <ChevronRight
            data-icon="inline-start"
            className="transition-transform duration-200 ease-settle group-aria-expanded/button:rotate-90"
          />
          Graph
        </CollapsibleTrigger>
        {graph.stubs.length > 0 && (
          <span className="text-xs text-muted-foreground">
            {graph.stubs.length} {graph.stubs.length === 1 ? 'stub' : 'stubs'}
          </span>
        )}
        <div className="ml-auto flex items-center">
          {open && !empty && (
            <IconButton label="Fit to view" onClick={() => view.current?.fit()}>
              <Scan />
            </IconButton>
          )}
          <IconButton label="Expand the graph" onClick={onExpand}>
            <Maximize2 />
          </IconButton>
        </div>
      </div>

      <CollapsibleContent className="h-(--collapsible-panel-height) overflow-hidden transition-all duration-200 ease-settle data-ending-style:h-0 data-starting-style:h-0">
        <div className="h-72 bg-card">
          {empty ? (
            <p className="flex h-full items-center justify-center px-6 text-center text-xs text-muted-foreground">
              Entries appear here as agents write them.
            </p>
          ) : (
            <ForceGraph ref={view} graph={graph} activeId={activeId} variant="compact" onOpen={onOpen} />
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
