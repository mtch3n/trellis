import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { templateLabel } from '@/lib/format'
import { NO_TEMPLATE, type TemplateInfo } from '@/lib/templates'
import { cn } from '@/lib/utils'

/**
 * Which template an entry follows, picked from the ones Trellis knows. No
 * template comes first, because most entries follow none. A strict template
 * says so in the list, since choosing it means the entry can be refused.
 *
 * A template the entry names but Trellis no longer has is still shown, so the
 * picker never claims the entry follows nothing.
 */
export function TemplateSelect({
  id,
  templates,
  value,
  disabled,
  className,
  onChange,
}: {
  id?: string
  templates: TemplateInfo[]
  value: string
  disabled?: boolean
  className?: string
  onChange: (template: string) => void
}) {
  const names = templates.map((template) => template.name)
  const known = value === NO_TEMPLATE || names.includes(value)
  const options = [NO_TEMPLATE, ...names, ...(known ? [] : [value])]

  return (
    <Select
      items={options.map((name) => ({ value: name, label: templateLabel(name) }))}
      value={value}
      onValueChange={(next) => { if (next !== null && next !== value) onChange(next) }}
    >
      <SelectTrigger id={id} aria-label="Template" className={cn('w-full', className)} disabled={disabled}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((name) => {
          const template = templates.find((item) => item.name === name)
          return (
            <SelectItem key={name || 'none'} value={name}>
              <span className={cn(name === NO_TEMPLATE && 'text-muted-foreground')}>{templateLabel(name)}</span>
              {template?.enforce === 'reject' && <span className="ml-auto self-center pl-4 text-xs text-muted-foreground">Strict</span>}
              {!known && name === value && <span className="ml-auto self-center pl-4 text-xs text-muted-foreground">Missing</span>}
            </SelectItem>
          )
        })}
      </SelectContent>
    </Select>
  )
}
