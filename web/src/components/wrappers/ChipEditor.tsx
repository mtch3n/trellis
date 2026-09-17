import { useState, type KeyboardEvent } from 'react'
import { Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'

/**
 * A set of short words on a card: its labels, or its tags. Each one carries
 * its own remove, and adding is either a pick from a list the project already
 * defines (labels) or free text (tags), which is exactly the difference
 * between the two. Changes apply as they are made, like the other facts
 * beside them, so they do not wait for an edit to be saved.
 */
export function ChipEditor({
  name,
  values,
  options,
  placeholder,
  disabledReason,
  onChange,
}: {
  /** What one of these is called, for the controls' labels: "label", "tag". */
  name: string
  values: string[]
  /** The words to pick from. Omitted where anything goes. */
  options?: string[]
  placeholder: string
  /** Set when the card cannot be changed right now, e.g. an agent holds it. */
  disabledReason?: string
  onChange: (change: { add?: string; remove?: string }) => void
}) {
  const [adding, setAdding] = useState(false)
  const [typed, setTyped] = useState('')
  const disabled = Boolean(disabledReason)
  const free = options === undefined
  const rest = options?.filter((option) => !values.includes(option)) ?? []

  const add = (value: string) => {
    const word = value.trim()
    if (!word || values.includes(word)) return
    onChange({ add: word })
  }

  const type = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      add(typed)
      setTyped('')
      return
    }
    if (event.key === 'Escape') {
      setAdding(false)
      setTyped('')
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {values.map((value) => (
        <Button
          key={value}
          variant="secondary"
          size="xs"
          className="gap-1 font-normal"
          disabled={disabled}
          title={disabledReason}
          aria-label={`Remove ${name} ${value}`}
          onClick={() => onChange({ remove: value })}
        >
          {value}
          <X data-icon="inline-end" className="text-muted-foreground" />
        </Button>
      ))}

      {free ? (
        adding ? (
          <Input
            autoFocus
            aria-label={`New ${name}`}
            placeholder={placeholder}
            className="h-6 w-28"
            value={typed}
            onChange={(event) => setTyped(event.target.value)}
            onKeyDown={type}
            onBlur={() => { add(typed); setTyped(''); setAdding(false) }}
          />
        ) : (
          <Button
            variant="ghost"
            size="xs"
            className="text-muted-foreground"
            disabled={disabled}
            title={disabledReason}
            onClick={() => setAdding(true)}
          >
            <Plus data-icon="inline-start" />
            Add
          </Button>
        )
      ) : (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="xs"
                className="text-muted-foreground"
                disabled={disabled || rest.length === 0}
                title={disabledReason ?? (rest.length === 0 ? `Every ${name} this project defines is already on this card` : undefined)}
              />
            }
          >
            <Plus data-icon="inline-start" />
            Add
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            {rest.map((option) => (
              <DropdownMenuItem key={option} onClick={() => add(option)}>
                {option}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  )
}
