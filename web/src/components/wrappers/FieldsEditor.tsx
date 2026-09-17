import { useState, type KeyboardEvent } from 'react'
import { X } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { IconButton } from '@/components/wrappers/IconButton'
import { sentence } from '@/lib/format'
import type { FieldRow } from '@/lib/templates'
import { cn } from '@/lib/utils'

/** The value a select holds while nothing is chosen. */
const UNSET = ''

/**
 * The fields an entry's template asks for, and any others its frontmatter
 * carries, changed where they are read. A field with fixed choices is a
 * select; any other is a line of text, saved when it is left or on Enter.
 * A list is shown but not edited here, because a line of text would flatten
 * it. A field the template does not name can be removed.
 *
 * Every change applies at once, like the other facts beside them. A strict
 * template can refuse one, and a refused value stays in its box to be fixed.
 */
export function FieldsEditor({
  rows,
  onSet,
}: {
  rows: FieldRow[]
  /** Resolves true when the server kept the value; "" removes the field. */
  onSet: (name: string, value: string) => Promise<boolean>
}) {
  return (
    <div className="flex flex-col gap-3">
      {rows.map((row) => {
        const id = `entry-field-${row.name}`
        return (
          <div key={row.name} className="flex flex-col gap-1">
            <Label htmlFor={id} className="text-xs font-normal text-muted-foreground">{sentence(row.name)}</Label>
            <div className="flex items-center gap-1">
              {Array.isArray(row.value) ? (
                <p id={id} className="min-w-0 flex-1 text-sm break-words" title="A list. Change it in the file.">
                  {row.value.join(', ')}
                </p>
              ) : row.options ? (
                <ChoiceField id={id} row={row} value={row.value ?? UNSET} onSet={onSet} />
              ) : (
                <TextField id={id} row={row} value={row.value ?? ''} onSet={onSet} />
              )}
              {!row.known && (
                <IconButton label={`Remove ${sentence(row.name).toLowerCase()}`} size="icon-sm" side="left" onClick={() => void onSet(row.name, '')}>
                  <X />
                </IconButton>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}

/** A field with fixed choices. A value the template no longer allows is still shown, so it can be seen and replaced. */
function ChoiceField({
  id,
  row,
  value,
  onSet,
}: {
  id: string
  row: FieldRow
  value: string
  onSet: (name: string, value: string) => Promise<boolean>
}) {
  const options = row.options ?? []
  const stale = value !== UNSET && !options.includes(value)
  const items = [
    ...(row.required && value !== UNSET ? [] : [{ value: UNSET, label: row.required ? 'Choose one' : 'None' }]),
    ...options.map((option) => ({ value: option, label: sentence(option) })),
    ...(stale ? [{ value, label: sentence(value) }] : []),
  ]

  return (
    <Select items={items} value={value} onValueChange={(next) => { if (next !== null && next !== value) void onSet(row.name, next) }}>
      <SelectTrigger id={id} className={cn('w-full', stale && 'text-danger')}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {items.map((item) => (
          <SelectItem key={item.value || 'unset'} value={item.value}>
            <span className={cn(item.value === UNSET && 'text-muted-foreground')}>{item.label}</span>
            {stale && item.value === value && <span className="ml-auto self-center pl-4 text-xs text-muted-foreground">Not allowed</span>}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** A field of free text. It saves when left or on Enter; Escape puts the saved value back. */
function TextField({
  id,
  row,
  value,
  onSet,
}: {
  id: string
  row: FieldRow
  value: string
  onSet: (name: string, value: string) => Promise<boolean>
}) {
  const [draft, setDraft] = useState(value)
  const [busy, setBusy] = useState(false)
  // A new saved value, from this box or from elsewhere, replaces the draft.
  // Adjusted while rendering, so the box keeps its focus.
  const [saved, setSaved] = useState(value)
  if (saved !== value) {
    setSaved(value)
    setDraft(value)
  }

  const commit = async () => {
    const next = draft.trim()
    if (next === value || busy) return
    setBusy(true)
    await onSet(row.name, next)
    setBusy(false)
  }

  const type = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      void commit()
    } else if (event.key === 'Escape') {
      event.stopPropagation()
      setDraft(value)
    }
  }

  return (
    <Input
      id={id}
      className="h-8"
      autoComplete="off"
      placeholder={row.required ? 'Required' : 'Not set'}
      // Read-only rather than disabled while saving, so the cursor stays.
      readOnly={busy}
      value={draft}
      onChange={(event) => setDraft(event.target.value)}
      onKeyDown={type}
      onBlur={() => void commit()}
    />
  )
}
