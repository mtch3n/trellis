import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@/components/ui/combobox'
import { Spinner } from '@/components/ui/spinner'
import { IconButton } from '@/components/wrappers/IconButton'

/** One entry a card cites, as the card detail returns it. */
export interface CardLink {
  /** The text the link was written as: a slug, with an anchor when it has one. */
  raw: string
  anchor?: string
  /** The entry's address, or null when the entry has since been deleted. */
  to: string | null
  title?: string
}

export interface EntryOption {
  slug: string
  title: string
}

/** The slug inside an entry address (`/KEY/vault/ops/db` -> `ops/db`). */
function slugOf(address: string) {
  const [, , , ...rest] = address.split('/')
  return rest.join('/')
}

/**
 * The entries a card cites, in its facts column. A row opens the entry; an
 * anchor is kept, because it says which part of the entry the card leans on.
 * A citation whose entry has been deleted reads as a stub, dashed and muted,
 * the way an unresolved wikilink does — it is a loose end, not an alarm.
 * Adding picks an entry by typing, and every row removes itself.
 */
export function EntryLinksEditor({
  links,
  entries,
  projectKey,
  disabledReason,
  onAdd,
  onRemove,
}: {
  links: CardLink[]
  /** The entries that could be cited, this project's and the vault's. */
  entries: EntryOption[]
  projectKey: string
  /** Set when the card cannot be changed right now, e.g. an agent holds it. */
  disabledReason?: string
  /** Resolves true when the citation was recorded. */
  onAdd: (target: string) => Promise<boolean>
  onRemove: (target: string) => Promise<void>
}) {
  const [adding, setAdding] = useState(false)
  const [target, setTarget] = useState<EntryOption | null>(null)
  const [busy, setBusy] = useState(false)
  const disabled = Boolean(disabledReason)
  const cited = new Set(links.map((link) => (link.to ? slugOf(link.to) : link.raw)))
  const options = entries.filter((entry) => !cited.has(entry.slug))

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!target) return
    setBusy(true)
    const linked = await onAdd(target.slug)
    setBusy(false)
    if (linked) {
      setTarget(null)
      setAdding(false)
    }
  }

  return (
    <div className="flex flex-col gap-3">
      {links.length === 0 && !adding && <p className="text-sm text-muted-foreground">No entries cited.</p>}

      {links.length > 0 && (
        <ul className="-mx-2 flex flex-col">
          {links.map((link) => (
            <li key={link.raw} className="group/link relative">
              {link.to ? (
                <Link
                  to={`/p/${projectKey}/vault/${encodeURIComponent(slugOf(link.to))}`}
                  className="flex flex-col gap-0.5 px-2 py-1.5 pr-8 transition-colors hover:bg-accent/50"
                >
                  <span className="truncate text-sm">{link.title || slugOf(link.to)}</span>
                  <span className="truncate text-meta text-muted-foreground">
                    {slugOf(link.to)}{link.anchor ? `#${link.anchor}` : ''}
                  </span>
                </Link>
              ) : (
                <p className="px-2 py-1.5 pr-8" title="Nothing by that name is in the vault">
                  <span className="truncate text-meta text-muted-foreground">{link.raw}</span>
                  <span className="ml-2 text-xs text-muted-foreground">Stub</span>
                </p>
              )}
              <IconButton
                label={`Remove the citation of ${link.raw}`}
                size="icon-xs"
                side="left"
                disabled={disabled}
                className="absolute top-1.5 right-1 opacity-0 transition-opacity group-hover/link:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
                onClick={() => void onRemove(link.raw)}
              >
                <X />
              </IconButton>
            </li>
          ))}
        </ul>
      )}

      {adding ? (
        <form className="flex flex-col gap-2" onSubmit={submit}>
          <Combobox
            autoHighlight
            items={options}
            value={target}
            onValueChange={(value) => setTarget(value)}
            itemToStringLabel={(entry) => `${entry.title} ${entry.slug}`}
            itemToStringValue={(entry) => entry.slug}
            isItemEqualToValue={(entry, value) => entry.slug === value.slug}
          >
            <ComboboxInput aria-label="Entry" placeholder="Find an entry" className="w-full" autoFocus />
            <ComboboxContent>
              <ComboboxEmpty>No entry matches.</ComboboxEmpty>
              <ComboboxList>
                {(entry: EntryOption) => (
                  <ComboboxItem key={entry.slug} value={entry}>
                    <span className="min-w-0 truncate">{entry.title}</span>
                    <span className="shrink-0 text-meta text-muted-foreground">{entry.slug}</span>
                  </ComboboxItem>
                )}
              </ComboboxList>
            </ComboboxContent>
          </Combobox>
          <div className="flex justify-end gap-2">
            <Button type="button" size="sm" variant="ghost" onClick={() => { setAdding(false); setTarget(null) }}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={!target || busy}>
              {busy && <Spinner data-icon="inline-start" />}
              Cite it
            </Button>
          </div>
        </form>
      ) : (
        <Button
          variant="ghost"
          size="xs"
          className="self-start text-muted-foreground"
          disabled={disabled}
          title={disabledReason}
          onClick={() => setAdding(true)}
        >
          <Plus data-icon="inline-start" />
          Cite an entry
        </Button>
      )}
    </div>
  )
}
