import { Fragment, useCallback, useEffect, useState, type ReactNode } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { toast } from '@/components/ui/toast'
import { ConfirmDialog } from '@/components/wrappers/ConfirmDialog'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { readError } from '@/lib/api'
import { bytes } from '@/lib/format'

interface MaintenanceStats {
  database_bytes: number
  wal_bytes: number
  /** Revision folders whose entry no longer exists. */
  leftover_revisions: number
  /** How many revisions each entry and card keeps. */
  history_keep: number
}

/** What one prune removes; exactly one of these is set per action. */
interface Prune {
  events?: boolean
  invocations?: boolean
  revisions?: boolean
  leftover_revisions?: boolean
  before?: string
}

const RETENTION = [
  { value: '30d', label: 'Older than 30 days', days: 30 },
  { value: '90d', label: 'Older than 90 days', days: 90 },
  { value: '180d', label: 'Older than 180 days', days: 180 },
  { value: '365d', label: 'Older than a year', days: 365 },
] as const

type Action = 'events' | 'invocations' | 'revisions' | 'leftovers' | 'compact'

/** What each trim counts, one and many. */
const DELETED: Record<Exclude<Action, 'compact'>, [string, string]> = {
  events: ['event', 'events'],
  invocations: ['invocation', 'invocations'],
  revisions: ['revision', 'revisions'],
  leftovers: ['revision folder', 'revision folders'],
}

/**
 * Keeping the database small: what it weighs, and the few ways to trim it.
 * Every trim deletes for good, so each asks first and reports how much went.
 * Compacting only rewrites the file, so it runs at once.
 */
export function SettingsMaintenance() {
  const [stats, setStats] = useState<MaintenanceStats | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [events, setEvents] = useState('90d')
  const [invocations, setInvocations] = useState('90d')
  const [confirming, setConfirming] = useState<Action | null>(null)
  // The last question asked, kept so its words stay while the dialog closes.
  const [asked, setAsked] = useState<Exclude<Action, 'compact'>>('events')
  const [running, setRunning] = useState<Action | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch('/api/maintenance', { signal })
      if (!response.ok) throw new Error(await readError(response))
      setStats((await response.json()) as MaintenanceStats)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not read the database')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const prune = (action: Action): Prune => {
    switch (action) {
      case 'events': return { events: true, before: events }
      case 'invocations': return { invocations: true, before: invocations }
      case 'revisions': return { revisions: true }
      default: return { leftover_revisions: true }
    }
  }

  const run = async (action: Action) => {
    setRunning(action)
    try {
      const compact = action === 'compact'
      const response = await fetch(compact ? '/api/maintenance/compact' : '/api/maintenance/prune', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(compact ? {} : prune(action)),
      })
      if (!response.ok) throw new Error(await readError(response))
      if (action === 'compact') {
        toast.add({ title: 'Database compacted', type: 'success' })
      } else {
        const { deleted } = (await response.json()) as { deleted: number }
        toast.add({ title: `Deleted ${deleted.toLocaleString()} ${DELETED[action][deleted === 1 ? 0 : 1]}`, type: 'success' })
      }
      setConfirming(null)
      await load()
    } catch (err) {
      toast.add({ title: 'Maintenance failed', description: err instanceof Error ? err.message : undefined, type: 'error' })
    } finally {
      setRunning(null)
    }
  }

  const days = (value: string) => RETENTION.find((option) => option.value === value)?.days ?? 0
  const questions: Record<Exclude<Action, 'compact'>, { title: string; description: string; confirm: string }> = {
    events: {
      title: `Delete events older than ${days(events)} days?`,
      description: 'The event log and card timelines lose everything before then. Cards and entries stay.',
      confirm: 'Delete events',
    },
    invocations: {
      title: `Delete invocations older than ${days(invocations)} days?`,
      description: 'The record of commands agents ran before then is removed.',
      confirm: 'Delete invocations',
    },
    revisions: {
      title: `Keep only the last ${stats?.history_keep ?? 0} revisions?`,
      description: 'Older revisions of every entry and card are removed, and their diffs with them.',
      confirm: 'Trim revisions',
    },
    leftovers: {
      title: 'Remove leftover revisions?',
      description: 'Revision folders whose entry no longer exists are deleted.',
      confirm: 'Remove revisions',
    },
  }
  const question = questions[asked]

  const button = (action: Action, label: string, disabled = false) => (
    <Button
      variant="outline"
      size="sm"
      disabled={disabled || running !== null}
      onClick={() => {
        if (action === 'compact') { void run(action); return }
        setAsked(action)
        setConfirming(action)
      }}
    >
      {running === action && <Spinner data-icon="inline-start" />}
      {label}
    </Button>
  )

  return (
    <>
      <PageHeader title="Maintenance" description="Keep the database small. Anything deleted here is gone for good." />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not read the database</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {stats ? (
        <p className="mt-2 text-sm text-muted-foreground">
          Database {bytes(stats.database_bytes)}, write-ahead log {bytes(stats.wal_bytes)},{' '}
          {stats.leftover_revisions} leftover revision {stats.leftover_revisions === 1 ? 'folder' : 'folders'}.
        </p>
      ) : (
        !error && <Skeleton className="mt-2 h-5 w-80" />
      )}

      <ul className="mt-8 flex flex-col">
        {[
          {
            id: 'events',
            title: 'Prune events',
            description: 'Delete old entries from the event log.',
            control: (
              <>
                <RetentionSelect label="Events to delete" value={events} onChange={setEvents} />
                {button('events', 'Prune')}
              </>
            ),
          },
          {
            id: 'invocations',
            title: 'Prune invocations',
            description: 'Delete the old record of commands agents ran.',
            control: (
              <>
                <RetentionSelect label="Invocations to delete" value={invocations} onChange={setInvocations} />
                {button('invocations', 'Prune')}
              </>
            ),
          },
          {
            id: 'revisions',
            title: 'Trim revisions',
            description: `Keep the last ${stats?.history_keep ?? '…'} revisions of each entry and card, as history.keep says.`,
            control: button('revisions', 'Trim', !stats),
          },
          {
            id: 'leftovers',
            title: 'Remove leftover revisions',
            description: 'Delete the revisions of entries that no longer exist.',
            control: button('leftovers', 'Remove', !stats || stats.leftover_revisions === 0),
          },
          {
            id: 'compact',
            title: 'Compact the database',
            description: 'Rewrite the file to give back the space deletions left.',
            control: button('compact', 'Compact'),
          },
        ].map((row, index) => (
          <Fragment key={row.id}>
            {index > 0 && <Separator />}
            <MaintenanceRow title={row.title} description={row.description}>{row.control}</MaintenanceRow>
          </Fragment>
        ))}
      </ul>

      <ConfirmDialog
        open={confirming !== null && confirming !== 'compact'}
        busy={running !== null}
        title={question.title}
        description={question.description}
        confirm={question.confirm}
        onOpenChange={(open) => { if (!open) setConfirming(null) }}
        onConfirm={() => { if (confirming) void run(confirming) }}
      />
    </>
  )
}

/** One maintenance task: what it does on the left, how to run it on the right. */
function MaintenanceRow({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 py-4">
      <div className="flex min-w-0 flex-col gap-1">
        <p className="text-sm font-medium">{title}</p>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </li>
  )
}

/** How old something must be to go. */
function RetentionSelect({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <Select items={RETENTION.map(({ value: item, label: text }) => ({ value: item, label: text }))} value={value} onValueChange={(next) => { if (next) onChange(next) }}>
      <SelectTrigger size="sm" aria-label={label} className="w-44">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {RETENTION.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}
