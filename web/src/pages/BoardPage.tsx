import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { useParams } from 'react-router-dom'
import {
  DndContext,
  DragOverlay,
  KeyboardCode,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  closestCenter,
  defaultDropAnimationSideEffects,
  getFirstCollision,
  pointerWithin,
  rectIntersection,
  useDroppable,
  useSensor,
  useSensors,
  type Announcements,
  type CollisionDetection,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
  type DropAnimation,
  type UniqueIdentifier,
} from '@dnd-kit/core'
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Plus, Upload } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Paged } from '@/components/wrappers/Paged'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/components/ui/toast'
import { Lamp } from '@/components/wrappers/Lamp'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { useLiveStatus } from '@/lib/live-status'
import { CardDialog } from '@/components/wrappers/CardDialog'
import { HistoryDialog } from '@/components/wrappers/HistoryDialog'
import { ImportCardsDialog } from '@/components/wrappers/ImportCardsDialog'
import { type CardInfo } from '@/components/wrappers/CardView'
import { PRIORITIES, PRIORITY_NUMBERS, shortActor, withDetail, type CardDetail } from '@/lib/cards'
import { cardActions } from '@/lib/card-actions'
import type { Artifact } from '@/components/wrappers/ArtifactList'
import type { CardComment, CardEvent } from '@/components/wrappers/CardTimeline'
import type { Entry } from '@/lib/entry'
import { sentence } from '@/lib/format'
import { cn } from '@/lib/utils'
import { readError } from '@/lib/api'

interface ColumnCardsInfo { name: string; cards: CardInfo[] }

/** Column name to the refs in it, in order: the board as the drag sees it. */
type Layout = Record<string, string[]>

const COLUMN = 'column:'

/** The filters' "no filter" value. A Select needs a value for "everything". */
const ALL = 'all'

function message(err: unknown) {
  return err instanceof Error ? err.message : 'Unknown error'
}

function reducedMotion() {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// A column reads urgent, high, normal, low; within a priority, the manual
// order holds. The server lists cards this way; the board keeps it while a
// card is in the hand.
const PRIORITY_WEIGHT: Record<string, number> = { urgent: 0, high: 1, normal: 2, low: 3 }

function weight(card?: CardInfo) {
  return PRIORITY_WEIGHT[card?.priority ?? 'normal'] ?? PRIORITY_WEIGHT.normal
}

function byPriority(refs: string[], cards: Map<string, CardInfo>) {
  // Array sort is stable, so equal priorities keep their manual order.
  return [...refs].sort((a, b) => weight(cards.get(a)) - weight(cards.get(b)))
}

/**
 * The card a move should be placed before. The server orders by rank, and
 * ranks interleave across priorities, so the anchor has to be the next card
 * of the same priority. With none, the card goes to the end of the rank order,
 * which is the end of its priority.
 */
function anchorAfter(order: string[], ref: string, cards: Map<string, CardInfo>) {
  const next = order[order.indexOf(ref) + 1]
  return next && weight(cards.get(next)) === weight(cards.get(ref)) ? next : ''
}

function layoutOf(columns: ColumnCardsInfo[]): Layout {
  const cards = new Map(columns.flatMap((column) => column.cards.map((card) => [card.ref, card] as const)))
  return Object.fromEntries(
    columns.map((column) => [column.name, byPriority(column.cards.map((card) => card.ref), cards)]),
  )
}

function columnOf(layout: Layout, id: UniqueIdentifier): string | undefined {
  const key = String(id)
  if (key.startsWith(COLUMN)) return key.slice(COLUMN.length)
  return Object.keys(layout).find((name) => layout[name].includes(key))
}

const DROP: DropAnimation = {
  duration: 220,
  easing: 'cubic-bezier(0.16, 1, 0.3, 1)',
  sideEffects: defaultDropAnimationSideEffects({ styles: { active: { opacity: '0' } } }),
}

export function BoardPage() {
  const { projectKey, boardSlug } = useParams<{ projectKey: string; boardSlug: string }>()
  const [columns, setColumns] = useState<ColumnCardsInfo[]>([])
  const cardOptions = useMemo(
    () => columns.flatMap((column) => column.cards.map((card) => ({ ref: card.ref, title: card.title }))),
    [columns],
  )
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [open, setOpen] = useState<CardInfo | null>(null)
  const [creating, setCreating] = useState(false)
  const [comments, setComments] = useState<CardComment[]>([])
  const [events, setEvents] = useState<CardEvent[]>([])
  const [saving, setSaving] = useState(false)
  const [labelOptions, setLabelOptions] = useState<string[]>([])
  const [entryOptions, setEntryOptions] = useState<Entry[]>([])
  const [storedArtifacts, setStoredArtifacts] = useState<Artifact[]>([])
  const [view, setView] = useState('board')
  // Filters narrow what the board shows without changing what it holds:
  // label and priority over the cards already loaded, archived by asking the
  // server for the other set.
  const [label, setLabel] = useState(ALL)
  const [priority, setPriority] = useState(ALL)
  const [importing, setImporting] = useState(false)
  const [history, setHistory] = useState<string | null>(null)
  // While a card is in the hand, the board renders this layout instead of the
  // server's, so the landing slot moves with the pointer across columns.
  const [preview, setPreview] = useState<Layout | null>(null)
  const [activeRef, setActiveRef] = useState<string | null>(null)
  const [overColumn, setOverColumn] = useState<string | null>(null)
  const origin = useRef<{ column: string; index: number } | null>(null)
  const lastOver = useRef<UniqueIdentifier | null>(null)
  const { setLive } = useLiveStatus()

  const base = `/api/p/${projectKey}/b/${boardSlug}`
  const archived = view === 'archived'

  const loadBoard = useCallback(async (signal?: AbortSignal) => {
    if (!projectKey || !boardSlug) return
    try {
      const response = await fetch(`${base}/cards${archived ? '?archived=1' : ''}`, { signal })
      if (!response.ok) throw new Error(await readError(response))
      setColumns(await response.json())
      setError(null)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(message(err))
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [projectKey, boardSlug, base, archived])

  useEffect(() => {
    if (!projectKey || !boardSlug) return
    const controller = new AbortController()
    void loadBoard(controller.signal)
    return () => controller.abort()
  }, [projectKey, boardSlug, loadBoard])

  useEffect(() => {
    if (!projectKey || !boardSlug) return
    const events = new EventSource(`${base}/events`)
    events.addEventListener('changed', () => { void loadBoard() })
    events.onopen = () => setLive('connected')
    events.onerror = () => setLive('disconnected')
    return () => { events.close(); setLive('unknown') }
  }, [projectKey, boardSlug, base, loadBoard, setLive])

  // The filters are read over the cards the server sent: a board is a page of
  // work, not a query, so narrowing it needs no round trip.
  const visible = useMemo(
    () => columns.map((column) => ({
      name: column.name,
      cards: column.cards.filter((card) =>
        (label === ALL || (card.labels ?? []).includes(label)) &&
        (priority === ALL || card.priority === priority)),
    })),
    [columns, label, priority],
  )
  const filtering = label !== ALL || priority !== ALL
  const byRef = useMemo(() => new Map(visible.flatMap((column) => column.cards.map((card) => [card.ref, card]))), [visible])
  const layout = useMemo(() => preview ?? layoutOf(visible), [preview, visible])
  const shown = useMemo(
    () => visible.map((column) => ({
      name: column.name,
      cards: (layout[column.name] ?? []).flatMap((ref) => byRef.get(ref) ?? []),
    })),
    [visible, layout, byRef],
  )

  const columnNames = useMemo(() => columns.map((column) => column.name), [columns])
  const openColumn = open ? columnOf(layoutOf(columns), open.ref) : undefined
  const activeCard = activeRef ? byRef.get(activeRef) : undefined

  const sensors = useSensors(
    // A small distance keeps a click from registering as a drag, so opening a
    // card to read it still works with a mouse. Touch waits for a press, so a
    // swipe across the board still scrolls it.
    useSensor(MouseSensor, { activationConstraint: { distance: 6 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 180, tolerance: 6 } }),
    // Space picks a card up; Enter stays free to open it.
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
      keyboardCodes: {
        start: [KeyboardCode.Space],
        cancel: [KeyboardCode.Esc],
        end: [KeyboardCode.Space, KeyboardCode.Enter],
      },
    }),
  )

  // Pointer first, so the column under the cursor wins over a card that merely
  // overlaps. Inside a column with cards, the nearest card is the target, so
  // the slot follows the pointer rather than jumping to the end.
  const collision: CollisionDetection = useCallback((args) => {
    const hits = pointerWithin(args)
    let overId = getFirstCollision(hits.length > 0 ? hits : rectIntersection(args), 'id')
    if (overId == null) return lastOver.current ? [{ id: lastOver.current }] : []
    const current = preview ?? layoutOf(visible)
    const key = String(overId)
    if (key.startsWith(COLUMN)) {
      const refs = current[key.slice(COLUMN.length)] ?? []
      if (refs.length > 0) {
        const nearest = closestCenter({
          ...args,
          droppableContainers: args.droppableContainers.filter((container) => refs.includes(String(container.id))),
        })
        overId = nearest[0]?.id ?? overId
      }
    }
    lastOver.current = overId
    return [{ id: overId }]
  }, [preview, visible])

  const openCard = async (card: CardInfo) => {
    setOpen(card)
    setComments([])
    setEvents([])
    try {
      const response = await fetch(`${base}/cards/${encodeURIComponent(card.ref)}`)
      if (response.ok) {
        const detail = (await response.json()) as CardDetail
        setComments(detail.comments ?? [])
        setEvents(detail.events ?? [])
      }
    } catch {
      /* the card reads fine without its comments */
    }
  }

  const refreshDetail = async (ref: string) => {
    const response = await fetch(`${base}/cards/${encodeURIComponent(ref)}`)
    if (!response.ok) return
    const detail = (await response.json()) as CardDetail
    // A move or a priority change bumps the version, and the next save of
    // the words has to send the new one. Only the card still open is replaced.
    setOpen((current) => (current?.ref === ref ? withDetail(detail) : current))
    setComments(detail.comments ?? [])
    setEvents(detail.events ?? [])
  }

  // A claim this person holds is theirs to edit, so the page needs to know
  // which principal the server writes as.
  const [me, setMe] = useState<string>()
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/me', { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : null))
      .then((who: { actor: string } | null) => { if (who) setMe(who.actor) })
      .catch(() => { /* without it every claim simply reads as someone else's */ })
    return () => controller.abort()
  }, [])

  // What a card can point at: the project's labels, which are its own
  // vocabulary and never invented; the entries it could cite; and the files
  // already stored. Each is an offer, so failing to read one leaves the rest
  // working.
  useEffect(() => {
    if (!projectKey) return
    const controller = new AbortController()
    const signal = controller.signal
    const read = async <T,>(url: string, fallback: T): Promise<T> => {
      try {
        const response = await fetch(url, { signal })
        return response.ok ? ((await response.json()) as T) : fallback
      } catch {
        return fallback
      }
    }
    void (async () => {
      const [labels, project, vault, artifacts] = await Promise.all([
        read<{ name: string }[]>(`/api/p/${projectKey}/labels`, []),
        read<Entry[]>(`/api/p/${projectKey}/vault`, []),
        read<Entry[]>('/api/global/vault', []),
        read<Artifact[]>(`/api/p/${projectKey}/artifacts`, []),
      ])
      if (signal.aborted) return
      setLabelOptions(labels.map((label) => label.name))
      setEntryOptions([...project, ...vault])
      setStoredArtifacts(artifacts)
    })()
    return () => controller.abort()
  }, [projectKey])

  // Every write a card supports, shared with the card page. The board says
  // what to bring up to date: its columns always, and the open card's detail
  // when one is open.
  const actions = useMemo(
    () => cardActions({
      base,
      refresh: async (ref: string) => {
        await loadBoard()
        if (ref) await refreshDetail(ref)
      },
    }),
    // refreshDetail closes over nothing that changes between renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [base, loadBoard],
  )

  const createCard = async (draft: { title: string; body: string; priority: string; column?: string; labels: string[]; tags: string[] }) => {
    setSaving(true)
    try {
      const response = await fetch(`${base}/cards`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...draft, priority: PRIORITY_NUMBERS[draft.priority as (typeof PRIORITIES)[number]] }),
      })
      if (!response.ok) throw new Error(await readError(response))
      setCreating(false)
      await loadBoard()
    } catch (err) {
      toast.add({ title: 'Could not create card', description: message(err), type: 'error' })
    } finally { setSaving(false) }
  }

  /** Resolves true once the card is saved, so the dialog can go back to reading. */
  const saveCard = async (edit: { title: string; body: string }) => {
    if (!open) return false
    setSaving(true)
    const saved = await actions.save(open, edit)
    setSaving(false)
    return saved
  }

  const deleteCard = async () => {
    if (!open) return false
    const deleted = await actions.remove(open.ref)
    if (deleted) {
      setOpen(null)
      setComments([])
      setEvents([])
      await loadBoard()
    }
    return deleted
  }

  const archiveCard = async (shelve: boolean) => {
    if (!open) return false
    const done = await actions.archive(open.ref, shelve)
    // An archived card is no longer on the board it was read from, so the
    // dialog closes with it.
    if (done && shelve !== archived) setOpen(null)
    return done
  }

  const endDrag = () => {
    setPreview(null)
    setActiveRef(null)
    setOverColumn(null)
    origin.current = null
    lastOver.current = null
  }

  const onDragStart = ({ active }: DragStartEvent) => {
    const start = layoutOf(visible)
    const column = columnOf(start, active.id)
    if (!column) return
    origin.current = { column, index: start[column].indexOf(String(active.id)) }
    setPreview(start)
    setActiveRef(String(active.id))
    setOverColumn(column)
  }

  // Crossing into another column moves the slot there immediately; ordering
  // inside a column is left to the sortable strategy until the drop.
  const onDragOver = ({ active, over }: DragOverEvent) => {
    if (!over) return
    const ref = String(active.id)
    const below = active.rect.current.translated
      ? active.rect.current.translated.top > over.rect.top + over.rect.height / 2
      : false
    setOverColumn(columnOf(layout, over.id) ?? null)
    setPreview((current) => {
      if (!current) return current
      const from = columnOf(current, active.id)
      const to = columnOf(current, over.id)
      if (!from || !to || from === to) return current
      const target = current[to]
      const overIndex = target.indexOf(String(over.id))
      const index = overIndex < 0 ? target.length : overIndex + (below ? 1 : 0)
      return {
        ...current,
        [from]: current[from].filter((item) => item !== ref),
        [to]: byPriority([...target.slice(0, index), ref, ...target.slice(index)], byRef),
      }
    })
  }

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    const start = origin.current
    const current = preview
    endDrag()
    if (!over || !current || !start) return

    const ref = String(active.id)
    const column = columnOf(current, active.id)
    const overColumnName = columnOf(current, over.id)
    if (!column || column !== overColumnName) return

    let order = current[column]
    const from = order.indexOf(ref)
    const to = order.indexOf(String(over.id))
    if (to >= 0 && from !== to) order = arrayMove(order, from, to)
    // A drop outside the card's priority settles at the nearest end of it.
    order = byPriority(order, byRef)
    const index = order.indexOf(ref)
    if (column === start.column && index === start.index) return

    const before = anchorAfter(order, ref, byRef)
    const next = { ...current, [column]: order }
    setColumns((existing) =>
      existing.map((item) => ({
        ...item,
        cards: (next[item.name] ?? []).flatMap((id) => byRef.get(id) ?? []),
      })),
    )
    void actions.move(ref, column, before)
  }

  const announcements: Announcements = {
    onDragStart: ({ active }) => {
      const card = byRef.get(String(active.id))
      return `Picked up ${active.id}${card ? `, ${card.title}` : ''}.`
    },
    onDragOver: ({ active, over }) => {
      if (!over || !preview) return undefined
      const column = columnOf(preview, over.id)
      if (!column) return undefined
      const position = preview[column].indexOf(String(active.id)) + 1
      return `${active.id} is over ${column}${position > 0 ? `, position ${position} of ${preview[column].length}` : ''}.`
    },
    onDragEnd: ({ active, over }) => {
      const column = over && preview ? columnOf(preview, over.id) : undefined
      return column ? `${active.id} dropped in ${column}.` : `${active.id} was not moved.`
    },
    onDragCancel: ({ active }) => `Cancelled. ${active.id} is back where it was.`,
  }

  if (loading && columns.length === 0) return <LoadingBoard />

  // The board is a fixed frame under the shell: the header stays, the column
  // strip scrolls sideways when the columns outgrow the screen, and each
  // column scrolls its own cards. The page itself never scrolls.
  return (
    <main className="flex h-under-shell flex-col px-6 lg:px-8">
      <Tabs value={view} onValueChange={(next) => setView(next ?? 'board')} className="min-h-0 flex-1 gap-3">
        <PageHeader
          title={boardSlug ?? 'Board'}
          facts={filtering ? [{ label: 'Shown', value: `${visible.reduce((total, column) => total + column.cards.length, 0)} of ${columns.reduce((total, column) => total + column.cards.length, 0)}` }] : []}
          actions={
            <>
              <BoardFilters
                labels={labelOptions}
                label={label}
                priority={priority}
                onLabel={setLabel}
                onPriority={setPriority}
              />
              <TabsList>
                <TabsTrigger value="board">Board</TabsTrigger>
                <TabsTrigger value="list">List</TabsTrigger>
                <TabsTrigger value="archived">Archived</TabsTrigger>
              </TabsList>
              <Button variant="outline" size="sm" onClick={() => setImporting(true)}>
                <Upload data-icon="inline-start" />
                Import
              </Button>
              <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
                <Plus data-icon="inline-start" />
                New card
              </Button>
            </>
          }
        />

        {error && (
          <Alert variant="destructive">
            <AlertTitle>Could not load the board</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <TabsContent value="board" className="min-h-0">
          <DndContext
            sensors={sensors}
            collisionDetection={collision}
            accessibility={{
              announcements,
              screenReaderInstructions: {
                draggable:
                  'Press Enter to open this card. To move it, press Space, use the arrow keys, then press Space again to drop it or Escape to cancel.',
              },
            }}
            onDragStart={onDragStart}
            onDragOver={onDragOver}
            onDragEnd={onDragEnd}
            onDragCancel={endDrag}
          >
            <div className="flex h-full scroll-fade-x gap-3 overflow-x-auto overscroll-x-contain pb-6">
              {shown.map((column) => (
                <BoardColumn
                  key={column.name}
                  column={column}
                  dragging={activeRef !== null}
                  over={overColumn === column.name}
                  openRef={open?.ref}
                  onOpen={openCard}
                />
              ))}
            </div>
            <DragOverlay dropAnimation={reducedMotion() ? null : DROP}>
              {activeCard ? <CardTile card={activeCard} lifted /> : null}
            </DragOverlay>
          </DndContext>
        </TabsContent>

        {/* Archived cards read as a list: they are a shelf, not work in
            motion, and nothing about them is dragged. */}
        <TabsContent value="archived" className="min-h-0 scroll-fade-y overflow-y-auto">
          <CardRows columns={visible} openRef={open?.ref} onOpen={openCard} empty="Nothing archived" />
        </TabsContent>

        <TabsContent value="list" className="min-h-0 scroll-fade-y overflow-y-auto">
          <CardRows columns={visible} openRef={open?.ref} onOpen={openCard} empty="No cards" />
        </TabsContent>
      </Tabs>


      <CardDialog
        open={creating || open !== null}
        card={creating ? null : open}
        comments={creating ? [] : comments}
        events={creating ? [] : events}
        columns={columnNames}
        currentColumn={openColumn}
        startIn={creating ? 'create' : 'read'}
        saving={saving}
        onOpenChange={(next) => { if (!next) { setCreating(false); setOpen(null); setComments([]); setEvents([]) } }}
        onSave={saveCard}
        onCreate={createCard}
        actions={actions}
        labelOptions={labelOptions}
        entryOptions={entryOptions}
        storedArtifacts={storedArtifacts}
        me={me}
        cardOptions={cardOptions}
        onDelete={deleteCard}
        onArchive={archiveCard}
        onHistory={() => { if (open) setHistory(open.ref) }}
      />

      <ImportCardsDialog
        open={importing}
        onOpenChange={setImporting}
        onImport={actions.importCards}
      />

      {history && (
        <HistoryDialog
          open
          title={history}
          base={`/api/p/${projectKey}/cards/${encodeURIComponent(history)}`}
          onOpenChange={(next) => { if (!next) setHistory(null) }}
        />
      )}
    </main>
  )
}

/**
 * One column. The whole column is the drop target, not just its cards, so a
 * drop below the last card or into an empty column lands. While a card is in
 * the hand every column shows it can take it, and the one under the pointer
 * says so more strongly.
 */
function BoardColumn({
  column,
  dragging,
  over,
  openRef,
  onOpen,
}: {
  column: ColumnCardsInfo
  dragging: boolean
  over: boolean
  openRef?: string
  onOpen: (card: CardInfo) => void
}) {
  const { setNodeRef } = useDroppable({ id: `${COLUMN}${column.name}` })
  const claimed = column.cards.filter((card) => card.claimed_by).length
  const refs = useMemo(() => column.cards.map((card) => card.ref), [column.cards])

  return (
    <section
      ref={setNodeRef}
      aria-label={`${column.name}, ${column.cards.length} cards`}
      data-dragging={dragging || undefined}
      data-over={over || undefined}
      className="flex h-full w-72 shrink-0 flex-col bg-muted/50 transition-colors duration-150 data-over:bg-accent xl:w-auto xl:min-w-64 xl:flex-1"
    >
      <header className="flex h-11 shrink-0 items-center gap-2 px-3.5">
        <h2 className="text-label">{sentence(column.name)}</h2>
        {claimed > 0 && <span className="text-xs text-claimed">{claimed} claimed</span>}
        <span className="ml-auto text-xs text-muted-foreground">{column.cards.length}</span>
      </header>

      <SortableContext items={refs} strategy={verticalListSortingStrategy}>
        <ul className="flex min-h-0 flex-1 scroll-fade-y flex-col gap-2 overflow-y-auto overscroll-y-contain px-2 pb-2">
          {column.cards.map((card) => (
            <li key={card.id}>
              <SortableCard card={card} selected={card.ref === openRef} onOpen={onOpen} />
            </li>
          ))}
          {column.cards.length === 0 && (dragging ? (
            // The slot only exists while something can land in it.
            <li
              className={cn(
                'flex h-24 animate-enter items-center justify-center border border-dashed border-rule-strong text-xs text-muted-foreground transition-colors',
                over && 'border-foreground text-foreground',
              )}
            >
              Drop here
            </li>
          ) : (
            <li className="px-1.5 py-2 text-xs text-muted-foreground">No cards</li>
          ))}
        </ul>
      </SortableContext>
    </section>
  )
}

/**
 * A card that can be picked up. While it is in the hand its place in the
 * column becomes the landing slot: a dashed outline of the same height, so
 * the drop target is visible before the drop.
 *
 * A claimed card does not drag: the claim is the concurrency contract, and a move
 * the server would refuse should not look available. Trying anyway says why.
 */
function SortableCard({
  card,
  selected,
  onOpen,
}: {
  card: CardInfo
  selected: boolean
  onOpen: (card: CardInfo) => void
}) {
  const locked = Boolean(card.claimed_by)
  // Claimed cards stay drop targets: a card may be placed beside one, and the
  // server re-ranks the column without touching the claim.
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: card.ref,
    disabled: { draggable: locked, droppable: false },
  })
  const refused = useRef<{ x: number; y: number; told: boolean } | null>(null)
  const [shake, setShake] = useState(0)

  const tryLocked = {
    onPointerDown: (event: ReactPointerEvent) => { refused.current = { x: event.clientX, y: event.clientY, told: false } },
    onPointerMove: (event: ReactPointerEvent) => {
      const start = refused.current
      if (!start || start.told || event.buttons === 0) return
      if (Math.hypot(event.clientX - start.x, event.clientY - start.y) < 6) return
      start.told = true
      setShake((count) => count + 1)
      toast.add({
        title: `${card.ref} is claimed by ${shortActor(card.claimed_by)}`,
        description: 'Open it and steal the claim before moving it.',
      })
    },
    // A refused drag still ends in a click on the same card; it should not
    // also open the card, because the reader was trying to move it.
    onClickCapture: (event: ReactMouseEvent) => {
      if (refused.current?.told) {
        event.stopPropagation()
        event.preventDefault()
      }
      refused.current = null
    },
  }

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className="relative"
    >
      <CardTile
        key={shake}
        card={card}
        selected={selected}
        placeholder={isDragging}
        refused={shake > 0}
        onOpen={onOpen}
        {...(locked ? tryLocked : { ...attributes, ...listeners })}
      />
    </div>
  )
}

/**
 * The face of a card, shared by the column, the slot and the card in the hand.
 * State is told once: the lamp and the ref colour carry it, not a stripe.
 */
function CardTile({
  card,
  selected = false,
  placeholder = false,
  lifted = false,
  refused = false,
  onOpen,
  ...handlers
}: {
  card: CardInfo
  selected?: boolean
  placeholder?: boolean
  lifted?: boolean
  refused?: boolean
  onOpen?: (card: CardInfo) => void
} & React.HTMLAttributes<HTMLButtonElement>) {
  const locked = Boolean(card.claimed_by)
  const urgent = card.priority === 'urgent'
  const actor = shortActor(card.claimed_by)

  return (
    <Button
      variant="ghost"
      onClick={() => onOpen?.(card)}
      aria-current={selected ? 'true' : undefined}
      aria-roledescription={locked ? 'claimed card' : 'draggable card'}
      title={locked ? `Claimed by ${actor}. Steal the claim to move it.` : undefined}
      {...handlers}
      className={cn(
        // No outline: a tile sits on its column by its shadow. The background
        // runs under the transparent border, so no hairline of column shows
        // between the card and its shadow.
        'h-auto w-full flex-col items-stretch justify-start gap-0 bg-tile bg-clip-border p-3 text-left whitespace-normal shadow-tile hover:bg-tile hover:shadow-tile-raised active:not-aria-[haspopup]:translate-y-0',
        locked ? 'cursor-pointer' : 'cursor-grab',
        selected && 'border-foreground',
        placeholder && 'border-dashed border-rule-strong bg-transparent shadow-none hover:bg-transparent hover:shadow-none *:invisible',
        lifted && 'scale-102 rotate-1 cursor-grabbing shadow-lift hover:shadow-lift',
        refused && 'animate-refuse',
      )}
    >
      <span className="block text-sm leading-snug text-foreground">{card.title}</span>
      {/* The words a card is filed under, above the facts line: a label is
          the project's own vocabulary, a tag is anyone's, so a label reads on
          a surface and a tag only in outline. */}
      {(card.labels?.length || card.tags?.length) ? (
        <span className="mt-2 flex w-full flex-wrap items-center gap-1">
          {card.labels?.map((name) => (
            <Badge key={`label-${name}`} variant="secondary" className="max-w-full font-normal">
              <span className="truncate">{name}</span>
            </Badge>
          ))}
          {card.tags?.map((name) => (
            <Badge key={`tag-${name}`} variant="outline" className="max-w-full font-normal text-muted-foreground">
              <span className="truncate">{name}</span>
            </Badge>
          ))}
        </span>
      ) : null}
      <span className="mt-2.5 flex w-full flex-wrap items-center gap-x-2 gap-y-1">
        <Lamp state={urgent ? 'alarm' : locked ? 'claimed' : 'idle'} />
        <span className="text-meta text-muted-foreground">{card.ref}</span>
        {card.priority !== 'normal' && (
          <span className={cn('text-xs', urgent ? 'text-danger' : 'text-muted-foreground')}>
            {sentence(card.priority)}
          </span>
        )}
        {actor && (
          <span className="ml-auto text-xs text-claimed">
            Claimed by <span className="text-meta">{actor}</span>
          </span>
        )}
      </span>
    </Button>
  )
}

function LoadingBoard() {
  return (
    <main className="flex h-under-shell flex-col px-6 lg:px-8">
      <div className="flex min-h-16 items-center py-4">
        <Skeleton className="h-7 w-48" />
      </div>
      <div className="mt-3 flex min-h-0 flex-1 gap-3 overflow-hidden pb-6">
        {[0, 1, 2, 3].map((column) => (
          <div key={column} className="flex w-72 shrink-0 flex-col gap-2 bg-muted/50 p-2 xl:w-auto xl:min-w-64 xl:flex-1">
            <Skeleton className="m-1.5 h-4 w-24" />
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-20 w-full" />
          </div>
        ))}
      </div>
    </main>
  )
}

/**
 * Cards as rows, grouped by column: the shape the list and the archived shelf
 * share. A row opens the card; nothing here is dragged, so the rows carry the
 * facts a tile shows in its footer instead.
 */
function CardRows({ columns, openRef, onOpen, empty }: {
  columns: ColumnCardsInfo[]
  openRef?: string
  onOpen: (card: CardInfo) => void
  empty: string
}) {
  return (
    <div className="flex flex-col gap-10 pb-10">
      {columns.map((column) => (
        <section key={column.name}>
          <h2 className="flex items-baseline gap-3 text-heading">
            {sentence(column.name)}
            <span className="text-xs font-normal text-muted-foreground">{column.cards.length}</span>
          </h2>
          {column.cards.length === 0 ? (
            <p className="mt-3 text-sm text-muted-foreground">{empty}</p>
          ) : (
            <Paged items={column.cards} label={`${column.name} pages`}>
              {(page) => (
                <Table className="mt-3">
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-7" aria-label="State" />
                      <TableHead className="w-28">Ref</TableHead>
                      <TableHead>Card</TableHead>
                      <TableHead className="w-32">Claimed by</TableHead>
                      <TableHead className="w-24">Priority</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {page.map((card) => (
                      <TableRow
                        key={card.id}
                        data-state={card.ref === openRef ? 'selected' : undefined}
                        className="cursor-pointer"
                        onClick={() => onOpen(card)}
                      >
                        <TableCell>
                          <Lamp state={card.priority === 'urgent' ? 'alarm' : card.claimed_by ? 'claimed' : 'idle'} />
                        </TableCell>
                        <TableCell className="text-meta text-muted-foreground">{card.ref}</TableCell>
                        <TableCell>{card.title}</TableCell>
                        <TableCell className={cn('text-meta', card.claimed_by ? 'text-claimed' : 'text-muted-foreground')}>
                          {shortActor(card.claimed_by) ?? 'none'}
                        </TableCell>
                        <TableCell className={cn('text-xs', card.priority === 'urgent' ? 'text-danger' : 'text-muted-foreground')}>
                          {sentence(card.priority)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </Paged>
          )}
        </section>
      ))}
    </div>
  )
}

/**
 * What the board is showing, narrowed: one of the project's labels, and one
 * priority. Both read over the cards already loaded, so they answer at once;
 * "All" is the absence of a filter rather than a value.
 */
function BoardFilters({ labels, label, priority, onLabel, onPriority }: {
  labels: string[]
  label: string
  priority: string
  onLabel: (label: string) => void
  onPriority: (priority: string) => void
}) {
  const labelItems = [{ value: ALL, label: 'All labels' }, ...labels.map((name) => ({ value: name, label: name }))]
  const priorityItems = [
    { value: ALL, label: 'All priorities' },
    ...PRIORITIES.map((value) => ({ value, label: sentence(value) })),
  ]
  return (
    <>
      {labels.length > 0 && (
        <Select items={labelItems} value={label} onValueChange={(value) => { if (value) onLabel(value) }}>
          <SelectTrigger size="sm" aria-label="Filter by label" className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {labelItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectContent>
        </Select>
      )}
      <Select items={priorityItems} value={priority} onValueChange={(value) => { if (value) onPriority(value) }}>
        <SelectTrigger size="sm" aria-label="Filter by priority" className="w-36">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {priorityItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
        </SelectContent>
      </Select>
    </>
  )
}
