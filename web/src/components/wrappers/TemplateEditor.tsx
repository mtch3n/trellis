import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Ellipsis } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { toast } from '@/components/ui/toast'
import { ConfirmDialog } from '@/components/wrappers/ConfirmDialog'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import { readError, readRefusal, type Refusal } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/navigation-guard'

/** A template file, as `GET /api/templates/{name}` returns it. */
interface TemplateFile {
  name: string
  builtin: boolean
  enforce: 'reject' | 'warn'
  /** The whole file: frontmatter rules, then the skeleton. */
  raw: string
}

/**
 * One template's file, edited as text: its rules in the frontmatter, then the
 * skeleton a new entry starts from. Nothing is written until Save, and a file
 * that does not parse is refused whole. Ctrl or Cmd with S, or with Enter,
 * saves from the text.
 *
 * Reinstalling a shipped template and deleting one are kept in a menu beside
 * Save, and both ask first.
 */
export function TemplateEditor() {
  const { name = '' } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const [template, setTemplate] = useState<TemplateFile | null>(null)
  const [raw, setRaw] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [refusal, setRefusal] = useState<Refusal | null>(null)
  const [saving, setSaving] = useState(false)
  const [confirming, setConfirming] = useState<'reinstall' | 'delete' | null>(null)
  const [busy, setBusy] = useState(false)
  const path = `/api/templates/${encodeURIComponent(name)}`

  const dirty = template !== null && raw !== template.raw
  useUnsavedChanges(dirty)

  const take = (file: TemplateFile) => {
    setTemplate(file)
    setRaw(file.raw)
    setRefusal(null)
  }

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch(path, { signal })
      if (!response.ok) throw new Error((await readRefusal(response)).message)
      const file = (await response.json()) as TemplateFile
      setTemplate(file)
      setRaw(file.raw)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not load the template')
    }
  }, [path])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const save = async (event?: FormEvent) => {
    event?.preventDefault()
    if (!dirty || saving) return
    setSaving(true)
    setRefusal(null)
    try {
      const response = await fetch(path, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ raw }),
      })
      if (!response.ok) {
        setRefusal(await readRefusal(response))
        return
      }
      take((await response.json()) as TemplateFile)
      toast.add({ title: `Saved ${name}`, type: 'success' })
    } catch (err) {
      setRefusal({ message: err instanceof Error ? err.message : 'Could not reach the daemon.', problems: [] })
    } finally {
      setSaving(false)
    }
  }

  const act = async () => {
    const reinstalling = confirming === 'reinstall'
    setBusy(true)
    try {
      // Every write to the daemon is JSON, even one with nothing to say.
      const response = await fetch(reinstalling ? `${path}/reinstall` : path, {
        method: reinstalling ? 'POST' : 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      })
      if (!response.ok) throw new Error(await readError(response))
      setConfirming(null)
      if (reinstalling) {
        take((await response.json()) as TemplateFile)
        toast.add({ title: `Reinstalled ${name}`, type: 'success' })
      } else {
        toast.add({ title: `Deleted ${name}`, type: 'success' })
        navigate('/settings/templates', { replace: true })
      }
    } catch (err) {
      toast.add({
        title: reinstalling ? `Could not reinstall ${name}` : `Could not delete ${name}`,
        description: err instanceof Error ? err.message : undefined,
        type: 'error',
      })
    } finally {
      setBusy(false)
    }
  }

  const rules = template ? (template.enforce === 'reject' ? 'strict' : 'advisory') : ''

  return (
    <>
      <PageHeader
        title={name}
        description={
          template
            ? `${template.builtin ? 'Shipped with Trellis' : 'Your template'}, ${rules}. The rules come first, then the skeleton a new entry starts from.`
            : 'The rules come first, then the skeleton a new entry starts from.'
        }
        actions={
          <>
            <TemplateMenu
              disabled={!template}
              builtin={Boolean(template?.builtin)}
              onReinstall={() => setConfirming('reinstall')}
              onDelete={() => setConfirming('delete')}
            />
            <Button type="submit" size="sm" form="template-form" disabled={!dirty || saving}>
              {saving && <Spinner data-icon="inline-start" />}
              Save
            </Button>
          </>
        }
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not load {name}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!template && !error && <Skeleton className="mt-6 h-96 w-full" />}

      {template && (
        <form id="template-form" onSubmit={save} className="mt-6 flex flex-col gap-4">
          {refusal && <RefusalAlert refusal={refusal} />}
          <Textarea
            aria-label={`The ${name} template file`}
            spellCheck={false}
            className="min-h-[60dvh] resize-y text-meta"
            value={raw}
            onChange={(event) => setRaw(event.target.value)}
            onKeyDown={(event) => {
              if ((event.metaKey || event.ctrlKey) && (event.key === 's' || event.key === 'Enter')) {
                event.preventDefault()
                void save()
              }
            }}
          />
        </form>
      )}

      <ConfirmDialog
        open={confirming !== null}
        busy={busy}
        title={confirming === 'reinstall' ? `Reinstall ${name}?` : `Delete ${name}?`}
        description={
          confirming === 'reinstall'
            ? 'The file goes back to the version Trellis ships. Your changes to it are lost.'
            : 'The file is removed. Entries that name this template keep the name, and its rules are no longer checked.'
        }
        confirm={confirming === 'reinstall' ? 'Reinstall template' : 'Delete template'}
        onOpenChange={(open) => { if (!open) setConfirming(null) }}
        onConfirm={() => void act()}
      />
    </>
  )
}

/** A template's secondary actions: reinstalling a shipped one, and deleting. */
function TemplateMenu({
  disabled,
  builtin,
  onReinstall,
  onDelete,
}: {
  disabled: boolean
  builtin: boolean
  onReinstall: () => void
  onDelete: () => void
}) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger
          render={
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon-sm" aria-label="More actions" disabled={disabled} />}
            />
          }
        >
          <Ellipsis />
        </TooltipTrigger>
        <TooltipContent side="bottom">More actions</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuGroup>
          {builtin && <DropdownMenuItem onClick={onReinstall}>Reinstall</DropdownMenuItem>}
          <DropdownMenuItem variant="destructive" onClick={onDelete}>Delete</DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
