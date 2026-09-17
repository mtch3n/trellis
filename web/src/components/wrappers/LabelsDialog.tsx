import { useState } from 'react'
import { Merge, X } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { IconButton } from '@/components/wrappers/IconButton'
import { ConfirmDialog } from '@/components/wrappers/ConfirmDialog'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import type { Refusal } from '@/lib/api'

/** One label of the project. */
export interface Label {
  name: string
  description?: string
}

/**
 * The project's label vocabulary: what a label means, a new one, merging two
 * that turned out to be the same word, and removing one.
 *
 * A label is the project's own vocabulary — cards pick from it and never
 * invent one — so a new label has to say what it means. Merging exists because
 * two words for one thing is the common mistake, and deleting a label that
 * cards still carry is refused by the server, which says how many.
 */
export function LabelsDialog({ open, labels, onOpenChange, onCreate, onDelete, onMerge }: {
  open: boolean
  labels: Label[]
  onOpenChange: (open: boolean) => void
  onCreate: (name: string, description: string) => Promise<Refusal | null>
  onDelete: (name: string) => Promise<Refusal | null>
  onMerge: (from: string, into: string) => Promise<Refusal | null>
}) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [merging, setMerging] = useState(false)
  const [from, setFrom] = useState('')
  const [into, setInto] = useState('')
  const [removing, setRemoving] = useState<string | null>(null)
  const [refusal, setRefusal] = useState<Refusal | null>(null)
  const [busy, setBusy] = useState(false)

  const [shown, setShown] = useState(open)
  if (shown !== open) {
    setShown(open)
    if (open) { setName(''); setDescription(''); setMerging(false); setFrom(''); setInto(''); setRefusal(null) }
  }

  const run = async (act: () => Promise<Refusal | null>, done?: () => void) => {
    setBusy(true)
    const said = await act()
    setBusy(false)
    setRefusal(said)
    if (!said) done?.()
  }

  const others = labels.filter((label) => label.name !== from)

  return (
    <>
      <Dialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
        <DialogContent className="sm:max-w-xl">
          <DialogTitle>Labels</DialogTitle>
          <DialogDescription>
            The project's own vocabulary. Cards pick from this list rather than inventing words, so a
            label says what it means.
          </DialogDescription>

          {labels.length === 0 ? (
            <p className="text-sm text-muted-foreground">No labels yet.</p>
          ) : (
            <ul className="-mx-2 flex max-h-64 flex-col overflow-y-auto">
              {labels.map((label) => (
                <li key={label.name} className="group/label flex items-baseline gap-3 px-2 py-1.5">
                  <span className="text-sm">{label.name}</span>
                  <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{label.description}</span>
                  <IconButton
                    label={`Delete the label ${label.name}`}
                    size="icon-xs"
                    side="left"
                    className="opacity-0 transition-opacity group-hover/label:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
                    onClick={() => { setRefusal(null); setRemoving(label.name) }}
                  >
                    <X />
                  </IconButton>
                </li>
              ))}
            </ul>
          )}

          {merging ? (
            <div className="flex flex-col gap-3">
              <Field>
                <FieldLabel htmlFor="merge-from">Merge this label</FieldLabel>
                <Select items={labels.map((label) => ({ value: label.name, label: label.name }))} value={from} onValueChange={(value) => { if (value) setFrom(value) }}>
                  <SelectTrigger id="merge-from" className="w-full">
                    <SelectValue placeholder="Pick a label" />
                  </SelectTrigger>
                  <SelectContent>
                    {labels.map((label) => <SelectItem key={label.name} value={label.name}>{label.name}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="merge-into">Into this one</FieldLabel>
                <Select items={others.map((label) => ({ value: label.name, label: label.name }))} value={into} onValueChange={(value) => { if (value) setInto(value) }}>
                  <SelectTrigger id="merge-into" className="w-full">
                    <SelectValue placeholder="Pick a label" />
                  </SelectTrigger>
                  <SelectContent>
                    {others.map((label) => <SelectItem key={label.name} value={label.name}>{label.name}</SelectItem>)}
                  </SelectContent>
                </Select>
                <FieldDescription>Every card carrying the first label gets the second, and the first goes.</FieldDescription>
              </Field>
              {refusal && <RefusalAlert refusal={refusal} />}
              <div className="flex justify-end gap-2">
                <Button variant="ghost" size="sm" disabled={busy} onClick={() => setMerging(false)}>Cancel</Button>
                <Button
                  size="sm"
                  disabled={busy || !from || !into || from === into}
                  onClick={() => void run(() => onMerge(from, into), () => { setMerging(false); setFrom(''); setInto('') })}
                >
                  {busy && <Spinner data-icon="inline-start" />}
                  Merge labels
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              <Field>
                <FieldLabel htmlFor="label-name">New label</FieldLabel>
                <Input
                  id="label-name"
                  value={name}
                  placeholder="Name"
                  autoComplete="off"
                  onChange={(event) => setName(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="label-description">What it means</FieldLabel>
                <Input
                  id="label-description"
                  value={description}
                  placeholder="When to put this on a card"
                  autoComplete="off"
                  onChange={(event) => setDescription(event.target.value)}
                />
                <FieldDescription>Required: a label nobody can define is a label nobody uses the same way.</FieldDescription>
              </Field>
              {refusal && <RefusalAlert refusal={refusal} />}
              <div className="flex items-center justify-between gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={busy || labels.length < 2}
                  title={labels.length < 2 ? 'Merging needs two labels' : undefined}
                  onClick={() => { setRefusal(null); setMerging(true) }}
                >
                  <Merge data-icon="inline-start" />
                  Merge two labels
                </Button>
                <Button
                  size="sm"
                  disabled={busy || !name.trim() || !description.trim()}
                  onClick={() => void run(() => onCreate(name.trim(), description.trim()), () => { setName(''); setDescription('') })}
                >
                  {busy && <Spinner data-icon="inline-start" />}
                  Add label
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={removing !== null}
        busy={busy}
        title={`Delete the label ${removing}?`}
        description="A label still on a card cannot be deleted; the server says how many carry it. Merging it into another keeps those cards labelled."
        confirm="Delete label"
        onOpenChange={(next) => { if (!next) setRemoving(null) }}
        onConfirm={() => { if (removing) void run(() => onDelete(removing), () => setRemoving(null)) }}
      />
    </>
  )
}
