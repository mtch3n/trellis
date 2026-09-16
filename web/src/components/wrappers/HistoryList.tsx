import { ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { DiffView } from '@/components/wrappers/DiffView'
import { ago, sentence } from '@/lib/format'
import { meaningfulEvents } from '@/lib/history'

export interface HistoryEvent {
  seq: number
  timestamp: number
  actor: string
  action: string
  field?: string
  old_value?: string
  new_value?: string
}

const TEXT_FIELDS = new Set(['title', 'body', 'summary'])

/**
 * What happened to a card or an entry, newest first. Status and priority
 * changes read as from and to; a text edit opens into a diff when the log kept
 * both sides, and says plainly when it did not.
 */
export function HistoryList({ events: all, statusLabel = 'Status' }: { events: HistoryEvent[]; statusLabel?: string }) {
  const events = meaningfulEvents(all)
  if (events.length === 0) {
    return <p className="text-sm text-muted-foreground">No history yet.</p>
  }

  return (
    <ol className="flex flex-col">
      {events.map((event, index) => {
        const text = event.action === 'edited' && TEXT_FIELDS.has(event.field ?? '')
        const after = event.new_value ?? ''
        // An older edit of the same field holds the text this one replaced.
        const before = event.old_value || events.slice(index + 1).find((older) => older.field === event.field && older.new_value)?.new_value
        return (
          <li key={event.seq} className="py-2.5">
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm">
              <span className="text-foreground">{summary(event, statusLabel)}</span>
              <span className="text-meta text-muted-foreground">{event.actor.replace(/^agent:/, '').slice(0, 8)}</span>
              <span className="text-xs text-muted-foreground" title={new Date(event.timestamp).toLocaleString()}>
                {ago(event.timestamp)}
              </span>
            </div>
            {text && (!after ? (
              <p className="mt-1 text-xs text-muted-foreground">The text of this change was not recorded.</p>
            ) : !before ? (
              <p className="mt-1 text-xs text-muted-foreground">The earlier text was not recorded.</p>
            ) : (
              <Collapsible>
                <CollapsibleTrigger render={<Button variant="ghost" size="xs" className="mt-1 -ml-2 text-muted-foreground" />}>
                  <ChevronRight data-icon="inline-start" className="transition-transform duration-200 ease-settle group-aria-expanded/button:rotate-90" />
                  Show changes
                </CollapsibleTrigger>
                <CollapsibleContent className="mt-2">
                  <DiffView before={before} after={after} mode={event.field === 'body' ? 'lines' : 'words'} />
                </CollapsibleContent>
              </Collapsible>
            ))}
          </li>
        )
      })}
    </ol>
  )
}

function summary(event: HistoryEvent, statusLabel: string) {
  const from = sentence(event.old_value ?? '')
  const to = sentence(event.new_value ?? '')
  if (event.action === 'moved' && event.field === 'column') return `${statusLabel}: ${from} to ${to}`
  if (event.action === 'edited' && event.field === 'priority') return `Priority: ${from} to ${to}`
  if (event.action === 'edited' && event.field && TEXT_FIELDS.has(event.field)) return `${sentence(event.field)} edited`
  if (event.action === 'created') return 'Created'
  // Labels and tags name themselves, so the value is the news, not the field.
  if (event.action === 'labeled' || event.action === 'tagged') return `${sentence(event.field ?? '')} added: ${event.new_value}`
  if (event.action === 'unlabeled' || event.action === 'untagged') return `${sentence(event.field ?? '')} removed: ${event.old_value}`
  return event.field ? `${sentence(event.action)} ${event.field}` : sentence(event.action)
}
