import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { readError } from '@/lib/api'

interface DaemonLog {
  path: string
  exists: boolean
  lines: string[]
}

const LINES = 500

/**
 * The end of the daemon's log, newest at the bottom and scrolled to it. Only a
 * daemon started by Trellis writes this file; one under a service manager
 * logs where that manager keeps logs, and the page says where.
 */
export function SettingsLogs() {
  const [log, setLog] = useState<DaemonLog | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const area = useRef<HTMLDivElement>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const response = await fetch(`/api/logs?lines=${LINES}`, { signal })
      if (!response.ok) throw new Error(await readError(response))
      setLog((await response.json()) as DaemonLog)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not read the log')
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  // Every load lands on the newest line.
  useLayoutEffect(() => {
    const viewport = area.current?.querySelector<HTMLElement>('[data-slot=scroll-area-viewport]')
    if (viewport) viewport.scrollTop = viewport.scrollHeight
  }, [log])

  return (
    <>
      <PageHeader
        title="Logs"
        description={log ? <span className="text-meta break-all">{log.path}</span> : `The last ${LINES} lines the daemon wrote.`}
        actions={
          <Button variant="outline" size="sm" disabled={loading} onClick={() => void load()}>
            {loading && <Spinner data-icon="inline-start" />}
            Refresh
          </Button>
        }
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not read the log</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!log && !error && <Skeleton className="mt-6 h-[60dvh] w-full" />}

      {log && !log.exists && (
        <Empty className="mt-16">
          <EmptyHeader>
            <EmptyTitle>No log file</EmptyTitle>
            <EmptyDescription className="text-pretty">
              A daemon run by systemd logs to the journal: <span className="text-meta whitespace-nowrap">journalctl --user -u trellis</span>.
              One run by launchd keeps no log file.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {log?.exists && (
        <ScrollArea ref={area} className="mt-6 h-[60dvh] border bg-card">
          {log.lines.length === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">The log is empty.</p>
          ) : (
            <pre className="p-4 text-meta break-words whitespace-pre-wrap">{log.lines.join('\n')}</pre>
          )}
        </ScrollArea>
      )}
    </>
  )
}
