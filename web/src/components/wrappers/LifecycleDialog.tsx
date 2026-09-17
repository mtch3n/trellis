import { useState } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import type { Refusal } from '@/lib/api'

/** Which act is being asked for; each one asks for different words. */
export type Lifecycle = 'promote' | 'demote' | 'verify' | 'pin'

const WORDS: Record<Lifecycle, {
  title: (slug: string) => string
  description: string
  reason?: { label: string; description: string; placeholder: string }
  confirm: string
  /** The name has to be retyped for the three acts that move an entry. */
  retype: boolean
}> = {
  promote: {
    title: (slug) => `Promote ${slug}?`,
    description:
      'The entry moves to the global vault, where every project can read it, and its file moves with it. Demoting undoes it.',
    reason: {
      label: 'Why it belongs beyond its project',
      description: 'Recorded with the promotion, for whoever reads it next.',
      placeholder: 'Every project hits this, and the answer does not change per project.',
    },
    confirm: 'Promote entry',
    retype: true,
  },
  demote: {
    title: (slug) => `Demote ${slug}?`,
    description: 'The entry returns to the project it came from, and its file moves back.',
    reason: {
      label: 'Why it does not belong globally',
      description: 'Recorded with the demotion.',
      placeholder: 'It is specific to one project after all.',
    },
    confirm: 'Demote entry',
    retype: true,
  },
  verify: {
    title: (slug) => `Verify ${slug}?`,
    description:
      'This records that the entry still holds, which resets its verify date. Nothing in the entry changes.',
    confirm: 'Verify entry',
    retype: true,
  },
  pin: {
    title: (slug) => `Pin ${slug}?`,
    description:
      'A pinned entry is read into every session that starts in this project. The recap is what they read before the body.',
    reason: {
      label: 'Recap',
      description: "Left empty, the entry's summary stands in. Trellis never writes one for you.",
      placeholder: 'The one thing a session needs to know from this entry.',
    },
    confirm: 'Pin entry',
    retype: false,
  },
}

/**
 * The acts that move an entry between a project and the global vault, and
 * pinning, which is the other thing a person decides about an entry. The CLI
 * refuses promote, demote and verify to an agent and points at a person, so
 * this is where they happen; it asks for the same two things: the entry's
 * name, retyped, and why. A refusal is shown inside the dialog, because the
 * form is where it has to be corrected.
 */
export function LifecycleDialog({ act, slug, onOpenChange, onConfirm }: {
  /** Which act, or null when the dialog is closed. */
  act: Lifecycle | null
  slug: string
  onOpenChange: (open: boolean) => void
  /** Resolves null when it landed, or the server's refusal. */
  onConfirm: (act: Lifecycle, values: { confirm: string; reason: string }) => Promise<Refusal | null>
}) {
  const [typed, setTyped] = useState('')
  const [reason, setReason] = useState('')
  const [refusal, setRefusal] = useState<Refusal | null>(null)
  const [busy, setBusy] = useState(false)

  // Each opening starts clean, adjusted while rendering so the first frame
  // never shows the last answer.
  const [shown, setShown] = useState(act)
  if (shown !== act) {
    setShown(act)
    setTyped('')
    setReason('')
    setRefusal(null)
  }

  if (!act) return null
  const words = WORDS[act]
  // The name is typed as the entry is called, with or without its folders.
  const leaf = slug.split('/').pop() ?? slug
  const named = typed.trim() === slug || typed.trim() === leaf
  const ready = (!words.retype || named) && (!words.reason || act === 'pin' || reason.trim() !== '')

  const submit = async () => {
    setBusy(true)
    const said = await onConfirm(act, { confirm: typed.trim() || slug, reason: reason.trim() })
    setBusy(false)
    setRefusal(said)
    if (!said) onOpenChange(false)
  }

  return (
    <Dialog open onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
      <DialogContent className="sm:max-w-lg">
        <DialogTitle>{words.title(leaf)}</DialogTitle>
        <DialogDescription className="text-pretty">{words.description}</DialogDescription>

        {words.reason && (
          <Field>
            <FieldLabel htmlFor="lifecycle-reason">{words.reason.label}</FieldLabel>
            <Textarea
              id="lifecycle-reason"
              rows={3}
              value={reason}
              placeholder={words.reason.placeholder}
              autoFocus
              onChange={(event) => setReason(event.target.value)}
            />
            <FieldDescription>{words.reason.description}</FieldDescription>
          </Field>
        )}

        {words.retype && (
          <Field>
            <FieldLabel htmlFor="lifecycle-confirm">Type <span className="text-meta">{leaf}</span> to confirm</FieldLabel>
            <Input
              id="lifecycle-confirm"
              value={typed}
              autoFocus={!words.reason}
              autoComplete="off"
              onChange={(event) => setTyped(event.target.value)}
            />
          </Field>
        )}

        {refusal && <RefusalAlert refusal={refusal} />}

        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={busy} onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button size="sm" disabled={busy || !ready} onClick={() => void submit()}>
            {busy && <Spinner data-icon="inline-start" />}
            {words.confirm}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
