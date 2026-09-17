import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Minus, Plus, Scan } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { ForceGraph, type ForceGraphHandle } from '@/components/wrappers/ForceGraph'
import { IconButton } from '@/components/wrappers/IconButton'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { readError } from '@/lib/api'
import { walkGraph, type Walk } from '@/lib/walk-graph'

/** How far a walk goes. Beyond four hops a picture stops meaning anything. */
const DEPTHS = [1, 2, 3, 4]

/** The kinds of link a walk can follow. */
const RELATIONS = [
  { value: 'blocked_by', label: 'Blocked by' },
  { value: 'cites', label: 'Cites' },
  { value: 'wikilink', label: 'Wikilinks' },
  { value: 'artifact', label: 'Artifacts' },
]

/**
 * The graph walked from one card, entry or artifact: what it reaches, how far
 * out, following which kinds of link, and in which direction.
 *
 * The vault's own graph is drawn from the links the server resolved, which
 * answers "what is connected to what" across a whole vault. This answers the
 * other question — "XPSCTL-12 is blocked, by what, and is that thing itself
 * blocked?" — which one hop cannot see. Walking inbound turns it into "what
 * breaks if I change this".
 *
 * The controls live in the URL, so a walk worth showing someone is a link.
 */
export function GraphPage() {
  const { projectKey, entity } = useParams<{ projectKey: string; entity: string }>()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const [walk, setWalk] = useState<Walk | null>(null)
  const [error, setError] = useState<string | null>(null)
  const view = useRef<ForceGraphHandle>(null)

  const depth = Number(params.get('depth') ?? '2') || 2
  const rel = params.get('rel') ?? ''
  const reverse = params.get('reverse') === '1'
  const set = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }

  const load = useCallback(async (signal: AbortSignal) => {
    if (!projectKey || !entity) return
    const query = new URLSearchParams({ depth: String(depth) })
    if (rel) query.set('rel', rel)
    if (reverse) query.set('reverse', '1')
    try {
      const response = await fetch(
        `/api/p/${projectKey}/graph/${encodeURIComponent(entity)}?${query}`,
        { signal },
      )
      if (!response.ok) throw new Error(await readError(response))
      setWalk((await response.json()) as Walk)
      setError(null)
    } catch (err) {
      if (signal.aborted) return
      setError(err instanceof Error ? err.message : 'Could not walk the graph')
    }
  }, [projectKey, entity, depth, rel, reverse])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const graph = useMemo(() => (walk ? walkGraph(walk) : null), [walk])
  const root = walk?.nodes.find((node) => node.id === walk.root)
  // Everything the walk reached, nearest first, so the list reads as the
  // chain it is rather than as a set.
  const reached = useMemo(
    () => (walk?.nodes ?? []).filter((node) => node.id !== walk?.root).sort((a, b) => a.depth - b.depth),
    [walk],
  )

  const pathOf = (node: { type: string; ref: string }) =>
    node.type === 'card'
      ? `/p/${projectKey}/card/${encodeURIComponent(node.ref)}`
      : `/p/${projectKey}/vault/${encodeURIComponent(node.ref.split('/').slice(3).join('/'))}`

  return (
    <main className="flex h-under-shell flex-col px-6 lg:px-8">
      <PageHeader
        title={root?.title ?? entity ?? 'Graph'}
        description={reverse ? 'What waits on this, outward from it' : 'What this waits on, outward from it'}
        facts={[{ label: 'Reached', value: reached.length }]}
        actions={
          <>
            <ToggleGroup
              value={[reverse ? 'in' : 'out']}
              onValueChange={(value) => set('reverse', value[0] === 'in' ? '1' : '')}
              className="shrink-0"
            >
              <ToggleGroupItem value="out" aria-label="Walk outbound">Outbound</ToggleGroupItem>
              <ToggleGroupItem value="in" aria-label="Walk inbound">Inbound</ToggleGroupItem>
            </ToggleGroup>
            <Select
              // The list a Select reads its shown label from has to hold every
              // option, "every link" included.
              items={[{ value: 'all', label: 'Every link' }, ...RELATIONS]}
              value={rel || 'all'}
              onValueChange={(value) => { if (value) set('rel', value === 'all' ? '' : value) }}
            >
              <SelectTrigger size="sm" aria-label="Kinds of link" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Every link</SelectItem>
                {RELATIONS.map((relation) => (
                  <SelectItem key={relation.value} value={relation.value}>{relation.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              items={DEPTHS.map((hops) => ({ value: String(hops), label: `${hops} hop${hops === 1 ? '' : 's'}` }))}
              value={String(depth)}
              onValueChange={(value) => { if (value) set('depth', value) }}
            >
              <SelectTrigger size="sm" aria-label="How far to walk" className="w-28">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DEPTHS.map((hops) => (
                  <SelectItem key={hops} value={String(hops)}>{hops} hop{hops === 1 ? '' : 's'}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <IconButton label="Zoom out" onClick={() => view.current?.zoomBy(1 / 1.4)}>
              <Minus />
            </IconButton>
            <IconButton label="Zoom in" onClick={() => view.current?.zoomBy(1.4)}>
              <Plus />
            </IconButton>
            <IconButton label="Fit to view" onClick={() => view.current?.fit()}>
              <Scan />
            </IconButton>
          </>
        }
      />

      {error && (
        <Alert variant="destructive">
          <AlertTitle>Could not walk the graph</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!error && (
        <div className="grid min-h-0 flex-1 gap-6 pb-6 lg:grid-cols-facts">
          <div className="min-h-0 bg-card">
            {graph ? (
              <ForceGraph
                ref={view}
                graph={graph}
                activeId={walk?.root}
                onOpen={(node) => navigate(pathOf({ type: node.template, ref: node.slug }))}
              />
            ) : (
              <Skeleton className="size-full" />
            )}
          </div>

          <aside className="min-h-0 overflow-y-auto">
            <h2 className="text-label text-muted-foreground">Reached</h2>
            {reached.length === 0 ? (
              <p className="mt-3 text-sm text-pretty text-muted-foreground">
                Nothing within {depth} {depth === 1 ? 'hop' : 'hops'} along {rel ? RELATIONS.find((relation) => relation.value === rel)?.label.toLowerCase() : 'any link'}.
              </p>
            ) : (
              <ul className="mt-3 -mx-2 flex flex-col">
                {reached.map((node) => (
                  <li key={node.id}>
                    <Link to={pathOf(node)} className="flex flex-col gap-0.5 px-2 py-1.5 transition-colors hover:bg-accent/50">
                      <span className="truncate text-sm">{node.title}</span>
                      <span className="flex items-baseline gap-2">
                        <span className="truncate text-meta text-muted-foreground">{node.ref}</span>
                        <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                          {node.depth} {node.depth === 1 ? 'hop' : 'hops'}
                        </span>
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
            <Button variant="outline" size="sm" className="mt-6" render={<Link to={`/p/${projectKey}`} />} nativeButton={false}>
              Back to the project
            </Button>
          </aside>
        </div>
      )}
    </main>
  )
}
