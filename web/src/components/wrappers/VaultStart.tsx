import { Link } from 'react-router-dom'
import { Network, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { ago } from '@/lib/format'
import type { Entry } from '@/lib/entry'
import type { Opened } from '@/lib/recent'

const ROWS = 6

interface Row {
  slug: string
  title: string
  /** When, beside the title. Pins have none: they stay until unpinned. */
  note?: string
}

/**
 * What the vault shows with no entry open: somewhere to go next rather than
 * a count. The entries this browser opened last, the ones edited last by
 * anyone, and the pinned ones every session starts from. A list with nothing
 * in it is left out, and a vault with nothing in it says so.
 */
export function VaultStart({
  projectKey,
  entries,
  opened,
  pins,
  missing,
  onCreate,
  onOpenGraph,
}: {
  projectKey: string
  entries: Entry[]
  opened: Opened[]
  pins: { slug: string; title: string }[]
  /** The slug in the address, when no entry answers to it. */
  missing?: string
  onCreate: () => void
  /** Absent when the graph would be empty. */
  onOpenGraph?: () => void
}) {
  const actions = (
    <>
      <Button variant="outline" size="sm" onClick={onCreate}>
        <Plus data-icon="inline-start" />
        New entry
      </Button>
      {onOpenGraph && (
        <Button variant="outline" size="sm" onClick={onOpenGraph}>
          <Network data-icon="inline-start" />
          Open the graph
        </Button>
      )}
    </>
  )

  if (entries.length === 0) {
    return (
      <Empty className="min-h-96 border-0">
        <EmptyHeader>
          <EmptyTitle>Nothing written yet</EmptyTitle>
          <EmptyDescription>Agents write entries here as they work.</EmptyDescription>
        </EmptyHeader>
        <EmptyContent className="flex-row justify-center">{actions}</EmptyContent>
      </Empty>
    )
  }

  const bySlug = new Map(entries.map((entry) => [entry.slug, entry]))
  const lists: { label: string; rows: Row[] }[] = [
    {
      label: 'Opened recently',
      rows: opened
        .filter((item) => bySlug.has(item.slug))
        .slice(0, ROWS)
        .map((item) => ({ slug: item.slug, title: bySlug.get(item.slug)!.title, note: ago(item.at) })),
    },
    {
      label: 'Edited recently',
      rows: entries
        .filter((entry) => entry.updated_at)
        .sort((a, b) => (b.updated_at ?? 0) - (a.updated_at ?? 0))
        .slice(0, ROWS)
        .map((entry) => ({ slug: entry.slug, title: entry.title, note: ago(entry.updated_at!) })),
    },
    { label: 'Pinned', rows: pins.slice(0, ROWS).map((pin) => ({ slug: pin.slug, title: pin.title })) },
  ].filter((list) => list.rows.length > 0)

  return (
    <div className="mx-auto flex max-w-reading flex-col gap-10 pt-3">
      <header className="flex flex-col gap-2">
        <h1 className="text-title">{missing ? 'No entry by that name' : 'Vault'}</h1>
        <p className="text-sm text-muted-foreground">
          {missing
            ? `Nothing in this project or the vault is called ${missing}.`
            : `${entries.length} ${entries.length === 1 ? 'entry' : 'entries'} in this project and the vault.`}
        </p>
        <div className="mt-2 flex gap-2">{actions}</div>
      </header>

      <div className="grid gap-x-10 gap-y-8 md:grid-cols-2">
        {lists.map((list) => (
          <section key={list.label} className="flex min-w-0 flex-col gap-2">
            <h2 className="text-label text-muted-foreground">{list.label}</h2>
            <ul className="-mx-2 flex flex-col">
              {list.rows.map((row) => (
                <li key={row.slug}>
                  <Link
                    to={`/p/${projectKey}/vault/${encodeURIComponent(row.slug)}`}
                    className="flex items-baseline gap-3 px-2 py-1.5 transition-colors hover:bg-accent/50"
                  >
                    <span className="min-w-0 flex-1 truncate text-sm">{row.title}</span>
                    {row.note && <span className="shrink-0 text-xs text-muted-foreground">{row.note}</span>}
                  </Link>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </div>
  )
}
