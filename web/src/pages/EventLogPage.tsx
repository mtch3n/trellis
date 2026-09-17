import { useEffect, useState } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { Paged } from '@/components/wrappers/Paged'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { shortActor } from '@/lib/cards'
import { readError } from '@/lib/api'
import { actionLabel } from '@/lib/events'

interface Event {
  seq: number
  timestamp: number
  actor: string
  entity: string
  action: string
  field?: string
  title: string
  project: string
}

function clock(timestamp: number) {
  return new Date(timestamp).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** Everything every agent did, newest first, across every project. */
export function EventLogPage() {
  const [events, setEvents] = useState<Event[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/events?limit=200', { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json()
      })
      .then(setEvents)
      .catch((err: unknown) => {
        if (err instanceof Error && err.name !== 'AbortError') { setError(err.message); setEvents([]) }
      })
    return () => controller.abort()
  }, [])

  return (
    <main className="px-6 pb-24 lg:px-8">
      <PageHeader title="Event log" />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not load the event log</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!events && (
        <div className="mt-8 flex flex-col gap-2">
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </div>
      )}

      {events && events.length === 0 && !error && (
        <Empty className="mt-16">
          <EmptyHeader>
            <EmptyTitle>The event log is empty</EmptyTitle>
            <EmptyDescription>What agents do on boards and in the vault shows up here as it happens.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {events && events.length > 0 && (
        <Paged items={events} label="Event log pages">
          {(page) => (
        <Table className="mt-6">
          <TableHeader>
            <TableRow>
              <TableHead className="w-40">When</TableHead>
              <TableHead className="w-32">Agent</TableHead>
              <TableHead className="w-28">Action</TableHead>
              <TableHead className="w-36">Project</TableHead>
              <TableHead>What</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {page.map((event) => (
              <TableRow key={event.seq}>
                <TableCell className="text-muted-foreground">{clock(event.timestamp)}</TableCell>
                <TableCell className="text-meta text-muted-foreground">{shortActor(event.actor) ?? event.actor}</TableCell>
                <TableCell className="text-muted-foreground">{actionLabel(event.action)}</TableCell>
                <TableCell className="text-muted-foreground">{event.project}</TableCell>
                <TableCell>{event.title}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
          )}
        </Paged>
      )}
    </main>
  )
}
