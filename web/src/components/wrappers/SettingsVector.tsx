import { useCallback, useEffect, useState } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { toast } from '@/components/ui/toast'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { readError } from '@/lib/api'

interface VectorStatus {
  enabled: boolean
  provider?: string
  configured_entries: number
  indexed_entries: number
  unindexed_entries: number
}

type Action = 'rebuild' | 'prune' | 'reindex' | 'compact'

/** What each action does, in the words the CLI uses for it. */
const ACTIONS: { id: Action; title: string; description: string; run: string }[] = [
  {
    id: 'rebuild',
    title: 'Rebuild the index',
    description: 'Embed every entry again. This calls the embedder once per entry, so it can take a while.',
    run: 'Rebuild',
  },
  {
    id: 'prune',
    title: 'Remove stale vectors',
    description: 'Delete the vectors of entries that have left the vault.',
    run: 'Prune',
  },
  {
    id: 'reindex',
    title: 'Rebuild the search cache',
    description: "Make the extension rebuild its persisted nearest-neighbour cache.",
    run: 'Reindex',
  },
  {
    id: 'compact',
    title: 'Compact the index',
    description: 'Checkpoint and rewrite the vector database to give back the space deletions left.',
    run: 'Compact',
  },
]

/**
 * One project's vector index: whether vector search is configured at all, how
 * much of the vault is in the index, and the four things that can be done to
 * it. Vector search being off is a state rather than a fault — text search
 * carries on without it — so the page says so and offers nothing to run.
 *
 * The index belongs to a project, not to the machine, so the project is
 * picked here rather than assumed.
 */
export function SettingsVector() {
  const [projects, setProjects] = useState<string[]>([])
  const [project, setProject] = useState('')
  const [status, setStatus] = useState<VectorStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [running, setRunning] = useState<Action | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/projects', { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : []))
      .then((data: { key: string }[]) => {
        setProjects(data.map((item) => item.key))
        setProject((current) => current || data[0]?.key || '')
      })
      .catch(() => { /* the page then says it could not read anything */ })
    return () => controller.abort()
  }, [])

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!project) return
    try {
      const response = await fetch(`/api/p/${project}/vector`, { signal })
      if (!response.ok) throw new Error(await readError(response))
      setStatus((await response.json()) as VectorStatus)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not read the index')
    }
  }, [project])

  useEffect(() => {
    const controller = new AbortController()
    setStatus(null)
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const run = async (action: Action) => {
    setRunning(action)
    try {
      const response = await fetch(`/api/p/${project}/vector/${action}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      })
      if (!response.ok) throw new Error(await readError(response))
      const answer = (await response.json()) as Record<string, string | number>
      const [key, value] = Object.entries(answer)[0] ?? ['done', '']
      toast.add({ title: `${key === 'status' || key === 'result' ? value : `${value} ${key}`}`, type: 'success' })
      await load()
    } catch (err) {
      toast.add({
        title: 'The index was not changed',
        description: err instanceof Error ? err.message : undefined,
        type: 'error',
      })
    } finally {
      setRunning(null)
    }
  }

  return (
    <>
      <PageHeader
        title="Vector index"
        description="Search by meaning, one index per project. Text search works whether or not this is on."
        actions={
          projects.length > 0 && (
            <Select
              items={projects.map((key) => ({ value: key, label: key }))}
              value={project}
              onValueChange={(value) => { if (value) setProject(value) }}
            >
              <SelectTrigger size="sm" aria-label="Project" className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {projects.map((key) => <SelectItem key={key} value={key}>{key}</SelectItem>)}
              </SelectContent>
            </Select>
          )
        }
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not read the index</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!status && !error && <Skeleton className="mt-4 h-6 w-72" />}

      {status && !status.enabled && (
        <p className="mt-2 text-sm text-pretty text-muted-foreground">
          Vector search is off. Turn it on in Settings › General
          (<span className="text-meta">search.vector.enabled</span>) and give it a provider; until then
          searching uses the words as written.
        </p>
      )}

      {status?.enabled && (
        <>
          <p className="mt-2 text-sm text-muted-foreground">
            {status.indexed_entries} of {status.configured_entries} entries indexed
            {status.unindexed_entries > 0 ? `, ${status.unindexed_entries} waiting` : ''}
            {status.provider ? ` · ${status.provider}` : ''}.
          </p>

          <ul className="mt-8 flex flex-col">
            {ACTIONS.map((action, index) => (
              <li key={action.id}>
                {index > 0 && <Separator />}
                <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 py-4">
                  <div className="flex min-w-0 flex-col gap-1">
                    <p className="text-sm font-medium">{action.title}</p>
                    <p className="text-sm text-pretty text-muted-foreground">{action.description}</p>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={running !== null}
                    onClick={() => void run(action.id)}
                  >
                    {running === action.id && <Spinner data-icon="inline-start" />}
                    {action.run}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </>
  )
}
