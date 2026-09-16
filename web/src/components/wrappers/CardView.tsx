import { useRef, useState, useCallback, type FormEvent, type MouseEvent } from 'react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'
import { EditForm, InPlaceText } from '@/components/wrappers/EditInPlace'
import { Lamp } from '@/components/wrappers/Lamp'
import { MarkdownEditor } from '@/components/wrappers/MarkdownEditor'
import { HistoryList, type HistoryEvent } from '@/components/wrappers/HistoryList'
import { MetaFacts, MetaGroup, MetaPanel } from '@/components/wrappers/MetaPanel'
import { sentence } from '@/lib/format'
import { meaningfulEvents } from '@/lib/history'
import { PRIORITIES, shortActor } from '@/lib/cards'

export interface CardInfo {
  id: string
  ref: string
  title: string
  body: string
  priority: string
  version: number
  owner?: string
  created_at?: number
  updated_at?: number
}

export interface CardNote { id: string; actor: string; body: string; created_at: number }

const BODY_PLACEHOLDER = 'Context, or how you will know it is done'

export type CardMode = 'read' | 'edit' | 'create'

/** A new card, as the create form collects it. */
export interface CardDraft {
  title: string
  body: string
  priority: string
  /** The status (board column) the card starts in. */
  column?: string
}

/** What Edit changes on an existing card: its words. */
export interface CardEdit {
  title: string
  body: string
}

const stamp = (ms: number) => new Date(ms).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })

/**
 * One card, two columns: what it says on the left, what is true about it on the
 * right. The same component backs the dialog and the full page, so a card reads
 * identically wherever it is opened.
 *
 * The view draws no chrome of its own. The dialog and the page each put one
 * action row above it, and nothing repeats the title or the facts.
 *
 * Editing happens in place and covers the words only. Reading, the title and
 * body show a faint wash when pointed at, and a click on either starts
 * editing it. Editing, they become fields where they stand, in the same type,
 * and notes and history stay put. The facts column has its own logic and
 * ignores Edit: status and priority apply the moment they change. Save,
 * Cancel and the markdown toggle belong to the parent's action row, in the
 * place Edit had, so starting an edit moves nothing.
 */
export function CardView({
  card,
  notes,
  history = [],
  columns,
  currentColumn,
  mode,
  saving,
  wide = false,
  source,
  onModeChange,
  onSave,
  onCreate,
  onMove,
  onPriority,
  onSteal,
}: {
  card: CardInfo | null
  notes: CardNote[]
  /** The card's event log, newest first, as the card detail returns it. */
  history?: HistoryEvent[]
  columns: string[]
  currentColumn?: string
  mode: CardMode
  saving: boolean
  wide?: boolean
  /** Show the body as markdown source. The parent owns the toggle, which sits with Save. */
  source: boolean
  onModeChange: (mode: CardMode) => void
  onSave: (edit: CardEdit) => Promise<void>
  onCreate: (draft: CardDraft) => Promise<void>
  onMove: (column: string) => Promise<void>
  onPriority: (priority: string) => Promise<void>
  onSteal: (reason: string) => Promise<void>
}) {
  // Priority and status are only drafted for a card that does not exist yet.
  const initial = () => ({ title: card?.title ?? '', priority: 'normal', column: columns[0] ?? '' })
  const [draft, setDraft] = useState(initial)
  const [focus, setFocus] = useState<'title' | 'body'>('title')
  const [stealing, setStealing] = useState(false)
  const [reason, setReason] = useState('')
  const readBody = useRef<() => string>(() => card?.body ?? '')
  const registerBody = useCallback((read: () => string) => { readBody.current = read }, [])

  // A different card, or a different mode, starts from what the card says.
  // Reset while rendering, so the first edit frame never shows a stale draft.
  const [shown, setShown] = useState({ id: card?.id, mode })
  if (shown.id !== card?.id || shown.mode !== mode) {
    setShown({ id: card?.id, mode })
    setDraft(initial())
    if (mode === 'read') setFocus('title')
    setStealing(false)
    setReason('')
  }

  const locked = Boolean(card?.owner)
  const holder = shortActor(card?.owner)
  const changes = meaningfulEvents(history)
  const editing = mode !== 'read'
  const creating = mode === 'create'
  // A held card's status and priority wait for its lease.
  const fixed = locked && !creating
  const status = creating ? draft.column : (currentColumn ?? '')
  const priority = creating ? draft.priority : (card?.priority ?? 'normal')

  const canEdit = mode === 'read' && Boolean(card) && !locked
  // Reading, a click on the words starts editing them, unless it lands on a
  // link or finishes a text selection.
  const startEdit = (target: 'title' | 'body') => (event: MouseEvent<HTMLElement>) => {
    if (!canEdit) return
    if ((event.target as HTMLElement).closest('a')) return
    if (window.getSelection()?.toString()) return
    setFocus(target)
    onModeChange('edit')
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!draft.title.trim()) return
    const body = readBody.current()
    if (creating) {
      await onCreate({ title: draft.title, body, priority: draft.priority, column: draft.column || undefined })
      return
    }
    await onSave({ title: draft.title, body })
  }

  const content = (
    <>
      {editing ? (
        <InPlaceText
          label="Card title"
          placeholder="What needs doing"
          required
          className="text-title text-balance md:text-title"
          value={draft.title}
          focusOnMount={mode === 'edit' && focus === 'title'}
          onChange={(title) => setDraft({ ...draft, title })}
        />
      ) : (
        <h1 className={cn('text-title text-balance', canEdit && 'edit-hint')} onClick={startEdit('title')}>{card?.title}</h1>
      )}
      <Separator className="my-6" />
      {editing ? (
        <MarkdownEditor
          key={`${card?.id ?? 'new'}-${mode}`}
          value={card?.body ?? ''}
          source={source}
          autoFocus={mode === 'edit' && focus === 'body'}
          onRead={registerBody}
          placeholder={BODY_PLACEHOLDER}
          minHeight={creating ? 'min-h-80' : 'min-h-40'}
        />
      ) : (
        <div className={cn(canEdit && 'edit-hint')} onClick={startEdit('body')}>
          {card?.body ? (
            <MarkdownContent content={card.body} />
          ) : canEdit ? (
            <p className="text-body text-muted-foreground/55">{BODY_PLACEHOLDER}</p>
          ) : (
            <p className="text-sm text-muted-foreground">This card has no body.</p>
          )}
        </div>
      )}
    </>
  )

  return (
    <div
      className={cn(
        'grid items-start gap-10',
        wide ? 'justify-center gap-x-14 grid-cols-measure xl:grid-cols-entry' : 'lg:grid-cols-doc',
      )}
    >
      <div className="min-w-0">
        {editing ? <EditForm id="card-form" onSubmit={submit}>{content}</EditForm> : content}

        {notes.length > 0 && (
          <section className="mt-10">
            <h2 className="text-heading">Notes</h2>
            <ul className="mt-4 flex flex-col gap-6">
              {notes.map((note) => (
                <li key={note.id}>
                  <p className="flex items-baseline gap-3 text-xs text-muted-foreground">
                    <span className="text-meta">{shortActor(note.actor) ?? note.actor}</span>
                    <span>{new Date(note.created_at).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</span>
                  </p>
                  <div className="mt-2">
                    <MarkdownContent content={note.body} />
                  </div>
                </li>
              ))}
            </ul>
          </section>
        )}

        {changes.length > 0 && (
          <section className="mt-10">
            <h2 className="flex items-baseline gap-2 text-heading">
              History
              <span className="text-xs font-normal text-muted-foreground">{changes.length}</span>
            </h2>
            <div className="mt-2">
              <HistoryList events={changes} />
            </div>
          </section>
        )}
      </div>

      <MetaPanel>
        {/* Status is the board column, named the way trackers name it. Status
            and priority apply the moment they change, editing or not; only a
            card being created drafts them. */}
        <MetaGroup label="Status">
          <Select
            items={columns.map((name) => ({ value: name, label: sentence(name) }))}
            value={status}
            onValueChange={(value) => {
              if (!value || value === status) return
              if (creating) setDraft({ ...draft, column: value })
              else void onMove(value)
            }}
          >
            <SelectTrigger
              aria-label="Status"
              className="w-full"
              disabled={fixed}
              title={fixed ? `Held by ${holder}. Take the lease to change its status.` : undefined}
            >
              <SelectValue placeholder="Status" />
            </SelectTrigger>
            <SelectContent>
              {columns.map((name) => (
                <SelectItem key={name} value={name}>{sentence(name)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </MetaGroup>

        <MetaGroup label="Priority">
          <Select
            items={PRIORITIES.map((value) => ({ value, label: sentence(value) }))}
            value={priority}
            onValueChange={(value) => {
              if (!value || value === priority) return
              if (creating) setDraft({ ...draft, priority: value })
              else void onPriority(value)
            }}
          >
            <SelectTrigger
              aria-label="Priority"
              className={cn('w-full', priority === 'urgent' && 'text-danger')}
              disabled={fixed}
              title={fixed ? `Held by ${holder}. Take the lease to change its priority.` : undefined}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PRIORITIES.map((value) => (
                <SelectItem key={value} value={value}>{sentence(value)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </MetaGroup>

        {!creating && card && (
          <MetaGroup label="Details">
            <MetaFacts
              facts={[
                { label: 'Ref', value: card.ref, mono: true },
                { label: 'Version', value: `v${card.version}` },
                { label: 'Notes', value: String(notes.length) },
              ]}
            />
          </MetaGroup>
        )}

        {!creating && card && (
          <MetaGroup label="Lease">
            {locked ? (
              <>
                <p className="flex items-center gap-2 text-sm">
                  <Lamp state="held" />
                  Held by <span className="text-meta text-held">{holder}</span>
                </p>
                {stealing ? (
                  <form
                    className="mt-3 flex flex-col gap-2"
                    onSubmit={async (event) => {
                      event.preventDefault()
                      if (!reason.trim()) return
                      await onSteal(reason.trim())
                      setStealing(false)
                      setReason('')
                    }}
                  >
                    <Input
                      aria-label="Reason for taking the lease"
                      placeholder="Why you are taking it"
                      value={reason}
                      autoFocus
                      onChange={(event) => setReason(event.target.value)}
                    />
                    <div className="flex gap-2">
                      <Button type="submit" size="sm" disabled={!reason.trim() || saving}>Take it</Button>
                      <Button type="button" size="sm" variant="ghost" onClick={() => setStealing(false)}>Cancel</Button>
                    </div>
                  </form>
                ) : (
                  <Button size="sm" variant="outline" className="mt-3 w-full" onClick={() => setStealing(true)}>
                    Take the lease
                  </Button>
                )}
              </>
            ) : (
              <p className="text-sm text-muted-foreground">Not held. Any agent can claim it.</p>
            )}
          </MetaGroup>
        )}

        {!creating && card && (card.created_at !== undefined || card.updated_at !== undefined) && (
          <p className="flex flex-col gap-1 text-xs text-muted-foreground">
            {card.created_at !== undefined && <span>Created {stamp(card.created_at)}</span>}
            {card.updated_at !== undefined && <span>Updated {stamp(card.updated_at)}</span>}
          </p>
        )}
      </MetaPanel>

    </div>
  )
}
