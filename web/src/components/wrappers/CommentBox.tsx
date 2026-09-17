import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Kbd, KbdGroup } from '@/components/ui/kbd'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'

/**
 * Saying something on a card. It sits at the top of the card's timeline,
 * where the newest item lands, and a comment appears there as soon as it is
 * posted. Markdown is rendered in the timeline; Ctrl or Cmd with Enter posts.
 * The box keeps what was typed when posting fails.
 */
export function CommentBox({ onSubmit }: { onSubmit: (body: string) => Promise<boolean> }) {
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const empty = body.trim() === ''

  const submit = async (event?: FormEvent) => {
    event?.preventDefault()
    if (empty || busy) return
    setBusy(true)
    const posted = await onSubmit(body.trim())
    setBusy(false)
    if (posted) setBody('')
  }

  return (
    <form className="flex flex-col gap-2" onSubmit={submit}>
      <Textarea
        aria-label="Comment"
        placeholder="Add a comment. Markdown works."
        className="min-h-20 resize-y"
        value={body}
        onChange={(event) => setBody(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
            event.preventDefault()
            void submit()
          }
        }}
      />
      <div className="flex items-center justify-end gap-2">
        {!empty && (
          <>
            <KbdGroup className="mr-1 hidden text-xs text-muted-foreground sm:inline-flex">
              <Kbd>Ctrl</Kbd>
              <Kbd>Enter</Kbd>
            </KbdGroup>
            <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => setBody('')}>
              Discard
            </Button>
          </>
        )}
        <Button type="submit" size="sm" disabled={empty || busy}>
          {busy && <Spinner data-icon="inline-start" />}
          Comment
        </Button>
      </div>
    </form>
  )
}
