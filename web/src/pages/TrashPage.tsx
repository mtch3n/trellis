import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { TrashTable } from '@/components/wrappers/TrashTable'
import { readTrash, type TrashItem } from '@/lib/trash'

/**
 * What this project's rm has taken: cards, entries, artifacts and boards,
 * each restorable until trash.retention passes. Nothing here deletes; the
 * purge does that on its own schedule.
 */
export function TrashPage() {
  const { projectKey } = useParams<{ projectKey: string }>()
  const [items, setItems] = useState<TrashItem[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!projectKey) return
    try {
      setItems(await readTrash(`/api/p/${projectKey}/trash`, signal))
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not read the trash')
      setItems([])
    }
  }, [projectKey])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  return (
    <main className="mx-auto w-full max-w-5xl px-6 pb-24 lg:px-8">
      <PageHeader
        title="Trash"
        description="What was removed from this project. Each item can be restored until trash.retention passes; then the purge takes it."
        facts={[{ label: 'Items', value: items?.length ?? 0 }]}
      />

      {error && (
        <Alert variant="destructive" className="mt-4">
          <AlertTitle>Could not read the trash</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!items ? (
        <div className="mt-6 flex flex-col gap-3">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : items.length === 0 ? (
        <Empty className="mt-10">
          <EmptyHeader>
            <EmptyTitle>The trash is empty</EmptyTitle>
            <EmptyDescription>Removing a card, entry, artifact or board puts it here.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <TrashTable items={items} projectKey={projectKey ?? null} label="Trash pages" onRestored={() => void load()} />
      )}
    </main>
  )
}
