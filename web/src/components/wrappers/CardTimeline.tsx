import type { ReactNode } from 'react'
import {
  Archive,
  ArchiveRestore,
  ArrowRight,
  Ban,
  ChevronRight,
  Circle,
  Flag,
  Link2,
  Link2Off,
  Lock,
  LockOpen,
  MessageSquare,
  Pencil,
  Plus,
  Tag,
  Trash2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { DiffView } from '@/components/wrappers/DiffView'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'
import { RELATIONS, shortActor } from '@/lib/cards'
import { sentence } from '@/lib/format'
import { meaningfulEvents } from '@/lib/card-events'
import { cn } from '@/lib/utils'

export interface CardEvent {
  seq: number
  timestamp: number
  actor: string
  action: string
  field?: string
  old_value?: string
  new_value?: string
}

/** A comment on a card, as the card detail returns it. */
export interface CardComment {
  id: string
  card_id?: string
  actor: string
  body: string
  created_at: number
}

type Item =
  | { kind: 'event'; at: number; key: string; event: CardEvent; older: CardEvent[] }
  | { kind: 'comment'; at: number; key: string; comment: CardComment }

const TEXT_FIELDS = new Set(['title', 'body', 'summary'])

/**
 * What happened to a card, as one timeline, newest first: its comments and its
 * events together, in the order they happened. Each item is a marker on one
 * line down the left with its own symbol; an event says in a sentence what
 * changed, a comment shows what was said. Who and when follow, and a new day
 * starts under its own heading. A text edit opens into a diff when the log
 * kept both sides, and says plainly when it did not.
 */
export function CardTimeline({
  events: all,
  comments = [],
  statusLabel = 'Status',
}: {
  events: CardEvent[]
  comments?: CardComment[]
  statusLabel?: string
}) {
  const events = meaningfulEvents(all)
  const items: Item[] = [
    ...events.map((event, index): Item => ({
      kind: 'event', at: event.timestamp, key: `event-${event.seq}`, event, older: events.slice(index + 1),
    })),
    ...comments.map((comment): Item => ({ kind: 'comment', at: comment.created_at, key: `comment-${comment.id}`, comment })),
  ].sort((a, b) => b.at - a.at)

  if (items.length === 0) {
    return <p className="text-sm text-muted-foreground">Nothing has happened yet.</p>
  }

  return (
    <ol className="flex flex-col">
      {items.map((item, index) => {
        const last = index === items.length - 1
        const heading = dayOf(item.at)
        const newDay = index === 0 || heading !== dayOf(items[index - 1].at)
        return (
          <li key={item.key} className="flex flex-col">
            {newDay && (
              <p className={cn('relative pb-2 pl-9 text-label text-muted-foreground', index > 0 && 'pt-1')}>
                {/* A day heading sits on the rail, so the line runs through it. */}
                {index > 0 && <span aria-hidden="true" className="absolute inset-y-0 left-3 w-px bg-border" />}
                {heading}
              </p>
            )}
            <div className="relative flex gap-3 pb-5">
              {/* The rail runs from this marker down to the next one. */}
              {!last && <span aria-hidden="true" className="absolute top-6 bottom-0 left-3 w-px bg-border" />}
              {item.kind === 'event' ? (
                <>
                  <span
                    aria-hidden="true"
                    className={cn(
                      'relative flex size-6 shrink-0 items-center justify-center bg-muted text-muted-foreground',
                      item.event.action === 'created' && 'bg-foreground text-background',
                    )}
                  >
                    {symbol(item.event)}
                  </span>
                  <div className="min-w-0 flex-1 pt-0.5">
                    <p className="text-sm text-foreground">{summary(item.event, statusLabel)}</p>
                    <Byline actor={item.event.actor} at={item.at} />
                    <TextChange event={item.event} older={item.older} />
                  </div>
                </>
              ) : (
                <>
                  <span aria-hidden="true" className="relative flex size-6 shrink-0 items-center justify-center bg-accent text-foreground">
                    <MessageSquare className="size-3" />
                  </span>
                  <div className="min-w-0 flex-1 pt-0.5">
                    <Byline actor={item.comment.actor} at={item.at} verb="commented" />
                    <div className="mt-1.5 bg-card px-3.5 py-2.5">
                      <MarkdownContent content={item.comment.body} />
                    </div>
                  </div>
                </>
              )}
            </div>
          </li>
        )
      })}
    </ol>
  )
}

/** Who, optionally what they did, and at what time of the day. */
function Byline({ actor, at, verb }: { actor: string; at: number; verb?: string }) {
  return (
    <p className={cn('flex items-baseline gap-2 text-xs text-muted-foreground', verb ? 'text-sm' : 'mt-0.5')}>
      <span className={cn('text-meta', verb && 'text-foreground')}>{shortActor(actor) ?? actor}</span>
      {verb && <span>{verb}</span>}
      <time className="text-xs" dateTime={new Date(at).toISOString()}>{clock(at)}</time>
    </p>
  )
}

/** The diff under a text edit, or a plain note when the log could not keep one. */
function TextChange({ event, older }: { event: CardEvent; older: CardEvent[] }) {
  if (event.action !== 'edited' || !TEXT_FIELDS.has(event.field ?? '')) return null
  const after = event.new_value ?? ''
  // An older edit of the same field holds the text this one replaced.
  const before = event.old_value || older.find((previous) => previous.field === event.field && previous.new_value)?.new_value
  if (!after) return <p className="mt-1 text-xs text-muted-foreground">The text of this change was not recorded.</p>
  if (!before) return <p className="mt-1 text-xs text-muted-foreground">The earlier text was not recorded.</p>
  return (
    <Collapsible>
      <CollapsibleTrigger render={<Button variant="ghost" size="xs" className="mt-1 -ml-2 text-muted-foreground" />}>
        <ChevronRight data-icon="inline-start" className="transition-transform duration-200 ease-settle group-aria-expanded/button:rotate-90" />
        Show changes
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2">
        <DiffView before={before} after={after} mode={event.field === 'body' ? 'lines' : 'words'} />
      </CollapsibleContent>
    </Collapsible>
  )
}

/** Each kind of event keeps one symbol, so a long timeline can be skimmed. */
function symbol(event: CardEvent): ReactNode {
  switch (event.action) {
    case 'created':
      return <Plus className="size-3" />
    case 'moved':
      return <ArrowRight className="size-3" />
    case 'edited':
      return event.field === 'priority' ? <Flag className="size-3" /> : <Pencil className="size-3" />
    case 'claimed':
      return <Lock className="size-3" />
    case 'released':
      return <LockOpen className="size-3" />
    case 'blocked':
      return <Ban className="size-3" />
    case 'unblocked':
      return <Circle className="size-3" />
    case 'linked':
    case 'related':
      return <Link2 className="size-3" />
    case 'unlinked':
    case 'unrelated':
      return <Link2Off className="size-3" />
    case 'labeled':
    case 'unlabeled':
    case 'tagged':
    case 'untagged':
      return <Tag className="size-3" />
    case 'archived':
      return <Archive className="size-3" />
    case 'unarchived':
      return <ArchiveRestore className="size-3" />
    case 'deleted':
      return <Trash2 className="size-3" />
    default:
      return <Circle className="size-3" />
  }
}

function summary(event: CardEvent, statusLabel: string) {
  const from = sentence(event.old_value ?? '')
  const to = sentence(event.new_value ?? '')
  const field = event.field ?? ''
  switch (event.action) {
    case 'created':
      return 'Created'
    case 'moved':
      return field === 'column' ? `${statusLabel} changed from ${from} to ${to}` : 'Moved'
    case 'edited':
      if (field === 'priority') return `Priority changed from ${from} to ${to}`
      return TEXT_FIELDS.has(field) ? `${sentence(field)} edited` : `${sentence(field)} changed`
    case 'claimed':
      return 'Took the lease'
    case 'released':
      return 'Released the lease'
    case 'blocked':
      return `Blocked by ${event.new_value}`
    case 'unblocked':
      return `No longer blocked by ${event.old_value}`
    case 'linked':
      return `Linked to ${event.new_value}`
    case 'unlinked':
      return `Unlinked from ${event.old_value}`
    // A relation's field is how it reads from this card: "Resolved by REL-2".
    case 'related':
      return `${relation(field)} ${event.new_value}`
    case 'unrelated':
      return `No longer ${relation(field).toLowerCase()} ${event.old_value || event.new_value}`
    // Labels and tags name themselves, so the value is the news, not the field.
    case 'labeled':
    case 'tagged':
      return `${sentence(field)} added: ${event.new_value}`
    case 'unlabeled':
    case 'untagged':
      return `${sentence(field)} removed: ${event.old_value}`
    case 'archived':
      return 'Archived'
    case 'unarchived':
      return 'Restored from the archive'
    default:
      return field ? `${sentence(event.action)} ${sentence(field).toLowerCase()}` : sentence(event.action)
  }
}

function relation(field: string) {
  return RELATIONS.find((kind) => kind.value === field.replace(/-/g, '_'))?.label ?? sentence(field.replace(/_/g, ' '))
}

function dayOf(ms: number) {
  const day = new Date(ms)
  const today = new Date()
  const yesterday = new Date()
  yesterday.setDate(today.getDate() - 1)
  if (day.toDateString() === today.toDateString()) return 'Today'
  if (day.toDateString() === yesterday.toDateString()) return 'Yesterday'
  return day.toLocaleDateString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    year: day.getFullYear() === today.getFullYear() ? undefined : 'numeric',
  })
}

function clock(ms: number) {
  return new Date(ms).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}
