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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { IconButton } from '@/components/wrappers/IconButton'
import { RELATIONS } from '@/lib/cards'
import { sentence } from '@/lib/format'
import { cn } from '@/lib/utils'

/** How another card relates to this one, read from this card's side. */
export interface Relation {
  rel: string
  ref: string
  title: string
  column: string
  /** The other card sits in a done column. */
  done: boolean
}

export interface CardOption {
  ref: string
  title: string
}

/**
 * The cards this one stands in relation to, grouped by how, each with where
 * it is now: finished, or still in a column. A blocker that is not finished
 * reads as a warning, because that is what makes this card wait. Relating and
 * unrelating apply at once, like the other facts beside them.
 */
export function RelationsEditor({
  relations,
  cards,
  base,
  disabledReason,
  onAdd,
  onRemove,
}: {
  relations: Relation[]
  /** Cards that could be related, the current one already left out. */
  cards: CardOption[]
  base: string
  /** Set when the card cannot be changed right now, e.g. an agent holds it. */
  disabledReason?: string
  /** Resolves true when the relation was recorded. */
  onAdd: (relation: { rel: string; ref: string }) => Promise<boolean>
  onRemove: (relation: { rel: string; ref: string }) => Promise<void>
}) {
  const [adding, setAdding] = useState(false)
  const [rel, setRel] = useState<string>('relates_to')
  const [target, setTarget] = useState<CardOption | null>(null)
  const [busy, setBusy] = useState(false)
  const disabled = Boolean(disabledReason)

  const groups = RELATIONS.map((kind) => ({ ...kind, rows: relations.filter((row) => row.rel === kind.value) }))
    .filter((group) => group.rows.length > 0)
  const taken = new Set(relations.filter((row) => row.rel === rel).map((row) => row.ref))
  const options = cards.filter((card) => !taken.has(card.ref))

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!target) return
    setBusy(true)
    const recorded = await onAdd({ rel, ref: target.ref })
    setBusy(false)
    if (recorded) {
      setTarget(null)
      setAdding(false)
    }
  }

  return (
    <div className="flex flex-col gap-3">
      {groups.length === 0 && !adding && <p className="text-sm text-muted-foreground">No related cards.</p>}

      {groups.map((group) => (
        <section key={group.value}>
          <h3 className="text-xs text-muted-foreground">{group.label}</h3>
          <ul className="-mx-2 mt-1 flex flex-col">
            {group.rows.map((row) => (
              <li key={row.ref} className="group/relation relative">
                <Link
                  to={`${base}/card/${encodeURIComponent(row.ref)}`}
                  className="flex flex-col gap-0.5 px-2 py-1.5 pr-8 transition-colors hover:bg-accent/50"
                >
                  <span className="flex items-center gap-2">
                    <span className="text-meta text-muted-foreground">{row.ref}</span>
                    <span
                      className={cn(
                        'ml-auto text-xs',
                        !row.done && group.value === 'blocked_by' ? 'text-danger' : 'text-muted-foreground',
                      )}
                    >
                      {row.done ? 'Done' : sentence(row.column)}
                    </span>
                  </span>
                  <span className={cn('truncate text-sm', row.done && 'text-muted-foreground')}>{row.title}</span>
                </Link>
                <IconButton
                  label={`Remove ${group.label.toLowerCase()} ${row.ref}`}
                  size="icon-xs"
                  side="left"
                  disabled={disabled}
                  className="absolute top-1.5 right-1 opacity-0 transition-opacity group-hover/relation:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
                  onClick={() => void onRemove({ rel: row.rel, ref: row.ref })}
                >
                  <X />
                </IconButton>
              </li>
            ))}
          </ul>
        </section>
      ))}

      {adding ? (
        <form className="flex flex-col gap-2" onSubmit={submit}>
          <Select
            items={RELATIONS.map((kind) => ({ value: kind.value, label: kind.label }))}
            value={rel}
            onValueChange={(value) => { if (value) setRel(value) }}
          >
            <SelectTrigger aria-label="Relation" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RELATIONS.map((kind) => (
                <SelectItem key={kind.value} value={kind.value}>{kind.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Combobox
            autoHighlight
            items={options}
            value={target}
            onValueChange={(value) => setTarget(value)}
            itemToStringLabel={(card) => `${card.ref} ${card.title}`}
            itemToStringValue={(card) => card.ref}
            isItemEqualToValue={(card, value) => card.ref === value.ref}
          >
            <ComboboxInput aria-label="Card" placeholder="Find a card" className="w-full" autoFocus />
            <ComboboxContent>
              <ComboboxEmpty>No card matches.</ComboboxEmpty>
              <ComboboxList>
                {(card: CardOption) => (
                  <ComboboxItem key={card.ref} value={card}>
                    <span className="shrink-0 text-meta text-muted-foreground">{card.ref}</span>
                    <span className="min-w-0 truncate">{card.title}</span>
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
              Relate
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
          Add relation
        </Button>
      )}
    </div>
  )
}
