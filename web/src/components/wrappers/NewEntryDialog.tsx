import { useState, type FormEvent } from 'react'
import { FolderPlus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { IconButton } from '@/components/wrappers/IconButton'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import { TemplateFields } from '@/components/wrappers/TemplateFields'
import { TemplateSelect } from '@/components/wrappers/TemplateSelect'
import { readRefusal, type Refusal } from '@/lib/api'
import { normalizeSource } from '@/lib/sources'
import {
  NO_TEMPLATE,
  describeTemplate,
  templateAsks,
  templateSet,
  unchosenFields,
  type TemplateInfo,
} from '@/lib/templates'
import { cn } from '@/lib/utils'
import type { KnowledgeEntry } from '@/pages/KnowledgePage'

/** What the server hands back for a new entry: the entry, and what its template warned about. */
export type CreatedEntry = KnowledgeEntry & { warnings?: string[] }

/** The project's top level, as a folder choice. */
const TOP = ''
/** The folder picker's way out of its list. No directory starts with a slash. */
const NEW_FOLDER = '/new'

/**
 * Starting an entry. It asks for the title, the folder, the template, and
 * only what that template cannot do without, so an entry that follows none is
 * a title and a button. The body is not written here: the new entry opens for
 * editing, with the template's sections already in place, where it will be
 * read.
 *
 * The template's rules are shown before they are met, and anything the server
 * still refuses, such as a source that does not resolve, is listed in the
 * dialog, next to what can fix it.
 */
export function NewEntryDialog({
  open,
  projectKey,
  board,
  templates,
  folders,
  dir: startDir,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  projectKey: string
  /** The board the entry is associated with. Null while boards load. */
  board: string | null
  templates: TemplateInfo[]
  /** The project's folders, as directories: `ops`, `ops/db`. */
  folders: string[]
  /** The folder the dialog was opened from; "" is the project's top level. */
  dir: string
  onOpenChange: (open: boolean) => void
  onCreated: (entry: CreatedEntry) => void
}) {
  const [title, setTitle] = useState('')
  const [dir, setDir] = useState(startDir)
  const [templateName, setTemplateName] = useState(NO_TEMPLATE)
  const [summary, setSummary] = useState('')
  const [sources, setSources] = useState([''])
  const [values, setValues] = useState<Record<string, string>>({})
  const [tried, setTried] = useState(false)
  const [creating, setCreating] = useState(false)
  const [refusal, setRefusal] = useState<Refusal | null>(null)

  // Every opening starts from a blank form. Reset while rendering, so the
  // previous entry's words never flash on screen.
  const [shownOpen, setShownOpen] = useState(open)
  if (shownOpen !== open) {
    setShownOpen(open)
    if (open) {
      setTitle('')
      setDir(startDir)
      setTemplateName(NO_TEMPLATE)
      setSummary('')
      setSources([''])
      setValues({})
      setTried(false)
      setRefusal(null)
    }
  }

  const template = templates.find((item) => item.name === templateName)
  const asks = templateAsks(template)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setTried(true)
    if (!board || !title.trim() || unchosenFields(template, values).length > 0) return
    setCreating(true)
    setRefusal(null)
    try {
      const response = await fetch(`/api/p/${projectKey}/b/${board}/knowledge`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: title.trim(),
          dir: dir.trim().replace(/^\/+|\/+$/g, '') || undefined,
          template: templateName,
          summary: asks.summary ? summary.trim() : undefined,
          sources: asks.sources
            ? sources.map((source) => normalizeSource(source, projectKey)).filter((source) => source !== '')
            : undefined,
          set: templateSet(template, values),
        }),
      })
      if (!response.ok) {
        setRefusal(await readRefusal(response))
        return
      }
      onCreated((await response.json()) as CreatedEntry)
    } catch (err) {
      setRefusal({ message: err instanceof Error ? err.message : 'Could not reach the daemon.', problems: [] })
    } finally {
      setCreating(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!creating) onOpenChange(next) }}>
      <DialogContent className="max-h-[88dvh] gap-5 overflow-y-auto p-6 sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-6">
          <DialogHeader>
            <DialogTitle>New entry</DialogTitle>
            <DialogDescription className="text-pretty">
              A markdown file in {projectKey}. It opens for writing once it exists.
            </DialogDescription>
          </DialogHeader>

          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="new-entry-title">Title</FieldLabel>
              <Input
                id="new-entry-title"
                required
                autoFocus
                autoComplete="off"
                placeholder="What the entry is about"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
              />
            </Field>

            <FolderField projectKey={projectKey} folders={folders} dir={dir} onChange={setDir} />

            <Field>
              <FieldLabel htmlFor="new-entry-template">Template</FieldLabel>
              <TemplateSelect id="new-entry-template" templates={templates} value={templateName} onChange={setTemplateName} />
              <FieldDescription className="text-pretty">{describeTemplate(template)}</FieldDescription>
            </Field>

            {asks.summary && (
              <Field>
                <FieldLabel htmlFor="new-entry-summary">Summary</FieldLabel>
                <Input
                  id="new-entry-summary"
                  required
                  autoComplete="off"
                  placeholder="One sentence a future session can act on"
                  value={summary}
                  onChange={(event) => setSummary(event.target.value)}
                />
              </Field>
            )}

            <TemplateFields
              template={template}
              projectKey={projectKey}
              sources={sources}
              values={values}
              showErrors={tried}
              onSourcesChange={setSources}
              onValuesChange={setValues}
            />
          </FieldGroup>

          {refusal && <RefusalAlert refusal={refusal} />}

          <DialogFooter className="mx-0 mb-0 border-0 bg-transparent p-0">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={creating}>
              Cancel
            </Button>
            <Button type="submit" disabled={creating || !board}>
              {creating && <Spinner data-icon="inline-start" />}
              Create entry
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Where the file goes: the project's top level, one of its folders, or a new
 * one typed in. A typed name close to an existing folder is refused by the
 * server, which keeps the tree from growing near-duplicates.
 */
function FolderField({
  projectKey,
  folders,
  dir,
  onChange,
}: {
  projectKey: string
  folders: string[]
  dir: string
  onChange: (dir: string) => void
}) {
  // A folder that is not in the list yet is being typed.
  const [typing, setTyping] = useState(dir !== TOP && !folders.includes(dir))
  const options = [TOP, ...folders]
  const name = (folder: string) => folder || `${projectKey}, top level`

  return (
    <Field>
      <FieldLabel htmlFor="new-entry-folder">Folder</FieldLabel>
      {typing ? (
        <div className="flex items-center gap-1.5">
          <Input
            id="new-entry-folder"
            autoFocus
            autoComplete="off"
            spellCheck={false}
            placeholder="ops/runbooks"
            value={dir}
            onChange={(event) => onChange(event.target.value)}
          />
          <IconButton label="Pick an existing folder" side="left" onClick={() => { setTyping(false); onChange(TOP) }}>
            <X />
          </IconButton>
        </div>
      ) : (
        <Select
          items={[
            ...options.map((folder) => ({ value: folder, label: name(folder) })),
            { value: NEW_FOLDER, label: 'New folder…' },
          ]}
          value={dir}
          onValueChange={(value) => {
            if (value === NEW_FOLDER) { setTyping(true); onChange(TOP) }
            else onChange(value ?? TOP)
          }}
        >
          <SelectTrigger id="new-entry-folder" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {options.map((folder) => (
              <SelectItem key={folder || 'top'} value={folder}>
                <span className={cn(folder === TOP && 'text-muted-foreground')}>{name(folder)}</span>
              </SelectItem>
            ))}
            <SelectSeparator />
            <SelectItem value={NEW_FOLDER}>
              <FolderPlus className="text-muted-foreground" />
              New folder…
            </SelectItem>
          </SelectContent>
        </Select>
      )}
    </Field>
  )
}
