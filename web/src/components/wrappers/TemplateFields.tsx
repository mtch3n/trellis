import { useState } from 'react'
import { Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { IconButton } from '@/components/wrappers/IconButton'
import { sentence, templateLabel } from '@/lib/format'
import { templateAsks, type FieldValues, type TemplateInfo } from '@/lib/templates'

/** The value a select holds while nothing is chosen. */
const UNSET = ''

/**
 * What a template needs from the writer beyond the title: its sources, a
 * value for each field with fixed choices, and any other field it requires.
 * Summary is left to the caller, which knows whether it already has one.
 * A field the entry already has is not asked for, and a template that needs
 * nothing renders nothing.
 */
export function TemplateFields({
  template,
  projectKey,
  sources,
  values,
  showErrors,
  askSources = true,
  has,
  onSourcesChange,
  onValuesChange,
}: {
  template: TemplateInfo | undefined
  projectKey: string
  sources: string[]
  values: Record<string, string>
  /** Mark required choices still unchosen, once a submit was tried. */
  showErrors: boolean
  /** False when the entry already has sources, so they are not asked for again. */
  askSources?: boolean
  /** The fields the entry already has. */
  has?: FieldValues
  onSourcesChange: (sources: string[]) => void
  onValuesChange: (values: Record<string, string>) => void
}) {
  const asks = templateAsks(template, has)

  return (
    <>
      {asks.sources && askSources && (
        <SourcesField projectKey={projectKey} sources={sources} onChange={onSourcesChange} />
      )}

      {asks.choices.map((choice) => {
        const invalid = showErrors && choice.required && !values[choice.field]
        const id = `template-field-${choice.field}`
        return (
          <Field key={choice.field} data-invalid={invalid || undefined}>
            <FieldLabel htmlFor={id}>{sentence(choice.field)}</FieldLabel>
            <Select
              items={[
                { value: UNSET, label: choice.required ? 'Choose one' : 'None' },
                ...choice.options.map((option) => ({ value: option, label: sentence(option) })),
              ]}
              value={values[choice.field] ?? UNSET}
              onValueChange={(value) => onValuesChange({ ...values, [choice.field]: value ?? UNSET })}
            >
              <SelectTrigger id={id} className="w-full" aria-invalid={invalid || undefined}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {!choice.required && <SelectItem value={UNSET}>None</SelectItem>}
                {choice.options.map((option) => (
                  <SelectItem key={option} value={option}>{sentence(option)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            {invalid && (
              <FieldError>
                {templateLabel(template?.name)} needs a {sentence(choice.field).toLowerCase()}.
              </FieldError>
            )}
          </Field>
        )
      })}

      {asks.fields.map((field) => (
        <Field key={field}>
          <FieldLabel htmlFor={`template-field-${field}`}>{sentence(field)}</FieldLabel>
          <Input
            id={`template-field-${field}`}
            required
            autoComplete="off"
            value={values[field] ?? ''}
            onChange={(event) => onValuesChange({ ...values, [field]: event.target.value })}
          />
        </Field>
      ))}
    </>
  )
}

/**
 * The sources a template asks for, one per line. The first is required; more
 * can be added and removed.
 */
function SourcesField({
  projectKey,
  sources,
  onChange,
}: {
  projectKey: string
  sources: string[]
  onChange: (sources: string[]) => void
}) {
  const [added, setAdded] = useState(0)
  const change = (index: number, value: string) => onChange(sources.map((source, at) => (at === index ? value : source)))

  return (
    <FieldSet className="gap-3">
      <FieldLegend variant="label">Sources</FieldLegend>
      <FieldDescription className="text-pretty">
        What the entry rests on. A card such as {projectKey}-12, or an entry written as [[its-slug]], has to
        exist. A URL, a file pointer or a sentence is kept as written.
      </FieldDescription>
      <div className="flex flex-col gap-2">
        {sources.map((source, index) => (
          <div key={index} className="flex items-center gap-1.5">
            <Input
              aria-label={`Source ${index + 1}`}
              required={index === 0}
              autoComplete="off"
              spellCheck={false}
              // A row added from the button takes the cursor.
              autoFocus={index > 0 && index === added}
              placeholder={index === 0 ? `${projectKey}-12, [[an-entry]] or https://…` : undefined}
              value={source}
              onChange={(event) => change(index, event.target.value)}
            />
            {sources.length > 1 && (
              <IconButton
                label={`Remove source ${index + 1}`}
                side="left"
                onClick={() => onChange(sources.filter((_, at) => at !== index))}
              >
                <X />
              </IconButton>
            )}
          </div>
        ))}
      </div>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="self-start text-muted-foreground"
        onClick={() => { setAdded(sources.length); onChange([...sources, '']) }}
      >
        <Plus data-icon="inline-start" />
        Add source
      </Button>
    </FieldSet>
  )
}
