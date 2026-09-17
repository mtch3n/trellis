import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { FilePlus, Network, PanelRightClose, PanelRightOpen, Pencil, Pin } from 'lucide-react'
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
import { ArtifactList } from '@/components/wrappers/ArtifactList'
import { ActionRow } from '@/components/wrappers/ActionRow'
import { EditActions } from '@/components/wrappers/EditInPlace'
import { EntryMenu } from '@/components/wrappers/EntryMenu'
import { EntryView, type EntryDraft } from '@/components/wrappers/EntryView'
import { HistoryDialog } from '@/components/wrappers/HistoryDialog'
import { LifecycleDialog, type Lifecycle } from '@/components/wrappers/LifecycleDialog'
import { Badge } from '@/components/ui/badge'
import { ChipEditor } from '@/components/wrappers/ChipEditor'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { vaultActions, type EntryPatch } from '@/lib/vault-actions'
import { FieldsEditor } from '@/components/wrappers/FieldsEditor'
import { GraphDock } from '@/components/wrappers/GraphDock'
import { GraphExplorer } from '@/components/wrappers/GraphExplorer'
import { IconButton } from '@/components/wrappers/IconButton'
import { VaultNav } from '@/components/wrappers/VaultNav'
import { MetaFacts, MetaGroup, MetaPanel } from '@/components/wrappers/MetaPanel'
import { NewEntryDialog, type CreatedEntry } from '@/components/wrappers/NewEntryDialog'
import { SourcesEditor } from '@/components/wrappers/SourcesEditor'
import { TemplateSelect } from '@/components/wrappers/TemplateSelect'
import { TemplateSwitchDialog, type TemplateSwitch } from '@/components/wrappers/TemplateSwitchDialog'
import { VaultLayout } from '@/components/wrappers/VaultLayout'
import { sentence, templateLabel } from '@/lib/format'
import { buildGraph, entryNodeId, type GraphNode, type EntryLink } from '@/lib/entry-graph'
import type { Entry } from '@/lib/entry'
import { fieldRows, switchNeedsDialog, type TemplateInfo } from '@/lib/templates'
import { cn } from '@/lib/utils'
import { projectFolders } from '@/lib/vault-tree'
import { readError, refusalText, type Refusal } from '@/lib/api'


/** A Select needs a value for "none"; the wire carries an empty string. */
const NO_BOARD = 'none'

/** One pinned entry, as the pins route returns it. */
interface PinnedEntry {
  slug: string
  title: string
  recap: string
  board?: string
  /** The entry changed after it was pinned, so the recap may no longer hold. */
  stale: boolean
}

/** A template's warnings as a toast's words, one sentence each. */
function warningText(warnings: string[]) {
  return warnings.map((warning) => `${sentence(warning.trim()).replace(/\.$/, '')}.`).join(' ')
}

function when(timestamp?: number) {
  if (!timestamp) return null
  return new Date(timestamp).toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  })
}

/**
 * The vault as a workspace: the navigator with the graph docked beneath it on
 * the left, and the open entry with its facts centred in the rest.
 *
 * The navigator is not decoration. These entries are markdown files and the
 * scopes are directories, so a persistent tree shows the real structure and
 * makes moving between entries one click; with it always present, a separate
 * index page has nothing left to do.
 *
 * Lists carry no bodies, so the open entry is fetched on its own, when it is
 * chosen and again after every save. The graph is built here, once, from the
 * links the server resolved, and handed to both the dock and the explorer so
 * they can never disagree. The
 * explorer's open state is the `view=graph` query, so it survives a reload and
 * following any link out of it closes it.
 */
export function VaultPage() {
  const { projectKey, slug } = useParams<{ projectKey: string; slug?: string }>()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const [vault, setVault] = useState<Entry[] | null>(null)
  const [project, setProject] = useState<Entry[] | null>(null)
  const [board, setBoard] = useState<string | null>(null)
  const [links, setLinks] = useState<EntryLink[]>([])
  // Tagged with the entry it belongs to, so moving to another entry needs no reset.
  const [editingSlug, setEditingSlug] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [source, setSource] = useState(false)
  // The open entry in full: its body and artifacts. Tagged with the slug it
  // belongs to, so a stale answer for the previous entry is never shown.
  const [detail, setDetail] = useState<Entry | null>(null)
  const [templates, setTemplates] = useState<TemplateInfo[]>([])
  // The folder a new entry is being started in, while the dialog is open.
  const [creatingIn, setCreatingIn] = useState<string | null>(null)
  // A just-created entry opens with the cursor in its body.
  const [fresh, setFresh] = useState<string | null>(null)
  const [switching, setSwitching] = useState<TemplateInfo | null>(null)
  // The project's boards and labels, for the facts an entry can change, and
  // its pins, so the open entry knows whether it is one.
  const [boards, setBoards] = useState<{ name: string; slug: string }[]>([])
  const [labelOptions, setLabelOptions] = useState<string[]>([])
  const [pins, setPins] = useState<PinnedEntry[]>([])
  const [history, setHistory] = useState(false)
  const [lifecycle, setLifecycle] = useState<Lifecycle | null>(null)

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
      if (!response.ok) throw new Error(await readError(response))
      return response.json()
    }
    try {
      const [globalEntries, projectEntries, projectBoards, resolved, known, pinned, labels] = await Promise.all([
        read('/api/global/vault') as Promise<Entry[]>,
        read(`/api/p/${projectKey}/vault`) as Promise<Entry[]>,
        read(`/api/p/${projectKey}/boards`) as Promise<{ name: string; slug: string }[]>,
        read(`/api/p/${projectKey}/links/vault`) as Promise<EntryLink[]>,
        read('/api/templates') as Promise<TemplateInfo[]>,
        read(`/api/p/${projectKey}/pins`) as Promise<PinnedEntry[]>,
        read(`/api/p/${projectKey}/labels`) as Promise<{ name: string }[]>,
      ])
      setVault(globalEntries ?? [])
      setProject(projectEntries ?? [])
      setLinks(resolved ?? [])
      setTemplates(known ?? [])
      setBoards(projectBoards ?? [])
      setBoard(projectBoards?.[0]?.slug ?? null)
      setPins(pinned ?? [])
      setLabelOptions((labels ?? []).map((label) => label.name))
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not load the vault')
      setVault([]); setProject([])
    }
  }, [projectKey])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const readEntry = useCallback(async (target: string, signal?: AbortSignal) => {
    const response = await fetch(`/api/p/${projectKey}/vault/${encodeURIComponent(target)}`, { signal })
    if (!response.ok) throw new Error(await readError(response))
    return (await response.json()) as Entry
  }, [projectKey])

  useEffect(() => {
    if (!slug) return
    const controller = new AbortController()
    readEntry(slug, controller.signal)
      .then(setDetail)
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        toast.add({ title: `Could not open ${slug}`, description: err instanceof Error ? err.message : undefined, type: 'error' })
      })
    return () => controller.abort()
  }, [slug, readEntry])

  /** The list and the open entry, together, after anything changes them. */
  const reload = async () => {
    await load()
    if (slug) setDetail(await readEntry(slug))
  }

  // An entry a project promoted is listed by both routes — the global vault
  // holds it now, and the project can still find what it wrote — so the two
  // lists are merged by id, the global copy first, or the tree would count
  // and show it twice.
  const entries = useMemo(() => {
    const merged = new Map<string, Entry>()
    for (const item of [...(vault ?? []), ...(project ?? [])]) {
      if (!merged.has(item.id)) merged.set(item.id, item)
    }
    return [...merged.values()]
  }, [vault, project])
  const entry = useMemo(() => entries.find((item) => item.slug === slug), [entries, slug])
  // The list's facts with the detail's body and artifacts, once they are in.
  const shown = useMemo(
    () => (entry && detail?.slug === entry.slug ? { ...entry, ...detail } : undefined),
    [entry, detail],
  )
  // The entry's fields come from the full entry only: lists empty them on
  // private entries.
  const rows = useMemo(
    () => (shown ? fieldRows(templates.find((template) => template.name === shown.template), shown.fields) : []),
    [shown, templates],
  )
  const graph = useMemo(() => buildGraph(entries, links), [entries, links])
  // Where a wikilink in a body leads. The files spell links the way Obsidian
  // does, so reading an entry here follows them; a target nobody has written
  // yet is marked as the stub it is.
  const wikilinks = useMemo(
    () => ({
      slugs: new Set(entries.map((item) => item.slug)),
      pathOf: (target: string) => `/p/${projectKey}/vault/${encodeURIComponent(target)}`,
    }),
    [entries, projectKey],
  )
  const folders = useMemo(() => projectFolders(project ?? []), [project])
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
    (node: GraphNode) => navigate(`/p/${projectKey}/vault/${encodeURIComponent(node.slug)}`),
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
        `/api/p/${projectKey}/b/${board}/vault/${encodeURIComponent(entry.slug)}`,
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
        await reload()
        toast.add({
          title: 'Changed elsewhere',
          description: `${entry.slug} changed while you were editing and has been reloaded. Save again to replace that change, or cancel.`,
          type: 'error',
        })
        return
      }
      if (!response.ok) throw new Error(await readError(response))
      // A template that only warns lets the save through and says why.
      const saved = (await response.json()) as { warnings?: string[] }
      await reload()
      setEditing(false)
      if (saved.warnings?.length) {
        toast.add({ title: `Saved ${entry.slug}, with warnings`, description: warningText(saved.warnings), type: 'warning' })
      } else {
        toast.add({ title: `Saved ${entry.slug}`, type: 'success' })
      }
    } catch (err) {
      toast.add({
        title: 'Could not save the entry',
        description: err instanceof Error ? err.message : 'Unknown error',
        type: 'error',
      })
    } finally { setSaving(false) }
  }

  // Every write an entry supports, shared with whatever else asks for one.
  // A change to the facts carries the version on screen; the reload after it
  // advances the version, so an open edit still saves over it.
  const actions = useMemo(
    () => vaultActions({ projectKey: projectKey ?? '', board, refresh: reload }),
    // reload closes over the slug and the loader, both already dependencies here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [projectKey, board, slug, load],
  )

  /**
   * A change to the entry's facts, applied at once. Resolves null once saved,
   * or with the server's refusal.
   */
  const patchEntry = async (change: EntryPatch, done: string): Promise<Refusal | null> => {
    if (!entry) return { message: 'The entry is not loaded yet.', problems: [] }
    return actions.patch(entry.slug, entry.version, change, done)
  }

  const pinned = useMemo(() => pins.find((pin) => pin.slug === slug), [pins, slug])
  // Cards that cite this entry. Entries that link to it are already in the
  // graph, which the Linked group reads.
  const citations = useMemo(
    () => (detail && detail.slug === slug ? (detail.backlinks ?? []) : []).filter((link) => link.from_type === 'card'),
    [detail, slug],
  )

  /** One label or tag added or removed. PATCH replaces the list, so it is rebuilt here. */
  const changeChips = async (field: 'labels' | 'tags', current: string[], change: { add?: string; remove?: string }) => {
    const next = change.remove
      ? current.filter((value) => value !== change.remove)
      : change.add && !current.includes(change.add) ? [...current, change.add] : current
    const refusal = await patchEntry({ [field]: next }, change.add ? `Added ${change.add}` : `Removed ${change.remove}`)
    if (refusal) refused(`Could not change the ${field}`, refusal)
  }

  const changePrivate = async (next: boolean) => {
    const refusal = await patchEntry({ private: next }, next ? 'Marked private' : 'No longer private')
    if (refusal) refused('Could not change private', refusal)
  }

  const changeBoard = async (name: string) => {
    const board = name === NO_BOARD ? '' : name
    const refusal = await patchEntry({ board }, board ? `Associated with ${board}` : 'Board association removed')
    if (refusal) refused('Could not change the board', refusal)
  }

  // Deleting takes the file with the row, so the page steps back to the vault
  // rather than staying on an entry that is gone.
  const remove = async () => {
    if (!entry) return false
    const deleted = await actions.remove(entry.slug)
    if (deleted) navigate(`/p/${projectKey}/vault`)
    return deleted
  }

  const refused = (title: string, refusal: Refusal) => {
    toast.add({ title, description: refusalText(refusal), type: 'error' })
  }

  // A strict template the entry does not meet yet asks first; anything else
  // switches at once.
  const changeTemplate = async (name: string) => {
    if (!shown) return
    const target = templates.find((template) => template.name === name)
    if (target && switchNeedsDialog(target, shown)) {
      setSwitching(target)
      return
    }
    const refusal = await patchEntry({ template: name }, `${shown.slug} now follows ${templateLabel(name).toLowerCase()}`)
    if (refusal) refused(`Could not switch to ${templateLabel(name).toLowerCase()}`, refusal)
  }

  const switchTemplate = async (change: TemplateSwitch) => {
    const refusal = await patchEntry(change, `${entry?.slug} now follows ${templateLabel(change.template).toLowerCase()}`)
    if (!refusal) setSwitching(null)
    return refusal
  }

  const changeSources = async (sources: string[]) => {
    const refusal = await patchEntry({ sources }, 'Sources saved')
    if (refusal) refused('Could not change the sources', refusal)
    return refusal === null
  }

  // An empty value removes the field.
  const setField = async (name: string, value: string) => {
    const label = sentence(name)
    const refusal = await patchEntry({ set: { [name]: value } }, value ? `${label} saved` : `${label} removed`)
    if (refusal) refused(`Could not change ${label.toLowerCase()}`, refusal)
    return refusal === null
  }

  // A new entry opens for writing, cursor in the body, where the template's
  // sections already wait.
  const created = async (made: CreatedEntry) => {
    setCreatingIn(null)
    await load()
    setDetail(made)
    setFresh(made.slug)
    setEditingSlug(made.slug)
    setSource(false)
    navigate(`/p/${projectKey}/vault/${encodeURIComponent(made.slug)}`)
    if (made.warnings?.length) {
      toast.add({ title: `Created ${made.slug}, with warnings`, description: warningText(made.warnings), type: 'warning' })
    } else {
      toast.add({ title: `Created ${made.slug}`, type: 'success' })
    }
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
          <VaultNav
            entries={entries}
            vaultCount={vault.length}
            projectKey={projectKey}
            activeId={entry?.id}
            templates={templates.map((template) => template.name)}
            dock={dock}
            onOpenGraph={() => setExploring(true)}
            onOpenHealth={() => navigate(`/p/${projectKey}/health`)}
            onCreate={setCreatingIn}
          />
        }
      >
        <main className="min-w-0 px-6 pb-8 lg:h-full lg:overflow-y-auto lg:px-12">
          {error && (
            <Alert variant="destructive" className="mx-auto mt-8 max-w-measure">
              <AlertTitle>Could not load the vault</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {!error && !entry && (
            <Empty className="min-h-under-shell -mb-8 border-0">
              <EmptyHeader>
                <EmptyTitle>
                  {entries.length === 0 ? 'Nothing written yet' : slug ? 'No entry by that name' : 'Choose an entry'}
                </EmptyTitle>
                <EmptyDescription>
                  {entries.length === 0
                    ? 'Agents write entries here as they work.'
                    : slug
                      ? `Nothing in this project or the vault is called ${slug}.`
                      : `${entries.length} ${entries.length === 1 ? 'entry' : 'entries'} in this project and the vault.`}
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent className="flex-row justify-center">
                <Button variant="outline" size="sm" onClick={() => setCreatingIn('')}>
                  <FilePlus data-icon="inline-start" />
                  New entry
                </Button>
                {graph.nodes.length > 0 && (
                  <Button variant="outline" size="sm" onClick={() => setExploring(true)}>
                    <Network data-icon="inline-start" />
                    Open the graph
                  </Button>
                )}
              </EmptyContent>
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
                      <BreadcrumbLink render={<Link to={`/p/${projectKey}/vault`} />}>Vault</BreadcrumbLink>
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
                  <EntryMenu
                    slug={entry.slug}
                    href={`/p/${projectKey}/vault/${encodeURIComponent(entry.slug)}`}
                    global={Boolean(entry.global)}
                    pinned={pinned !== undefined}
                    onDelete={remove}
                    onHistory={() => setHistory(true)}
                    onPin={() => setLifecycle('pin')}
                    onUnpin={() => void actions.unpin(entry.slug)}
                    onPromote={() => setLifecycle('promote')}
                    onDemote={() => setLifecycle('demote')}
                    onVerify={() => setLifecycle('verify')}
                  />
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
                {shown ? (
                  <EntryView
                    entry={shown}
                    editing={editing}
                    initialFocus={shown.slug === fresh ? 'body' : 'title'}
                    source={source}
                    wikilinks={wikilinks}
                    onEditingChange={setEditing}
                    onSave={save}
                  />
                ) : (
                  // The title is already known; the body is on its way.
                  <div className="flex min-w-0 flex-col gap-3">
                    <h1 className="text-title text-balance">{entry.title}</h1>
                    <Skeleton className="mt-9 h-4 w-full" />
                    <Skeleton className="h-4 w-11/12" />
                    <Skeleton className="h-4 w-4/5" />
                  </div>
                )}

                {facts && (
                  <MetaPanel className="xl:sticky xl:top-16 xl:self-start">
                    {/* The template and the sources apply the moment they change,
                        editing or not, like a card's status. */}
                    <MetaGroup label="Template">
                      <TemplateSelect
                        templates={templates}
                        value={shown?.template ?? entry.template ?? ''}
                        disabled={!shown}
                        onChange={(name) => void changeTemplate(name)}
                      />
                    </MetaGroup>

                    <MetaGroup label="Sources" count={shown?.sources?.length || undefined} collapsible>
                      {shown ? (
                        <SourcesEditor
                          key={shown.slug}
                          sources={shown.sources ?? []}
                          projectKey={projectKey ?? ''}
                          onChange={changeSources}
                        />
                      ) : (
                        <Skeleton className="h-4 w-2/3" />
                      )}
                    </MetaGroup>

                    {rows.length > 0 && (
                      <MetaGroup label="Fields" collapsible>
                        <FieldsEditor key={shown?.slug} rows={rows} onSet={setField} />
                      </MetaGroup>
                    )}

                    {/* What the entry is filed under, and who may read it
                        automatically. All of it applies the moment it
                        changes, like the template above. */}
                    <MetaGroup label="Labels">
                      <ChipEditor
                        name="label"
                        values={shown?.labels ?? []}
                        options={labelOptions}
                        placeholder="Label"
                        disabledReason={shown ? undefined : 'The entry is still loading.'}
                        onChange={(change) => void changeChips('labels', shown?.labels ?? [], change)}
                      />
                    </MetaGroup>

                    <MetaGroup label="Tags">
                      <ChipEditor
                        name="tag"
                        values={shown?.tags ?? []}
                        placeholder="Tag"
                        disabledReason={shown ? undefined : 'The entry is still loading.'}
                        onChange={(change) => void changeChips('tags', shown?.tags ?? [], change)}
                      />
                    </MetaGroup>

                    <MetaGroup label="Private" collapsible>
                      <div className="flex items-start gap-3">
                        <Switch
                          checked={Boolean(shown?.private ?? entry.private)}
                          disabled={!shown}
                          aria-label="Private"
                          onCheckedChange={(next) => void changePrivate(next)}
                        />
                        <p className="text-xs text-pretty text-muted-foreground">
                          A private entry is never sent anywhere automatically: no recall
                          injection, no remote embedder. Reading it here is unaffected.
                        </p>
                      </div>
                    </MetaGroup>

                    {/* Association, never a claim: which board's work this
                        entry belongs with. A global entry has none. */}
                    {!entry.global && boards.length > 0 && (
                      <MetaGroup label="Board" collapsible>
                        <Select
                          items={[{ value: NO_BOARD, label: 'No board' }, ...boards.map((item) => ({ value: item.name, label: item.name }))]}
                          value={shown?.board || NO_BOARD}
                          onValueChange={(value) => { if (value) void changeBoard(value) }}
                        >
                          <SelectTrigger aria-label="Board" className="w-full" disabled={!shown}>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value={NO_BOARD}>No board</SelectItem>
                            {boards.map((item) => (
                              <SelectItem key={item.slug} value={item.name}>{item.name}</SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </MetaGroup>
                    )}

                    {pinned && (
                      <MetaGroup label="Pinned">
                        <p className="flex items-start gap-2 text-sm text-pretty">
                          <Pin aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                          <span>{pinned.recap}</span>
                        </p>
                        {pinned.stale && (
                          <p className="mt-2 flex items-center gap-2">
                            <Badge variant="outline">Stale</Badge>
                            <span className="text-xs text-muted-foreground">The entry changed after it was pinned.</span>
                          </p>
                        )}
                      </MetaGroup>
                    )}

                    <MetaGroup label="Details" collapsible>
                      <MetaFacts
                        facts={[
                          { label: 'Slug', value: entry.slug, mono: true, stacked: true },
                          { label: 'Scope', value: entry.global ? 'Global vault' : (projectKey ?? 'Project') },
                          ...(entry.private ? [{ label: 'Private', value: 'Yes' }] : []),
                          { label: 'Version', value: `v${entry.version}` },
                          { label: 'Created', value: when(entry.created_at) ?? 'Unknown' },
                          { label: 'Updated', value: when(entry.updated_at) ?? 'Unknown' },
                        ]}
                      />
                    </MetaGroup>

                    {shown?.artifacts && shown.artifacts.length > 0 && (
                      <MetaGroup label="Artifacts" count={shown.artifacts.length} collapsible>
                        <ArtifactList artifacts={shown.artifacts} />
                      </MetaGroup>
                    )}

                    <MetaGroup label="Linked" count={neighbours.length || undefined} collapsible>
                      {neighbours.length === 0 ? (
                        <p className="text-sm text-muted-foreground">
                          No links yet. A wikilink in this entry, or one pointing at it, appears here.
                        </p>
                      ) : (
                        <ul className="-mx-2 flex flex-col">
                          {neighbours.map((node) => (
                            <li key={node.id}>
                              {node.stub ? (
                                <p className="flex items-baseline justify-between gap-3 px-2 py-1.5" title="Not written yet">
                                  <span className="min-w-0 truncate text-meta text-muted-foreground">{node.slug}</span>
                                  <span className="shrink-0 text-xs text-muted-foreground">Stub</span>
                                </p>
                              ) : (
                                <Link
                                  to={`/p/${projectKey}/vault/${encodeURIComponent(node.slug)}`}
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

                    {/* What points here from outside the vault's own links:
                        the cards that cite this entry. */}
                    <MetaGroup label="Cited by" count={citations.length || undefined} collapsible>
                      {citations.length === 0 ? (
                        <p className="text-sm text-muted-foreground">No card cites this entry.</p>
                      ) : (
                        <ul className="-mx-2 flex flex-col">
                          {citations.map((citation) => (
                            <li key={`${citation.ref}${citation.anchor ?? ''}`}>
                              <Link
                                to={`/p/${projectKey}/card/${encodeURIComponent(citation.ref)}`}
                                className="flex flex-col gap-0.5 px-2 py-1.5 transition-colors hover:bg-accent/50"
                              >
                                <span className="truncate text-sm">{citation.title}</span>
                                <span className="text-meta text-muted-foreground">
                                  {citation.ref}{citation.anchor ? `#${citation.anchor}` : ''}
                                </span>
                              </Link>
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

      {projectKey && (
        <NewEntryDialog
          open={creatingIn !== null}
          projectKey={projectKey}
          board={board}
          templates={templates}
          folders={folders}
          dir={creatingIn ?? ''}
          onOpenChange={(open) => { if (!open) setCreatingIn(null) }}
          onCreated={(made) => void created(made)}
        />
      )}

      {projectKey && (
        <TemplateSwitchDialog
          template={switching}
          entry={shown ?? {}}
          projectKey={projectKey}
          editing={editing}
          onOpenChange={(open) => { if (!open) setSwitching(null) }}
          onSwitch={switchTemplate}
        />
      )}

      {entry && (
        <HistoryDialog
          open={history}
          title={entry.slug}
          base={`/api/p/${projectKey}/vault/${encodeURIComponent(entry.slug)}`}
          onOpenChange={setHistory}
        />
      )}

      {entry && (
        <LifecycleDialog
          act={lifecycle}
          slug={entry.slug}
          onOpenChange={(open) => { if (!open) setLifecycle(null) }}
          onConfirm={(act, values) => {
            switch (act) {
              case 'pin': return actions.pin(entry.slug, values.reason)
              case 'promote': return actions.promote(entry.slug, values.confirm, values.reason)
              case 'demote': return actions.demote(entry.slug, values.confirm, values.reason)
              case 'verify': return actions.verify(entry.slug, values.confirm)
            }
          }}
        />
      )}

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
