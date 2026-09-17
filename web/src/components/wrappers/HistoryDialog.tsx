import { useCallback, useEffect, useState } from 'react'
import { X } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { IconButton } from '@/components/wrappers/IconButton'
import { readError } from '@/lib/api'
import { ago } from '@/lib/format'
import { shortActor } from '@/lib/cards'
import { cn } from '@/lib/utils'

/** One retained version. A card's revisions also say who wrote them. */
export interface Revision {
  version: number
  timestamp: number
  actor?: string
}

/**
 * The revisions of a card or an entry, and what changed between two of them.
 * History means revisions here: a card's comments and events are its
 * Timeline, and this is the other thing.
 *
 * The list is on the left, newest first, and the diff for the selected
 * version fills the rest. Choosing a version shows what that save changed,
 * which is the question a reader actually has; the server answers it with a
 * unified diff, so it is rendered as text. Versions have gaps — a move bumps
 * a card's version without keeping a revision — so the list is what the
 * server kept, never a count.
 */
export function HistoryDialog({ open, title, base, onOpenChange }: {
  open: boolean
  /** What the history belongs to, for the dialog's heading: a ref or a slug. */
  title: string
  /** The API path whose `/history` and `/diff` this reads. */
  base: string
  onOpenChange: (open: boolean) => void
}) {
  const [revisions, setRevisions] = useState<Revision[] | null>(null)
  const [chosen, setChosen] = useState<number | null>(null)
  const [diff, setDiff] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    setRevisions(null)
    setChosen(null)
    setDiff(null)
    setError(null)
    fetch(`${base}/history`, { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return (await response.json()) as Revision[]
      })
      .then((list) => {
        setRevisions(list)
        setChosen(list[0]?.version ?? null)
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'Could not read the history')
      })
    return () => controller.abort()
  }, [open, base])

  const load = useCallback(async (version: number, signal: AbortSignal) => {
    // A save is read as the step from the version before it, which is what
    // "what changed" means; the oldest kept version has nothing before it.
    const response = await fetch(`${base}/diff?from=${version - 1}&to=${version}`, { signal })
    if (!response.ok) throw new Error(await readError(response))
    return ((await response.json()) as { diff: string }).diff
  }, [base])

  useEffect(() => {
    if (!open || chosen === null) return
    const controller = new AbortController()
    setDiff(null)
    load(chosen, controller.signal)
      .then(setDiff)
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setDiff('')
        setError(err instanceof Error ? err.message : 'Could not read that version')
      })
    return () => controller.abort()
  }, [open, chosen, load])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="top-[6dvh] flex max-h-[88dvh] translate-y-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl"
      >
        <header className="flex h-14 shrink-0 items-center gap-3 px-5">
          <div className="min-w-0">
            <DialogTitle className="truncate">History</DialogTitle>
            <DialogDescription className="truncate text-meta">{title}</DialogDescription>
          </div>
          <IconButton label="Close" className="ml-auto" onClick={() => onOpenChange(false)}>
            <X />
          </IconButton>
        </header>

        <div className="grid min-h-0 flex-1 gap-0 overflow-hidden md:grid-cols-history">
          <nav aria-label="Revisions" className="min-h-0 overflow-y-auto border-border px-2 pb-4 md:border-r">
            {revisions === null && !error && (
              <div className="flex flex-col gap-2 p-2">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
              </div>
            )}
            {revisions?.length === 0 && (
              <p className="p-2 text-sm text-muted-foreground">No revisions kept yet.</p>
            )}
            <ul className="flex flex-col">
              {(revisions ?? []).map((revision) => (
                <li key={revision.version}>
                  <Button
                    variant="ghost"
                    className={cn(
                      'h-auto w-full flex-col items-start gap-0.5 px-2 py-1.5 font-normal',
                      revision.version === chosen && 'bg-accent',
                    )}
                    aria-current={revision.version === chosen ? 'true' : undefined}
                    onClick={() => setChosen(revision.version)}
                  >
                    <span className="flex w-full items-baseline gap-2">
                      <span className="text-sm">v{revision.version}</span>
                      <span className="ml-auto text-xs text-muted-foreground">{ago(revision.timestamp)}</span>
                    </span>
                    {revision.actor && (
                      <span className="text-meta text-muted-foreground">{shortActor(revision.actor)}</span>
                    )}
                  </Button>
                </li>
              ))}
            </ul>
          </nav>

          <div className="min-h-0 overflow-auto px-5 pb-6">
            {error && (
              <Empty className="mt-10">
                <EmptyHeader>
                  <EmptyTitle>Nothing to show</EmptyTitle>
                  <EmptyDescription>{error}</EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
            {!error && diff === null && chosen !== null && (
              <div className="flex flex-col gap-2 pt-4">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-5/6" />
                <Skeleton className="h-4 w-2/3" />
              </div>
            )}
            {!error && diff !== null && (
              diff.trim() === '' ? (
                <p className="pt-4 text-sm text-muted-foreground">This version changed no text.</p>
              ) : (
                // The server's diff is unified text, so it is shown as text.
                <pre className="pt-4 text-meta leading-relaxed whitespace-pre-wrap">{diff}</pre>
              )
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
