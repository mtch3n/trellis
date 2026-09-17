import { useState } from 'react'
import { Ellipsis, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { IconButton } from '@/components/wrappers/IconButton'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import { sentence } from '@/lib/format'
import type { Refusal } from '@/lib/api'

/** What a column form is for. */
type Form = 'add' | 'rename' | 'delete' | null

export interface ColumnActions {
  add: (name: string, after: string, done: boolean) => Promise<Refusal | null>
  rename: (column: string, name: string) => Promise<Refusal | null>
  move: (column: string, after: string) => Promise<Refusal | null>
  remove: (column: string, moveCardsTo: string) => Promise<Refusal | null>
}

/**
 * One column's own settings, on its header: renaming it, moving it along the
 * board, adding another beside it, and removing it.
 *
 * A column's place is "after this one", which is how the CLI takes it and how
 * a reader thinks about a strip, so moving is left and right rather than a
 * number. Removing a column with cards in it asks where the cards go, because
 * the cards are the work and the column is only where they sit.
 */
export function ColumnMenu({ column, columns, cards, actions }: {
  column: string
  /** Every column on the board, in order. */
  columns: string[]
  /** How many cards are in this one, so removing can ask about them. */
  cards: number
  actions: ColumnActions
}) {
  const at = columns.indexOf(column)
  const first = at <= 0
  const last = at === columns.length - 1

  return (
    <ColumnForms column={column} columns={columns} cards={cards} actions={actions}>
      {(open) => (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={`${sentence(column)} settings`}
                className="text-muted-foreground"
              />
            }
          >
            <Ellipsis />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuItem onClick={() => open('rename')}>Rename column</DropdownMenuItem>
            <DropdownMenuItem
              disabled={first}
              onClick={() => void actions.move(column, at >= 2 ? columns[at - 2] : '')}
            >
              Move left
            </DropdownMenuItem>
            <DropdownMenuItem disabled={last} onClick={() => void actions.move(column, columns[at + 1])}>
              Move right
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => open('add')}>Add a column after this</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => open('delete')}>
              Delete column
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </ColumnForms>
  )
}

/**
 * Adding the first column. A board started without the usual four has nowhere
 * to put a card until it has one, so the empty board offers this directly.
 */
export function AddColumnButton({ columns, actions, className }: {
  columns: string[]
  actions: ColumnActions
  className?: string
}) {
  return (
    <ColumnForms column={columns[columns.length - 1] ?? ''} columns={columns} cards={0} actions={actions}>
      {(open) => (
        <IconButton label="Add a column" className={className} onClick={() => open('add')}>
          <Plus />
        </IconButton>
      )}
    </ColumnForms>
  )
}

/**
 * The forms a column's settings need — add, rename, delete — kept in one
 * place for whichever control opens them. The trigger is the caller's, so a
 * menu and a button can share the same three dialogs.
 */
function ColumnForms({ column, columns, cards, actions, children }: {
  column: string
  columns: string[]
  cards: number
  actions: ColumnActions
  children: (open: (form: Form) => void) => React.ReactNode
}) {
  const [form, setForm] = useState<Form>(null)
  const [name, setName] = useState('')
  const [done, setDone] = useState(false)
  const [moveTo, setMoveTo] = useState('')
  const [refusal, setRefusal] = useState<Refusal | null>(null)
  const [busy, setBusy] = useState(false)

  const open = (which: Form) => {
    setName(which === 'rename' ? column : '')
    setDone(false)
    setMoveTo(columns.find((other) => other !== column) ?? '')
    setRefusal(null)
    setForm(which)
  }

  const submit = async () => {
    setBusy(true)
    const said = form === 'add'
      ? await actions.add(name.trim(), column, done)
      : form === 'rename'
        ? await actions.rename(column, name.trim())
        : await actions.remove(column, cards > 0 ? moveTo : '')
    setBusy(false)
    setRefusal(said)
    if (!said) setForm(null)
  }

  const ready = form === 'delete' ? cards === 0 || moveTo !== '' : name.trim() !== ''
  const words = {
    add: { title: 'New column', confirm: 'Add column' },
    rename: { title: `Rename ${column}`, confirm: 'Rename column' },
    delete: { title: `Delete ${column}?`, confirm: 'Delete column' },
  }

  return (
    <>
      {children(open)}
      {form && (
        <Dialog open onOpenChange={(next) => { if (!busy && !next) setForm(null) }}>
          <DialogContent className="sm:max-w-md">
            <DialogTitle>{words[form].title}</DialogTitle>
            <DialogDescription>
              {form === 'add'
                ? `It goes after ${column || 'the last column'}. A done column is where finished work lands, which is what releases a claim.`
                : form === 'rename'
                  ? 'Cards keep their place; only the name changes.'
                  : cards > 0
                    ? `${cards} ${cards === 1 ? 'card is' : 'cards are'} in this column, so they need somewhere to go.`
                    : 'The column is empty, so nothing moves.'}
            </DialogDescription>

            {form !== 'delete' && (
              <Field>
                <FieldLabel htmlFor="column-name">Name</FieldLabel>
                <Input
                  id="column-name"
                  value={name}
                  autoFocus
                  autoComplete="off"
                  onChange={(event) => setName(event.target.value)}
                  onKeyDown={(event) => { if (event.key === 'Enter' && ready) void submit() }}
                />
              </Field>
            )}

            {form === 'add' && (
              <Field orientation="horizontal">
                <Switch id="column-done" checked={done} onCheckedChange={setDone} />
                <div className="flex flex-col gap-0.5">
                  <FieldLabel htmlFor="column-done">Finished work lands here</FieldLabel>
                  <FieldDescription>A card moved into a done column releases its claim.</FieldDescription>
                </div>
              </Field>
            )}

            {form === 'delete' && cards > 0 && (
              <Field>
                <FieldLabel htmlFor="column-move">Move the cards to</FieldLabel>
                <Select
                  items={columns.filter((other) => other !== column).map((other) => ({ value: other, label: sentence(other) }))}
                  value={moveTo}
                  onValueChange={(value) => { if (value) setMoveTo(value) }}
                >
                  <SelectTrigger id="column-move" className="w-full">
                    <SelectValue placeholder="Pick a column" />
                  </SelectTrigger>
                  <SelectContent>
                    {columns.filter((other) => other !== column).map((other) => (
                      <SelectItem key={other} value={other}>{sentence(other)}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}

            {refusal && <RefusalAlert refusal={refusal} />}

            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => setForm(null)}>Cancel</Button>
              <Button
                size="sm"
                variant={form === 'delete' ? 'destructive' : 'default'}
                disabled={busy || !ready}
                onClick={() => void submit()}
              >
                {busy && <Spinner data-icon="inline-start" />}
                {words[form].confirm}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
