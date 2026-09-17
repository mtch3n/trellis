import { useState } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'
import { Spinner } from '@/components/ui/spinner'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'

const EXAMPLE = `[
  { "id": "a", "title": "Write the migration", "priority": "high" },
  { "id": "b", "title": "Run it on staging", "blocked_by": ["a"] }
]`

/**
 * A plan as cards, the way `card import` takes it: a JSON array, where a
 * card's `id` is a handle the others in the same import can wait on. The board
 * gains every card or none, so the text is checked here for being an array of
 * objects with titles before it is sent, and the server's own refusal is shown
 * inside the dialog rather than as a toast that outlives the form.
 */
export function ImportCardsDialog({ open, onOpenChange, onImport }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Resolves true when the cards landed. */
  onImport: (cards: unknown[]) => Promise<boolean>
}) {
  const [text, setText] = useState('')
  const [problem, setProblem] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const [shown, setShown] = useState(open)
  if (shown !== open) {
    setShown(open)
    if (open) { setText(''); setProblem(null) }
  }

  const submit = async () => {
    let parsed: unknown
    try {
      parsed = JSON.parse(text)
    } catch (err) {
      setProblem(`That is not JSON: ${err instanceof Error ? err.message : 'it could not be parsed'}.`)
      return
    }
    if (!Array.isArray(parsed)) {
      setProblem('The import is an array of cards, even when there is only one.')
      return
    }
    const untitled = parsed.filter(
      (card) => typeof card !== 'object' || card === null || typeof (card as { title?: unknown }).title !== 'string',
    )
    if (untitled.length > 0) {
      setProblem(`Every card needs a title; ${untitled.length} of them has none.`)
      return
    }
    setProblem(null)
    setBusy(true)
    const landed = await onImport(parsed)
    setBusy(false)
    if (landed) onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
      <DialogContent className="sm:max-w-2xl">
        <DialogTitle>Import cards</DialogTitle>
        <DialogDescription>
          A JSON array, as <span className="text-meta">trellis card import</span> takes it. A card's
          id is a handle only inside this import, so a later card can say it is blocked by an
          earlier one. They land together or not at all.
        </DialogDescription>

        <Field>
          <FieldLabel htmlFor="import-cards">Cards</FieldLabel>
          <Textarea
            id="import-cards"
            value={text}
            placeholder={EXAMPLE}
            rows={12}
            className="text-meta"
            autoFocus
            onChange={(event) => setText(event.target.value)}
          />
        </Field>

        {problem && <RefusalAlert refusal={{ message: problem, problems: [] }} />}

        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={busy} onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button size="sm" disabled={busy || text.trim() === ''} onClick={() => void submit()}>
            {busy && <Spinner data-icon="inline-start" />}
            Import
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
