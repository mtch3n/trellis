import { useState } from 'react'
import { Ellipsis } from 'lucide-react'
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
import { Switch } from '@/components/ui/switch'
import { Spinner } from '@/components/ui/spinner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/wrappers/ConfirmDialog'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import type { Refusal } from '@/lib/api'

/** Which form the menu has open. */
type Form = 'new' | 'rename' | null

/**
 * A board's own settings, behind one button in its header: another board,
 * renaming this one, making it the board the project opens on, the project's
 * labels, and deleting it.
 *
 * Renaming keeps the board's slug, because a `.trellis` marker names the slug
 * and a rename must not strand a repository. Deleting a board with cards on it
 * needs saying so twice, and a project always keeps one board.
 */
export function BoardMenu({ board, cards, isDefault, onCreate, onRename, onSetDefault, onDelete, onLabels }: {
  /** The board's name, as a person reads it. */
  board: string
  /** How many cards are on it, so deleting can say what goes. */
  cards: number
  isDefault: boolean
  onCreate: (name: string, seedColumns: boolean) => Promise<Refusal | null>
  onRename: (name: string) => Promise<Refusal | null>
  onSetDefault: () => Promise<Refusal | null>
  /** Force is sent when the board still holds cards. */
  onDelete: (force: boolean) => Promise<Refusal | null>
  onLabels: () => void
}) {
  const [form, setForm] = useState<Form>(null)
  const [name, setName] = useState('')
  const [seedColumns, setSeedColumns] = useState(true)
  const [refusal, setRefusal] = useState<Refusal | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)

  const open = (which: Form) => {
    setName(which === 'rename' ? board : '')
    setSeedColumns(true)
    setRefusal(null)
    setForm(which)
  }

  const submit = async () => {
    setBusy(true)
    const said = form === 'new' ? await onCreate(name.trim(), seedColumns) : await onRename(name.trim())
    setBusy(false)
    setRefusal(said)
    if (!said) setForm(null)
  }

  const remove = async () => {
    setBusy(true)
    const said = await onDelete(cards > 0)
    setBusy(false)
    if (said) setRefusal(said)
    else setConfirming(false)
  }

  return (
    <>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger
            render={<DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Board settings" />} />}
          >
            <Ellipsis />
          </TooltipTrigger>
          <TooltipContent side="bottom">Board settings</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuItem onClick={() => open('new')}>New board</DropdownMenuItem>
          <DropdownMenuItem onClick={() => open('rename')}>Rename this board</DropdownMenuItem>
          <DropdownMenuItem disabled={isDefault} onClick={() => void onSetDefault()}>
            {isDefault ? 'Opens by default' : 'Open this board by default'}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onLabels}>Labels…</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => { setRefusal(null); setConfirming(true) }}>
            Delete this board
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {form && (
        <Dialog open onOpenChange={(next) => { if (!busy && !next) setForm(null) }}>
          <DialogContent className="sm:max-w-md">
            <DialogTitle>{form === 'new' ? 'New board' : `Rename ${board}`}</DialogTitle>
            <DialogDescription>
              {form === 'new'
                ? 'A second board keeps a different stream of work apart. Cards stay on the board they were made on.'
                : 'The name is what people read. The board keeps its address, so a repository pointing at it still resolves.'}
            </DialogDescription>
            <Field>
              <FieldLabel htmlFor="board-name">Name</FieldLabel>
              <Input
                id="board-name"
                value={name}
                autoFocus
                autoComplete="off"
                onChange={(event) => setName(event.target.value)}
                onKeyDown={(event) => { if (event.key === 'Enter' && name.trim()) void submit() }}
              />
            </Field>
            {form === 'new' && (
              <Field orientation="horizontal">
                <Switch id="board-columns" checked={seedColumns} onCheckedChange={setSeedColumns} />
                <div className="flex flex-col gap-0.5">
                  <FieldLabel htmlFor="board-columns">Start with the usual columns</FieldLabel>
                  <FieldDescription>Backlog, in-progress, review and done. Without them, add your own.</FieldDescription>
                </div>
              </Field>
            )}
            {refusal && <RefusalAlert refusal={refusal} />}
            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => setForm(null)}>Cancel</Button>
              <Button size="sm" disabled={busy || !name.trim()} onClick={() => void submit()}>
                {busy && <Spinner data-icon="inline-start" />}
                {form === 'new' ? 'Create board' : 'Rename board'}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}

      <ConfirmDialog
        open={confirming}
        busy={busy}
        title={`Delete ${board}?`}
        description={
          cards > 0
            ? `The board goes with its ${cards} ${cards === 1 ? 'card' : 'cards'}. The event log keeps a record; nothing else does.`
            : 'The board is empty, so nothing on it is lost. A project always keeps one board.'
        }
        confirm="Delete board"
        onOpenChange={(next) => { if (!next) setConfirming(false) }}
        onConfirm={() => void remove()}
      />
    </>
  )
}
