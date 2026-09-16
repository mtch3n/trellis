import { useCallback, useEffect, useState } from 'react'
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
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { EditActions } from '@/components/wrappers/EditInPlace'
import { ActionRow } from '@/components/wrappers/ActionRow'
import { CardMenu } from '@/components/wrappers/CardMenu'
import {
  CardView,
  type CardEdit,
  type CardInfo,
  type CardMode,
  type CardNote,
} from '@/components/wrappers/CardView'
import { PRIORITIES, PRIORITY_NUMBERS, shortActor } from '@/lib/cards'
import type { HistoryEvent } from '@/components/wrappers/HistoryList'

interface ColumnCardsInfo { name: string; cards: CardInfo[] }
interface CardDetail { card: CardInfo; notes: CardNote[]; activity?: HistoryEvent[] }

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
  const [notes, setNotes] = useState<CardNote[]>([])
  const [history, setHistory] = useState<HistoryEvent[]>([])
  const [columns, setColumns] = useState<ColumnCardsInfo[]>([])
  const [board, setBoard] = useState<string | null>(null)
  const [mode, setMode] = useState<CardMode>('read')
  const [source, setSource] = useState(false)
  const [saving, setSaving] = useState(false)
  const [labelOptions, setLabelOptions] = useState<string[]>([])
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
      if (!detail.ok) throw new Error(await detail.text())
      const data = (await detail.json()) as CardDetail
      setCard(data.card)
      setNotes(data.notes ?? [])
      setHistory(data.activity ?? [])
      setError(null)

      if (slug) {
        const onBoard = await fetch(`/api/p/${projectKey}/b/${slug}/cards`, { signal })
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

  const save = async (edit: CardEdit) => {
    if (!card || !board) return
    setSaving(true)
    try {
      // Only what changed is sent, so the history records edits, not saves.
      const changes: { title?: string; body?: string } = {}
      if (edit.title !== card.title) changes.title = edit.title
      if (edit.body !== card.body) changes.body = edit.body
      const response = Object.keys(changes).length === 0
        ? null
        : await fetch(`${base}/cards/${encodeURIComponent(card.ref)}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ...changes, if_version: card.version }),
          })
      if (response && !response.ok) {
        const text = await response.text()
        await load()
        toast.add({ title: 'Card changed underneath you', description: `${text} It has been reloaded.`, type: 'error' })
        return
      }
      // Reload rather than patch in the response, so the history shows the edit.
      await load()
      changeMode('read')
      toast.add({ title: `Saved ${card.ref}`, type: 'success' })
    } catch (err) {
      toast.add({ title: 'Could not save card', description: message(err), type: 'error' })
    } finally { setSaving(false) }
  }

  const move = async (column: string) => {
    if (!card || !board) return
    try {
      const response = await fetch(`${base}/cards/${encodeURIComponent(card.ref)}/move`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ column, before: '' }),
      })
      if (!response.ok) throw new Error(await response.text())
      await load()
    } catch (err) {
      toast.add({ title: 'Could not move card', description: message(err), type: 'error' })
    }
  }

  // A lease this person holds is theirs to edit, so the page needs to know
  // which principal the server writes as.
  const [me, setMe] = useState<string>()
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/me', { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : null))
      .then((who: { actor: string } | null) => { if (who) setMe(who.actor) })
      .catch(() => { /* without it every lease simply reads as someone else's */ })
    return () => controller.abort()
  }, [])

  // The project's labels are its own vocabulary, so the card offers those and
  // never invents one.
  useEffect(() => {
    if (!projectKey) return
    const controller = new AbortController()
    fetch(`/api/p/${projectKey}/labels`, { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : []))
      .then((labels: { name: string }[]) => setLabelOptions(labels.map((label) => label.name)))
      .catch(() => { /* the list is an offer, not a requirement */ })
    return () => controller.abort()
  }, [projectKey])

  /** One label or tag added or removed, applied at once like status and priority. */
  const chip = (field: 'labels' | 'tags') => async (change: { add?: string; remove?: string }) => {
    if (!card || !board) return
    const patch = change.add ? { [`add_${field}`]: [change.add] } : { [`remove_${field}`]: [change.remove] }
    try {
      const response = await fetch(`${base}/cards/${encodeURIComponent(card.ref)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
      })
      if (!response.ok) throw new Error(await response.text())
    } catch (err) {
      toast.add({ title: `Could not change the ${field}`, description: message(err), type: 'error' })
    } finally {
      await load()
    }
  }

  // A single field, so no version: the server asks for one only when a
  // title or body is replaced wholesale.
  const setPriority = async (priority: string) => {
    if (!card || !board) return
    try {
      const response = await fetch(`${base}/cards/${encodeURIComponent(card.ref)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ priority: PRIORITY_NUMBERS[priority as (typeof PRIORITIES)[number]] }),
      })
      if (!response.ok) throw new Error(await response.text())
    } catch (err) {
      toast.add({ title: 'Could not change the priority', description: message(err), type: 'error' })
    } finally {
      await load()
    }
  }

  const steal = async (reason: string) => {
    if (!card || !board) return
    setSaving(true)
    try {
      const response = await fetch(`${base}/cards/${encodeURIComponent(card.ref)}/steal`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ reason }),
      })
      if (!response.ok) throw new Error(await response.text())
      await load()
    } catch (err) {
      toast.add({ title: 'Could not take the lease', description: message(err), type: 'error' })
    } finally { setSaving(false) }
  }

  const back = board ? `/p/${projectKey}/b/${board}` : '/'

  const remove = async () => {
    if (!card || !board) return false
    try {
      const response = await fetch(`${base}/cards/${encodeURIComponent(card.ref)}`, {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      })
      if (!response.ok) throw new Error(await response.text())
      toast.add({ title: `Deleted ${card.ref}`, type: 'success' })
      navigate(back, { replace: true })
      return true
    } catch (err) {
      toast.add({ title: `Could not delete ${card.ref}`, description: message(err), type: 'error' })
      return false
    }
  }

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
          {card && (
            <div className="ml-auto flex items-center gap-1.5">
              {mode === 'read' ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={Boolean(card.owner) && card.owner !== me}
                  onClick={() => changeMode('edit')}
                  title={card.owner && card.owner !== me ? 'Held by an agent. Take the lease to edit.' : undefined}
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
                deleteDisabledReason={card.owner ? `Held by ${shortActor(card.owner)}. Take the lease to delete it.` : undefined}
                onDelete={remove}
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
              notes={notes}
              history={history}
              columns={columns.map((column) => column.name)}
              currentColumn={currentColumn}
              mode={mode}
              saving={saving}
              wide
              source={source}
              onModeChange={changeMode}
              onSave={save}
              onCreate={async () => {}}
              onMove={move}
              onPriority={setPriority}
              labelOptions={labelOptions}
              onLabel={chip('labels')}
              onTag={chip('tags')}
              me={me}
              onSteal={steal}
            />
          </div>
        )}
      </div>
    </main>
  )
}
