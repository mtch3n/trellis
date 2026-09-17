import { useState, type FormEvent } from 'react'
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
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { RefusalAlert } from '@/components/wrappers/RefusalAlert'
import { TemplateFields } from '@/components/wrappers/TemplateFields'
import type { Refusal } from '@/lib/api'
import { sentence, wordList } from '@/lib/format'
import { normalizeSource } from '@/lib/sources'
import {
  describeTemplate,
  missingSections,
  templateAsks,
  templateSet,
  unchosenFields,
  withSections,
  type SwitchedEntry,
  type TemplateInfo,
} from '@/lib/templates'

/** One save that moves an entry onto a template, with what the template needs. */
export interface TemplateSwitch {
  template: string
  summary?: string
  sources?: string[]
  set?: Record<string, string>
  body?: string
}

/**
 * Moving an entry onto a strict template, which refuses an entry that falls
 * short. The dialog asks for what the entry lacks and says what it will add:
 * any section the body is missing goes at its end, empty, for the writer to
 * fill in. Everything is sent as one save, so the entry is checked once, as
 * it will be.
 *
 * Adding sections changes the body, so it waits while the body is being
 * edited rather than writing under the editor.
 */
export function TemplateSwitchDialog({
  template,
  entry,
  projectKey,
  editing,
  onOpenChange,
  onSwitch,
}: {
  /** The template being switched to; null keeps the dialog closed. */
  template: TemplateInfo | null
  entry: SwitchedEntry
  projectKey: string
  /** The body is open in the editor. */
  editing: boolean
  onOpenChange: (open: boolean) => void
  /** Resolves null when the switch was saved, or with why it was refused. */
  onSwitch: (change: TemplateSwitch) => Promise<Refusal | null>
}) {
  const [summary, setSummary] = useState('')
  const [sources, setSources] = useState([''])
  const [values, setValues] = useState<Record<string, string>>({})
  const [tried, setTried] = useState(false)
  const [saving, setSaving] = useState(false)
  const [refusal, setRefusal] = useState<Refusal | null>(null)

  // Every opening starts from a blank form. The last template is kept while
  // the dialog closes, so its words do not vanish mid-animation.
  const [open, setOpen] = useState(false)
  const [last, setLast] = useState(template)
  if ((template !== null) !== open) {
    setOpen(template !== null)
    if (template) {
      setLast(template)
      setSummary('')
      setSources([''])
      setValues({})
      setTried(false)
      setRefusal(null)
    }
  }
  const target = template ?? last ?? undefined

  const asks = templateAsks(target, entry.fields)
  const hasSources = Boolean(entry.sources?.length)
  const askSummary = asks.summary && !entry.summary?.trim()
  const missing = target ? missingSections(entry.body ?? '', target.sections) : []
  // Whether there is anything to fill in, or only sections to add.
  const asking = askSummary || (asks.sources && !hasSources) || asks.choices.length > 0 || asks.fields.length > 0
  const blocked = editing && missing.length > 0
  const lacks = `The body lacks the ${missing.length === 1 ? 'section' : 'sections'} ${wordList(missing)}`
  const name = sentence(target?.name ?? '')

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setTried(true)
    if (!template || blocked || unchosenFields(template, values, entry.fields).length > 0) return
    setSaving(true)
    setRefusal(null)
    const change: TemplateSwitch = {
      template: template.name,
      summary: askSummary ? summary.trim() : undefined,
      sources: asks.sources && !hasSources
        ? sources.map((source) => normalizeSource(source, projectKey)).filter((source) => source !== '')
        : undefined,
      set: templateSet(template, values, entry.fields),
      body: missing.length ? withSections(entry.body ?? '', missing) : undefined,
    }
    const refused = await onSwitch(change)
    setSaving(false)
    setRefusal(refused)
  }

  return (
    <Dialog open={template !== null} onOpenChange={(next) => { if (!saving) onOpenChange(next) }}>
      <DialogContent className="max-h-[88dvh] gap-5 overflow-y-auto p-6 sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-6">
          <DialogHeader>
            <DialogTitle>Switch to {name}</DialogTitle>
            <DialogDescription className="text-pretty">{describeTemplate(target)}</DialogDescription>
          </DialogHeader>

          {asking && (
            <FieldGroup>
              {askSummary && (
                <Field>
                  <FieldLabel htmlFor="switch-summary">Summary</FieldLabel>
                  <Input
                    id="switch-summary"
                    required
                    autoComplete="off"
                    placeholder="One sentence a future session can act on"
                    value={summary}
                    onChange={(event) => setSummary(event.target.value)}
                  />
                </Field>
              )}
              <TemplateFields
                template={target}
                projectKey={projectKey}
                sources={sources}
                values={values}
                showErrors={tried}
                askSources={!hasSources}
                has={entry.fields}
                onSourcesChange={setSources}
                onValuesChange={setValues}
              />
            </FieldGroup>
          )}

          {missing.length > 0 && (
            <Alert>
              <AlertDescription className="text-pretty">
                {blocked
                  ? `${lacks}, and switching adds ${missing.length === 1 ? 'it' : 'them'}. Save or cancel your edit first, so nothing is written under it.`
                  : `${lacks}. Switching adds ${missing.length === 1 ? 'it' : 'them'} at the end, empty, for you to fill in.`}
              </AlertDescription>
            </Alert>
          )}

          {refusal && <RefusalAlert refusal={refusal} />}

          <DialogFooter className="mx-0 mb-0 border-0 bg-transparent p-0">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving || blocked}>
              {saving && <Spinner data-icon="inline-start" />}
              Switch to {name}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
