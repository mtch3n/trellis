import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Network, PanelRightClose, PanelRightOpen, Pencil } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { ArtifactList, type Artifact } from '@/components/wrappers/ArtifactList'
import { ActionRow } from '@/components/wrappers/ActionRow'
import { EditActions } from '@/components/wrappers/EditInPlace'
import { EntryView, type EntryDraft } from '@/components/wrappers/EntryView'
import { GraphDock } from '@/components/wrappers/GraphDock'
import { GraphExplorer } from '@/components/wrappers/GraphExplorer'
import { IconButton } from '@/components/wrappers/IconButton'
import { KnowledgeNav } from '@/components/wrappers/KnowledgeNav'
import { MetaFacts, MetaGroup, MetaPanel } from '@/components/wrappers/MetaPanel'
import { VaultLayout } from '@/components/wrappers/VaultLayout'
import { sentence } from '@/lib/format'
import { buildGraph, entryNodeId, type GraphNode } from '@/lib/knowledge-graph'
import { cn } from '@/lib/utils'

export interface KnowledgeEntry {
  id: string
  slug: string
  ref: string
  title: string
  summary?: string
  /** Set when the entry is pinned: the line a session reads before the body. */
  recap?: string
  body?: string
  type?: string
  path?: string
  global?: boolean
  private?: boolean
  created_at?: number
  updated_at?: number
  version: number
  /** Files attached to the entry. Absent when there are none, and never on vault entries. */
  artifacts?: Artifact[]
}

function when(timestamp?: number) {
  if (!timestamp) return null
  return new Date(timestamp).toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  })
}

/**
 * Knowledge as a workspace: the navigator with the graph docked beneath it on
 * the left, and the open entry with its facts centred in the rest.
 *
 * The navigator is not decoration. These entries are markdown files and the
 * scopes are directories, so a persistent tree shows the real structure and
 * makes moving between entries one click; with it always present, a separate
 * index page has nothing left to do.
 *
 * The graph is built here, once, from the bodies the list already carries, and
 * handed to both the dock and the explorer so they can never disagree. The
 * explorer's open state is the `view=graph` query, so it survives a reload and
 * following any link out of it closes it.
 */
export function KnowledgePage() {
  const { projectKey, slug } = useParams<{ projectKey: string; slug?: string }>()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const [vault, setVault] = useState<KnowledgeEntry[] | null>(null)
  const [project, setProject] = useState<KnowledgeEntry[] | null>(null)
  const [board, setBoard] = useState<string | null>(null)
  // Tagged with the entry it belongs to, so moving to another entry needs no reset.
  const [editingSlug, setEditingSlug] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [source, setSource] = useState(false)

  const exploring = params.get('view') === 'graph'
  // The facts column can be put away for reading, and stays put away.
  const [facts, setFacts] = useState(() => {
    try { return localStorage.getItem('trellis.vault-facts') !== 'hidden' } catch { return true }
  })
  const toggleFacts = () => {
    const next = !facts
    setFacts(next)
    try { localStorage.setItem('trellis.vault-facts', next ? 'shown' : 'hidden') } catch { /* private mode */ }
  }

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!projectKey) return
    const read = async (url: string) => {
      const response = await fetch(url, { signal })
      if (!response.ok) throw new Error(await response.text())
      return response.json()
    }
    try {
      const [globalEntries, projectEntries, boards] = await Promise.all([
        read('/api/global/knowledge') as Promise<KnowledgeEntry[]>,
        read(`/api/p/${projectKey}/knowledge`) as Promise<KnowledgeEntry[]>,
        read(`/api/p/${projectKey}/boards`) as Promise<{ slug: string }[]>,
      ])
      setVault(globalEntries ?? [])
      setProject(projectEntries ?? [])
      setBoard(boards?.[0]?.slug ?? null)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not load knowledge')
      setVault([]); setProject([])
    }
  }, [projectKey])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const entries = useMemo(() => [...(vault ?? []), ...(project ?? [])], [vault, project])
  const entry = useMemo(() => entries.find((item) => item.slug === slug), [entries, slug])
  const graph = useMemo(() => buildGraph(entries, projectKey), [entries, projectKey])
  const activeNode = entry ? entryNodeId(entry) : undefined
  const editing = Boolean(slug) && editingSlug === slug
  const setEditing = (on: boolean) => { setEditingSlug(on ? (slug ?? null) : null); setSource(false) }

  const neighbours = useMemo(() => {
    if (!activeNode) return []
    const near = graph.neighbours.get(activeNode) ?? new Set<string>()
    return graph.nodes.filter((node) => near.has(node.id)).sort((a, b) => Number(a.stub) - Number(b.stub) || a.title.localeCompare(b.title))
  }, [graph, activeNode])

  const titleOf = useCallback(
    (target: string) => entries.find((item) => item.slug === target)?.title ?? target,
    [entries],
  )
  const openNode = useCallback(
    (node: GraphNode) => navigate(`/p/${projectKey}/knowledge/${encodeURIComponent(node.slug)}`),
    [navigate, projectKey],
  )
  // Opening pushes a history entry so Back closes the explorer. Closing an
  // explorer this page opened steps back over that entry instead of adding
  // one, so Back never lands on a state that looks unchanged.
  const setExploring = (open: boolean) => {
    const next = new URLSearchParams(params)
    if (open) {
      next.set('view', 'graph')
      setParams(next, { state: { explorer: true } })
    } else if ((location.state as { explorer?: boolean } | null)?.explorer) {
      navigate(-1)
    } else {
      next.delete('view')
      setParams(next, { replace: true })
    }
  }

  const save = async (draft: EntryDraft) => {
    if (!projectKey || !board || !entry) return
    setSaving(true)
    try {
      const response = await fetch(
        `/api/p/${projectKey}/b/${board}/knowledge/${encodeURIComponent(entry.slug)}`,
        {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ...draft, version: entry.version }),
        },
      )
      // A conflict means the file or the entry moved on while this edit was
      // open. The page takes the new version and the edit stays open, so the
      // writer can look before deciding to save over it.
      if (response.status === 409) {
        await load()
        toast.add({
          title: 'Changed elsewhere',
          description: `${entry.slug} changed while you were editing and has been reloaded. Save again to replace that change, or cancel.`,
          type: 'error',
        })
        return
      }
      if (!response.ok) throw new Error(await response.text())
      await load()
      setEditing(false)
      toast.add({ title: `Saved ${entry.slug}`, type: 'success' })
    } catch (err) {
      toast.add({
        title: 'Could not save the entry',
        description: err instanceof Error ? err.message : 'Unknown error',
        type: 'error',
      })
    } finally { setSaving(false) }
  }

  if (vault === null || project === null) {
    return (
      <div className="lg:grid lg:grid-cols-nav">
        <div className="flex flex-col gap-3 px-4 pt-5 lg:h-under-shell lg:border-r lg:border-border">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="mt-4 h-4 w-24" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
        <main className="grid justify-center gap-x-14 px-6 py-8 grid-cols-measure lg:px-12 xl:grid-cols-entry">
          <div className="flex flex-col gap-4">
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-8 w-3/4" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="mt-6 h-96 w-full" />
          </div>
          <Skeleton className="hidden h-48 w-full xl:block" />
        </main>
      </div>
    )
  }

  const dock = (
    <GraphDock graph={graph} activeId={activeNode} onOpen={openNode} onExpand={() => setExploring(true)} />
  )

  // A fixed frame, like the board: the navigator and the entry each scroll on
  // their own, so the page never slides under the shell.
  return (
    <>
      <VaultLayout
        nav={
          <KnowledgeNav
            entries={entries}
            vaultCount={vault.length}
            projectKey={projectKey}
            activeId={entry?.id}
            dock={dock}
            onOpenGraph={() => setExploring(true)}
          />
        }
      >
        <main className="min-w-0 px-6 pb-8 lg:h-full lg:overflow-y-auto lg:px-12">
          {error && (
            <Alert variant="destructive" className="mx-auto mt-8 max-w-measure">
              <AlertTitle>Could not load knowledge</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {!error && !entry && (
            <Empty className="min-h-under-shell -mb-8 border-0">
              <EmptyHeader>
                <EmptyTitle className="text-heading font-semibold">
                  {entries.length === 0 ? 'Nothing written yet' : slug ? 'No entry by that name' : 'Choose an entry'}
                </EmptyTitle>
                <EmptyDescription>
                  {entries.length === 0
                    ? 'Agents write knowledge here as they work.'
                    : slug
                      ? `Nothing in this project or the vault is called ${slug}.`
                      : `${entries.length} ${entries.length === 1 ? 'entry' : 'entries'} in this project and the vault.`}
                </EmptyDescription>
              </EmptyHeader>
              {graph.nodes.length > 0 && (
                <EmptyContent>
                  <Button variant="outline" onClick={() => setExploring(true)}>
                    <Network data-icon="inline-start" />
                    Open the graph
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          )}

          {!error && entry && (
            <>
              {/* One row of actions over the entry, sticky while it scrolls: Edit, or
                  Save and Cancel in its place, then the facts toggle. Keeping them out
                  of the title's row means an edit never rewraps the title. */}
              <ActionRow className={cn('mx-auto h-16 max-w-measure lg:top-0', facts && 'xl:max-w-entry')}>
                <Breadcrumb className="min-w-0">
                  <BreadcrumbList className="flex-nowrap">
                    <BreadcrumbItem>
                      <BreadcrumbLink render={<Link to={`/p/${projectKey}/knowledge`} />}>Vault</BreadcrumbLink>
                    </BreadcrumbItem>
                    <BreadcrumbSeparator />
                    <BreadcrumbItem className="min-w-0">
                      <BreadcrumbPage className="truncate text-meta">{entry.slug}</BreadcrumbPage>
                    </BreadcrumbItem>
                  </BreadcrumbList>
                </Breadcrumb>
                <div className="ml-auto flex shrink-0 items-center gap-1.5">
                  {editing ? (
                    <EditActions form="entry-form" source={source} onSourceChange={setSource} label="Save entry" saving={saving} onCancel={() => setEditing(false)} />
                  ) : (
                    <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                      <Pencil data-icon="inline-start" />
                      Edit
                    </Button>
                  )}
                  <IconButton label={facts ? 'Hide details' : 'Show details'} onClick={toggleFacts}>
                    {facts ? <PanelRightClose /> : <PanelRightOpen />}
                  </IconButton>
                </div>
              </ActionRow>

              <div
                key={entry.id}
                className={cn(
                  // The gap under the action row leaves room for the title's edit wash.
                  'grid animate-enter justify-center gap-x-14 gap-y-10 pt-3 grid-cols-measure',
                  facts && 'xl:grid-cols-entry',
                )}
              >
                <EntryView
                  entry={entry}
                  editing={editing}
                  source={source}
                  onEditingChange={setEditing}
                  onSave={save}
                />

                {facts && (
                  <MetaPanel className="gap-6 xl:sticky xl:top-16 xl:self-start">
                    <MetaGroup label="Details" collapsible>
                      <MetaFacts
                        facts={[
                          { label: 'Slug', value: entry.slug, mono: true, stacked: true },
                          { label: 'Kind', value: sentence(entry.type ?? 'note') },
                          { label: 'Scope', value: entry.global ? 'Global vault' : (projectKey ?? 'Project') },
                          ...(entry.private ? [{ label: 'Visibility', value: 'Private' }] : []),
                          { label: 'Version', value: `v${entry.version}` },
                          { label: 'Created', value: when(entry.created_at) ?? 'Unknown' },
                          { label: 'Updated', value: when(entry.updated_at) ?? 'Unknown' },
                        ]}
                      />
                    </MetaGroup>

                    {entry.artifacts && entry.artifacts.length > 0 && (
                      <MetaGroup label="Attachments" count={entry.artifacts.length} collapsible>
                        <ArtifactList artifacts={entry.artifacts} />
                      </MetaGroup>
                    )}

                    <MetaGroup label="Linked" count={neighbours.length || undefined} collapsible>
                      {neighbours.length === 0 ? (
                        <p className="text-xs text-muted-foreground">
                          No links yet. A wikilink in this entry, or one pointing at it, appears here.
                        </p>
                      ) : (
                        <ul className="-mx-2 flex flex-col">
                          {neighbours.map((node) => (
                            <li key={node.id}>
                              {node.stub ? (
                                <p className="flex items-baseline justify-between gap-3 px-2 py-1.5" title="Not written yet">
                                  <span className="min-w-0 truncate text-meta text-muted-foreground">{node.slug}</span>
                                  <span className="shrink-0 text-xs text-muted-foreground">stub</span>
                                </p>
                              ) : (
                                <Link
                                  to={`/p/${projectKey}/knowledge/${encodeURIComponent(node.slug)}`}
                                  className="block px-2 py-1.5 text-sm leading-snug transition-colors hover:bg-accent/50"
                                >
                                  {node.title}
                                </Link>
                              )}
                            </li>
                          ))}
                        </ul>
                      )}
                    </MetaGroup>

                    {entry.path && (
                      <MetaGroup label="File" collapsible>
                        <p className="text-meta break-all text-muted-foreground">{entry.path}</p>
                      </MetaGroup>
                    )}
                  </MetaPanel>
                )}
              </div>
            </>
          )}
        </main>
      </VaultLayout>

      <GraphExplorer
        open={exploring}
        graph={graph}
        activeId={activeNode}
        projectKey={projectKey}
        titleOf={titleOf}
        onOpen={openNode}
        onOpenChange={setExploring}
      />
    </>
  )
}
