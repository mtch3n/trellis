import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Pencil } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { EditActions } from '@/components/wrappers/EditInPlace'
import { ActionRow } from '@/components/wrappers/ActionRow'
import { CardMenu } from '@/components/wrappers/CardMenu'
import { HistoryDialog } from '@/components/wrappers/HistoryDialog'
import {
  CardView,
  type CardEdit,
  type CardInfo,
  type CardMode,
} from '@/components/wrappers/CardView'
import { shortActor } from '@/lib/cards'
import { cardActions } from '@/lib/card-actions'
import type { CardComment, CardEvent } from '@/components/wrappers/CardTimeline'
import { readError } from '@/lib/api'
import { withDetail, type CardDetail } from '@/lib/cards'
import type { Entry } from '@/lib/entry'
import type { Artifact } from '@/components/wrappers/ArtifactList'

interface ColumnCardsInfo { name: string; cards: CardInfo[] }

function message(err: unknown) {
  return err instanceof Error ? err.message : 'Unknown error'
}

/**
 * A card at its own URL, full width. This is where the board's expand control
 * lands: the same view, the app shell still around it, and an address that can
 * be linked to from outside.
 */
export function CardPage() {
  const { projectKey, cardRef } = useParams<{ projectKey: string; cardRef: string }>()
  const [card, setCard] = useState<CardInfo | null>(null)
  const [comments, setComments] = useState<CardComment[]>([])
  const [events, setEvents] = useState<CardEvent[]>([])
  const [columns, setColumns] = useState<ColumnCardsInfo[]>([])
  const cardOptions = useMemo(
    () => columns.flatMap((column) => column.cards.map((item) => ({ ref: item.ref, title: item.title }))),
    [columns],
  )
  const [board, setBoard] = useState<string | null>(null)
  const [mode, setMode] = useState<CardMode>('read')
  const [source, setSource] = useState(false)
  const [saving, setSaving] = useState(false)
  const [labelOptions, setLabelOptions] = useState<string[]>([])
  const [entryOptions, setEntryOptions] = useState<Entry[]>([])
  const [storedArtifacts, setStoredArtifacts] = useState<Artifact[]>([])
  const [history, setHistory] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  const changeMode = (next: CardMode) => { setMode(next); setSource(false) }

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!projectKey || !cardRef) return
    try {
      const boards = await fetch(`/api/p/${projectKey}/boards`, { signal })
      const slug = boards.ok ? (((await boards.json()) as { slug: string }[])[0]?.slug ?? null) : null
      setBoard(slug)

      const detail = await fetch(`/api/p/${projectKey}/cards/${encodeURIComponent(cardRef)}`, { signal })
      if (!detail.ok) throw new Error(await readError(detail))
      const data = (await detail.json()) as CardDetail
      setCard(withDetail(data))
      setComments(data.comments ?? [])
      setEvents(data.events ?? [])
      setError(null)

      if (slug) {
        // An archived card is not on the live board, so its columns are read
        // from the archived view instead; either way the page needs the
        // column names and the cards it could be related to.
        const onBoard = await fetch(
          `/api/p/${projectKey}/b/${slug}/cards${data.card.archived_at ? '?archived=1' : ''}`,
          { signal },
        )
        if (onBoard.ok) setColumns((await onBoard.json()) as ColumnCardsInfo[])
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(message(err))
    }
  }, [projectKey, cardRef])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const base = `/api/p/${projectKey}/b/${board}`
  const currentColumn = columns.find((c) => c.cards.some((item) => item.ref === card?.ref))?.name

  // Every write a card supports, shared with the board; the page only says
  // what to bring up to date afterwards.
  const actions = useMemo(
    () => cardActions({ base, refresh: async () => { await load() } }),
    [base, load],
  )

  const save = async (edit: CardEdit) => {
    if (!card || !board) return
    setSaving(true)
    const saved = await actions.save(card, edit)
    setSaving(false)
    if (saved) {
      changeMode('read')
      toast.add({ title: `Saved ${card.ref}`, type: 'success' })
    }
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

  // What the card can point at: the project's labels, the entries it could
  // cite, and the files already stored. Each is an offer, so a failure to read
  // one leaves the card working without it.
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

  const back = board ? `/p/${projectKey}/b/${board}` : '/'

  const remove = async () => {
    if (!card) return false
    const deleted = await actions.remove(card.ref)
    if (deleted) navigate(back, { replace: true })
    return deleted
  }

  const claimed = Boolean(card?.claimed_by) && card?.claimed_by !== me
  const editable = card && !claimed

  return (
    <main className="px-6 pb-24 lg:px-8">
      <div className="mx-auto max-w-measure xl:max-w-entry">
        <ActionRow>
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink render={<Link to={back} />}>Board</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage className="text-meta">{cardRef}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
          {/* An archived card says so here: it is not on the board, and every
              action on it still works. */}
          {card?.archived_at && <Badge variant="outline">Archived</Badge>}
          {card && (
            <div className="ml-auto flex items-center gap-1.5">
              {mode === 'read' ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!editable}
                  onClick={() => changeMode('edit')}
                  title={claimed ? 'Claimed by an agent. Steal the claim to edit.' : undefined}
                >
                  <Pencil data-icon="inline-start" />
                  Edit
                </Button>
              ) : (
                <EditActions form="card-form" source={source} onSourceChange={setSource} label="Save card" saving={saving} onCancel={() => changeMode('read')} />
              )}
              <CardMenu
                cardRef={card.ref}
                href={`/p/${projectKey}/card/${encodeURIComponent(card.ref)}`}
                archived={Boolean(card.archived_at)}
                disabledReason={card.claimed_by ? `Claimed by ${shortActor(card.claimed_by)}. Steal the claim first.` : undefined}
                onDelete={remove}
                onArchive={(archived) => actions.archive(card.ref, archived)}
                onHistory={() => setHistory(true)}
              />
            </div>
          )}
        </ActionRow>

        {error && (
          <Alert variant="destructive" className="mt-6">
            <AlertTitle>Could not load the card</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {!card && !error && (
          <div className="mt-6 grid items-start gap-x-14 gap-y-10 grid-cols-measure xl:grid-cols-entry">
            <Skeleton className="h-96 w-full" />
            <Skeleton className="h-48 w-full" />
          </div>
        )}

        {card && (
          <div className="mt-6">
            <CardView
              card={card}
              comments={comments}
              events={events}
              columns={columns.map((column) => column.name)}
              currentColumn={currentColumn}
              mode={mode}
              saving={saving}
              wide
              source={source}
              onModeChange={changeMode}
              onSave={save}
              onCreate={async () => {}}
              actions={actions}
              labelOptions={labelOptions}
              entryOptions={entryOptions}
              storedArtifacts={storedArtifacts}
              me={me}
              base={`/p/${projectKey}`}
              cardOptions={cardOptions}
            />
          </div>
        )}

        {card && (
          <HistoryDialog
            open={history}
            title={card.ref}
            base={`/api/p/${projectKey}/cards/${encodeURIComponent(card.ref)}`}
            onOpenChange={setHistory}
          />
        )}
      </div>
    </main>
  )
}
