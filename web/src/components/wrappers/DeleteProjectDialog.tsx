import { useEffect, useState, type FormEvent } from 'react'
import { Trash2 } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { readError } from '@/lib/api'

/**
 * Deleting a project, with the key retyped. The server asks for the same
 * confirmation, so this dialog is the explanation, not the guard: it says what
 * goes, what stays, and why the project can come back.
 *
 * A refusal from the server (a lease held right now, entries in the global
 * vault) is shown inside the dialog, where the reader is looking.
 */
export function DeleteProjectDialog({
  projectKey,
  cards,
  onOpenChange,
  onDeleted,
}: {
  /** The project to delete; null keeps the dialog closed. */
  projectKey: string | null
  cards: number
  onOpenChange: (open: boolean) => void
  onDeleted: (key: string) => void
}) {
  const [typed, setTyped] = useState('')
  const [entries, setEntries] = useState<number | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [refusal, setRefusal] = useState<string | null>(null)

  // Another project starts from a blank confirmation. Reset while rendering,
  // so the dialog never shows the previous project's typing.
  const [shownKey, setShownKey] = useState(projectKey)
  if (shownKey !== projectKey) {
    setShownKey(projectKey)
    setTyped('')
    setRefusal(null)
    setEntries(null)
  }

  useEffect(() => {
    if (!projectKey) return
    const controller = new AbortController()
    fetch(`/api/p/${projectKey}/knowledge`, { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : []))
      .then((list: unknown[]) => setEntries(list?.length ?? 0))
      .catch(() => { /* the count is a courtesy */ })
    return () => controller.abort()
  }, [projectKey])

  const confirmed = projectKey !== null && typed.trim().toUpperCase() === projectKey

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!projectKey || !confirmed) return
    setDeleting(true)
    setRefusal(null)
    try {
      const response = await fetch(`/api/p/${projectKey}`, {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: typed.trim() }),
      })
      if (!response.ok) {
        setRefusal(await readError(response))
        return
      }
      onDeleted(projectKey)
    } catch (err) {
      setRefusal(err instanceof Error ? err.message : 'Could not reach the daemon.')
    } finally {
      setDeleting(false)
    }
  }

  const what = [
    `${cards} ${cards === 1 ? 'card' : 'cards'}`,
    entries === null ? 'its vault entries' : `${entries} vault ${entries === 1 ? 'entry' : 'entries'}`,
  ].join(' and ')

  return (
    <Dialog open={projectKey !== null} onOpenChange={(open) => { if (!deleting) onOpenChange(open) }}>
      <DialogContent className="gap-5 p-6 sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col gap-5">
          <DialogHeader>
            <DialogTitle>Delete {projectKey}?</DialogTitle>
            <DialogDescription className="text-pretty">
              This removes its boards, {what}, and their files from this machine. The event log keeps a
              record. Running trellis in the repository again starts a new, empty project.
            </DialogDescription>
          </DialogHeader>

          <Field>
            <FieldLabel htmlFor="confirm-project-key">
              Type <span className="font-semibold">{projectKey}</span> to confirm
            </FieldLabel>
            <Input
              id="confirm-project-key"
              autoComplete="off"
              spellCheck={false}
              autoFocus
              value={typed}
              onChange={(event) => setTyped(event.target.value)}
            />
          </Field>

          {refusal && (
            <Alert variant="destructive">
              <AlertDescription className="whitespace-pre-line">{refusal}</AlertDescription>
            </Alert>
          )}

          <DialogFooter className="mx-0 mb-0 border-0 bg-transparent p-0">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={deleting}>
              Cancel
            </Button>
            <Button type="submit" variant="destructive" disabled={!confirmed || deleting}>
              {deleting ? <Spinner data-icon="inline-start" /> : <Trash2 data-icon="inline-start" />}
              Delete project
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
