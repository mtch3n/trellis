import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import type { ProjectSummary } from '@/components/wrappers/AppShell'
import { shortActor } from '@/lib/cards'
import { type CardInfo } from '@/components/wrappers/CardView'
import { Lamp } from '@/components/wrappers/Lamp'
import { MetaGroup } from '@/components/wrappers/MetaPanel'
import { ProjectTimeline } from '@/components/wrappers/ProjectTimeline'
import { PageHeader } from '@/components/wrappers/PageHeader'
import type { KnowledgeEntry } from '@/pages/KnowledgePage'
import { ago, sentence } from '@/lib/format'
import { buildMarks, type ProjectEvent } from '@/lib/timeline-marks'
import { cn } from '@/lib/utils'

interface ColumnCards { name: string; cards: CardInfo[] }
interface Event {
  seq: number
  timestamp: number
  actor: string
  entity_type: string
  action: string
  title: string
}

interface Snapshot {
  summary: ProjectSummary | null
  columns: ColumnCards[]
  events: Event[]
  entries: KnowledgeEntry[]
  history: ProjectEvent[]
  loadedAt: number
}

/** The events endpoint's largest page. */
const HISTORY_PAGE = 5000

/**
 * The project at a glance, and where the app lands. PRODUCT.md makes
 * orientation after absence the primary job, so this page answers "what is
 * happening here" for someone who has forgotten: what is moving and who holds
 * it, what changed last, and what was written down.
 *
 * The first column is the queue and the done column is history; everything
 * between them is work in motion, so those columns get sections of their own
 * and the rest is counted in the rail.
 */
export function OverviewPage() {
  const { projectKey } = useParams<{ projectKey: string }>()
  const [data, setData] = useState<Snapshot | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!projectKey) return
    const controller = new AbortController()
    const { signal } = controller
    const read = async <T,>(url: string): Promise<T> => {
      const response = await fetch(url, { signal })
      if (!response.ok) throw new Error(await response.text())
      return response.json() as Promise<T>
    }
    ;(async () => {
      const projects = await read<ProjectSummary[]>('/api/projects')
      const summary = projects.find((project) => project.key === projectKey) ?? null
      const board = summary?.boards[0]?.slug
      // The whole history, a page at a time, for the timeline.
      const readHistory = async () => {
        const history: ProjectEvent[] = []
        for (let after = 0; ;) {
          const page = await read<{ events: ProjectEvent[]; next: number | null }>(
            `/api/p/${projectKey}/events?after=${after}&limit=${HISTORY_PAGE}`,
          )
          history.push(...page.events)
          if (page.events.length < HISTORY_PAGE || page.next === null) return history
          after = page.next
        }
      }
      const [columns, events, entries, history] = await Promise.all([
        board ? read<ColumnCards[]>(`/api/p/${projectKey}/b/${board}/cards`) : Promise.resolve([]),
        read<Event[]>(`/api/activity?project=${encodeURIComponent(projectKey)}&limit=20`),
        read<KnowledgeEntry[]>(`/api/p/${projectKey}/knowledge`),
        readHistory(),
      ])
      setData({ summary, columns, events, entries: entries ?? [], history, loadedAt: Date.now() })
      setError(null)
    })().catch((err: unknown) => {
      if (signal.aborted) return
      setError(err instanceof Error ? err.message : 'Could not load the project')
    })
    return () => controller.abort()
  }, [projectKey])

  const view = useMemo(() => {
    if (!data) return null
    const doneNames = new Set(data.summary?.columns.filter((column) => column.is_done).map((column) => column.name))
    const moving = data.columns.filter((column, index) => index > 0 && !doneNames.has(column.name))
    const cards = data.columns.flatMap((column) => column.cards)
    const titles = new Map(
      data.entries.map((entry) => [`${entry.global ? 'GLOBAL' : projectKey}/${entry.slug}`, entry.title]),
    )
    const marks = buildMarks({
      cards: cards.flatMap((card) =>
        card.created_at === undefined
          ? []
          : [{ ref: card.ref, title: card.title, createdAt: card.created_at, owner: card.owner }],
      ),
      events: data.history,
      titles,
      queue: data.columns[0]?.name ?? '',
      doneColumns: doneNames,
    })
    return {
      marks,
      moving,
      total: cards.length,
      held: cards.filter((card) => card.owner).length,
      urgent: cards.filter((card) => card.priority === 'urgent' && !doneNames.has(columnOf(data.columns, card.ref))),
      pinned: data.entries.filter((entry) => entry.recap),
      recent: [...data.entries].sort((a, b) => (b.updated_at ?? 0) - (a.updated_at ?? 0)).slice(0, 5),
      board: data.summary?.boards[0]?.slug,
    }
  }, [data, projectKey])

  if (error) {
    return (
      <main className="mx-auto max-w-overview px-6 pb-24 lg:px-8">
        <PageHeader title={projectKey ?? 'Overview'} />
        <Alert variant="destructive" className="mt-4">
          <AlertTitle>Could not load the project</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      </main>
    )
  }

  if (!data || !view) return <LoadingOverview />

  const base = `/p/${projectKey}`
  const stale = data.summary?.stale_leases ?? 0

  if (view.total === 0 && data.entries.length === 0) {
    return (
      <main className="mx-auto max-w-overview px-6 pb-24 lg:px-8">
        <PageHeader title={projectKey} />
        <Empty className="mt-16">
          <EmptyHeader>
            <EmptyTitle>Nothing here yet</EmptyTitle>
            <EmptyDescription>Agents create cards and write entries as they work. They show up here.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </main>
    )
  }

  return (
    <main className="mx-auto max-w-overview animate-enter px-6 pb-24 lg:px-8">
      <PageHeader
        title={projectKey}
        facts={[
          { label: 'Cards', value: view.total },
          { label: 'Held', value: view.held, tone: 'held' },
          { label: 'Stale leases', value: stale, tone: 'held' },
          { label: 'Entries', value: data.entries.length },
        ]}
      />

      {view.marks.created.length + view.marks.written.length > 0 && (
        <div className="mt-6">
          <ProjectTimeline marks={view.marks} base={base} now={data.loadedAt} />
        </div>
      )}

      <div className="mt-12 grid items-start gap-x-14 gap-y-12 lg:grid-cols-overview">
        <div className="flex min-w-0 flex-col gap-12">
          {view.urgent.length > 0 && (
            <CardSection title="Urgent" cards={view.urgent} base={base} empty="" />
          )}

          {view.moving.map((column) => (
            <CardSection
              key={column.name}
              title={sentence(column.name)}
              cards={column.cards}
              base={base}
              empty="No cards."
            />
          ))}

          <section>
            <h2 className="text-heading">Recent activity</h2>
            {data.events.length === 0 ? (
              <p className="mt-3 text-sm text-muted-foreground">No activity yet.</p>
            ) : (
              <ol className="mt-3 flex flex-col">
                {data.events.map((event) => (
                  <li key={event.seq} className="flex items-baseline gap-4 py-1.5">
                    <span className="w-20 shrink-0 text-xs text-muted-foreground">{ago(event.timestamp)}</span>
                    <span className="w-20 shrink-0 truncate text-meta text-muted-foreground">
                      {shortActor(event.actor) ?? event.actor}
                    </span>
                    <span className="w-24 shrink-0 text-xs text-muted-foreground">{sentence(event.action)}</span>
                    <span className="min-w-0 truncate text-sm">{event.title}</span>
                  </li>
                ))}
              </ol>
            )}
            <Link
              to="/activity"
              className="mt-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              All activity
              <ArrowRight className="size-3.5" />
            </Link>
          </section>
        </div>

        <aside className="flex flex-col gap-8 lg:sticky lg:top-shell-gap">
          <MetaGroup label="Board" count={view.total}>
            <dl className="flex flex-col gap-2">
              {data.columns.map((column) => (
                <div key={column.name} className="flex items-baseline justify-between gap-4">
                  <dt className="text-sm">{sentence(column.name)}</dt>
                  <dd className="text-xs text-muted-foreground">{column.cards.length}</dd>
                </div>
              ))}
            </dl>
            {view.board && (
              <Link
                to={`${base}/b/${view.board}`}
                className="mt-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
              >
                Open the board
                <ArrowRight className="size-3.5" />
              </Link>
            )}
          </MetaGroup>

          {view.pinned.length > 0 && (
            <MetaGroup label="Pinned" count={view.pinned.length}>
              <ul className="flex flex-col gap-4">
                {view.pinned.map((entry) => (
                  <li key={entry.id}>
                    <Link to={`${base}/knowledge/${encodeURIComponent(entry.slug)}`} className="text-sm leading-snug hover:underline">
                      {entry.title}
                    </Link>
                    <p className="mt-1 text-xs text-pretty text-muted-foreground">{entry.recap}</p>
                  </li>
                ))}
              </ul>
            </MetaGroup>
          )}

          <MetaGroup label="Recently written">
            {view.recent.length === 0 ? (
              <p className="text-sm text-muted-foreground">No entries yet.</p>
            ) : (
              <ul className="-mx-2 flex flex-col">
                {view.recent.map((entry) => (
                  <li key={entry.id}>
                    <Link
                      to={`${base}/knowledge/${encodeURIComponent(entry.slug)}`}
                      className="block px-2 py-1.5 transition-colors hover:bg-accent/50"
                    >
                      <span className="block text-sm leading-snug">{entry.title}</span>
                      <span className="mt-0.5 block text-xs text-muted-foreground">
                        {sentence(entry.type ?? 'note')}
                        {entry.updated_at ? `, ${ago(entry.updated_at)}` : ''}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </MetaGroup>
        </aside>
      </div>
    </main>
  )
}

function columnOf(columns: ColumnCards[], ref: string) {
  return columns.find((column) => column.cards.some((card) => card.ref === ref))?.name ?? ''
}

/** One column of work in motion, as rows that open the card's page. */
function CardSection({
  title,
  cards,
  base,
  empty,
}: {
  title: string
  cards: CardInfo[]
  base: string
  empty: string
}) {
  return (
    <section>
      <h2 className="flex items-baseline gap-2 text-heading">
        {title}
        <span className="text-xs font-normal text-muted-foreground">{cards.length}</span>
      </h2>
      {cards.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">{empty}</p>
      ) : (
        <ul className="-mx-3 mt-2 flex flex-col">
          {cards.map((card) => {
            const holder = shortActor(card.owner)
            const urgent = card.priority === 'urgent'
            return (
              <li key={card.id}>
                <Link
                  to={`${base}/card/${encodeURIComponent(card.ref)}`}
                  className="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-accent/50"
                >
                  <Lamp state={urgent ? 'alarm' : holder ? 'held' : 'idle'} />
                  <span className="min-w-0 flex-1 truncate text-sm">{card.title}</span>
                  {card.priority !== 'normal' && (
                    <span className={cn('shrink-0 text-xs', urgent ? 'text-danger' : 'text-muted-foreground')}>
                      {sentence(card.priority)}
                    </span>
                  )}
                  {holder && <span className="shrink-0 text-meta text-held">{holder}</span>}
                  <span className="w-24 shrink-0 text-right text-meta text-muted-foreground">{card.ref}</span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

function LoadingOverview() {
  return (
    <main className="mx-auto max-w-overview px-6 pb-24 lg:px-8">
      <div className="flex min-h-16 items-center py-4">
        <Skeleton className="h-7 w-40" />
      </div>
      <div className="mt-6 grid gap-x-14 gap-y-12 lg:grid-cols-overview">
        <div className="flex flex-col gap-3">
          <Skeleton className="h-5 w-32" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
        <div className="flex flex-col gap-3">
          <Skeleton className="h-4 w-20" />
          <Skeleton className="h-24 w-full" />
        </div>
      </div>
    </main>
  )
}
