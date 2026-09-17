import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { toast } from '@/components/ui/toast'
import { GuardedLink } from '@/components/wrappers/NavigationGuard'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import { readRefusal, type Refusal } from '@/lib/api'
import type { TemplateInfo } from '@/lib/templates'

/**
 * The vault templates on this machine, the shipped ones and the user's.
 * A row opens the template's file for editing; a new template starts from a
 * name and is written as a file the editor then opens.
 */
export function SettingsTemplates() {
  const [templates, setTemplates] = useState<TemplateInfo[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const navigate = useNavigate()

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/templates', { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error((await readRefusal(response)).message)
        return (await response.json()) as TemplateInfo[]
      })
      .then((list) => { setTemplates(list); setError(null) })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'Could not load the templates')
      })
    return () => controller.abort()
  }, [])

  const open = (name: string) => navigate(`/settings/templates/${encodeURIComponent(name)}`)

  return (
    <>
      <PageHeader
        title="Templates"
        description="The shapes a vault entry can follow: the fields it needs and the sections it keeps."
        actions={<Button variant="outline" size="sm" onClick={() => setCreating(true)}>New template</Button>}
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not load the templates</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!templates && !error && (
        <div className="mt-6 flex flex-col gap-2">
          {[0, 1, 2, 3].map((row) => <Skeleton key={row} className="h-10 w-full" />)}
        </div>
      )}

      {templates && (
        <Table className="mt-6">
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Rules</TableHead>
              <TableHead className="text-right">Sections</TableHead>
              <TableHead><span className="sr-only">Origin</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {templates.map((template) => (
              <TableRow key={template.name} className="cursor-pointer" onClick={() => open(template.name)}>
                <TableCell>
                  <GuardedLink
                    to={`/settings/templates/${encodeURIComponent(template.name)}`}
                    className="text-meta text-foreground hover:underline"
                    onClick={(event) => event.stopPropagation()}
                  >
                    {template.name}
                  </GuardedLink>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {template.enforce === 'reject' ? 'Strict' : 'Advisory'}
                </TableCell>
                <TableCell className="text-right text-muted-foreground tabular-nums">{template.sections.length}</TableCell>
                <TableCell className="text-right">
                  {template.builtin && <Badge variant="secondary">Built-in</Badge>}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <NewTemplateDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(name) => {
          setCreating(false)
          toast.add({ title: `Created ${name}`, type: 'success' })
          open(name)
        }}
      />
    </>
  )
}

/** Naming a new template. The file it creates is then opened for editing. */
function NewTemplateDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (name: string) => void
}) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [refusal, setRefusal] = useState<Refusal | null>(null)

  // Every opening starts blank. Reset while rendering, so the last name never flashes.
  const [shown, setShown] = useState(open)
  if (shown !== open) {
    setShown(open)
    if (open) { setName(''); setRefusal(null) }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const wanted = name.trim()
    if (!wanted || busy) return
    setBusy(true)
    setRefusal(null)
    try {
      const response = await fetch('/api/templates', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: wanted }),
      })
      if (!response.ok) {
        setRefusal(await readRefusal(response))
        return
      }
      const created = (await response.json()) as { name: string }
      onCreated(created.name)
    } catch (err) {
      setRefusal({ message: err instanceof Error ? err.message : 'Could not reach the daemon.', problems: [] })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
      <DialogContent className="gap-5 p-6 sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col gap-5">
          <DialogHeader>
            <DialogTitle>New template</DialogTitle>
            <DialogDescription>It starts as a small file you then edit.</DialogDescription>
          </DialogHeader>

          <Field data-invalid={refusal ? true : undefined}>
            <FieldLabel htmlFor="new-template-name">Name</FieldLabel>
            <Input
              id="new-template-name"
              required
              autoFocus
              autoComplete="off"
              spellCheck={false}
              placeholder="incident"
              aria-invalid={refusal ? true : undefined}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
            <FieldDescription>Lower case, as entries will name it.</FieldDescription>
          </Field>

          {refusal && <RefusalAlert refusal={refusal} />}

          <DialogFooter className="mx-0 mb-0 border-0 bg-transparent p-0">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim()}>
              {busy && <Spinner data-icon="inline-start" />}
              Create template
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
