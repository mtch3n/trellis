import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { toast } from '@/components/ui/toast'
import { Paged } from '@/components/wrappers/Paged'
import { shortActor } from '@/lib/cards'
import { ago } from '@/lib/format'
import { restoreTrash, type TrashItem } from '@/lib/trash'

const KIND: Record<TrashItem['kind'], string> = {
  card: 'Card', entry: 'Entry', artifact: 'Artifact', board: 'Board', project: 'Project',
}

/** The day the purge takes an item, said as a date rather than a countdown. */
function purgeDay(ms: number) {
  return new Date(ms).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

/**
 * Trashed items with a Restore button each. A restore that collides with
 * something live — a slug taken since, a board still in the trash — is
 * refused by the server, and its reason is shown as it came.
 */
export function TrashTable({
  items,
  projectKey,
  label,
  onRestored,
}: {
  items: TrashItem[]
  projectKey: string | null
  label: string
  onRestored: () => void
}) {
  const [busy, setBusy] = useState<string | null>(null)

  const restore = async (item: TrashItem) => {
    setBusy(item.id || item.name)
    try {
      await restoreTrash(projectKey, item)
      toast.add({ title: `Restored ${item.name}`, type: 'success' })
      onRestored()
    } catch (err) {
      toast.add({ title: `Could not restore ${item.name}`, description: err instanceof Error ? err.message : undefined, type: 'error' })
    } finally {
      setBusy(null)
    }
  }

  return (
    <Paged items={items} label={label}>
      {(page) => (
        <Table className="mt-3">
          <TableHeader>
            <TableRow>
              <TableHead className="w-24">Kind</TableHead>
              <TableHead>Name</TableHead>
              <TableHead className="w-40">Trashed</TableHead>
              <TableHead className="w-36">Purged on</TableHead>
              <TableHead className="w-28" aria-label="Actions" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {page.map((item) => {
              const key = item.id || item.name
              return (
                <TableRow key={key}>
                  <TableCell className="text-muted-foreground">{KIND[item.kind]}</TableCell>
                  <TableCell>
                    <span className="font-medium">{item.name}</span>
                    {item.title && item.title !== item.name && (
                      <span className="ml-2 text-muted-foreground">{item.title}</span>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {ago(item.trashed_at)}{item.trashed_by ? ` by ${shortActor(item.trashed_by)}` : ''}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{purgeDay(item.purge_at)}</TableCell>
                  <TableCell className="text-right">
                    <Button variant="outline" size="sm" disabled={busy !== null} onClick={() => void restore(item)}>
                      {busy === key && <Spinner data-icon="inline-start" />}
                      Restore
                    </Button>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </Paged>
  )
}
