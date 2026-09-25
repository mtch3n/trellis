import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'

/** Every effort, lowest first. Empty leaves it to the model. */
const EFFORTS = [
  { value: '', label: 'Default' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
  { value: 'xhigh', label: 'Extra high' },
]

/**
 * How hard the model reasons before it answers. The same control in a
 * provider's settings, where it sets the default, and in the chat, where it
 * overrides that default. Without a field label beside it, it names itself
 * inside the trigger.
 */
export function EffortSelect({ id, value, onChange, fallback, labelled = true, size = 'default', className }: {
  id?: string
  value: string
  onChange: (value: string) => void
  /** The provider's own default, shown on the empty choice. */
  fallback?: string
  /** False when no field label names the control, as in the chat's header. */
  labelled?: boolean
  size?: 'sm' | 'default'
  className?: string
}) {
  const fallbackLabel = EFFORTS.find((effort) => effort.value === fallback && fallback)?.label
  const items = EFFORTS.map((effort) =>
    effort.value === '' && fallbackLabel ? { value: '', label: `Default (${fallbackLabel.toLowerCase()})` } : effort,
  )
  return (
    <Select items={items} value={value} onValueChange={(next) => onChange(next ?? '')}>
      <SelectTrigger id={id} size={size} aria-label="Effort" className={cn('min-w-0', className)}>
        <span className="flex min-w-0 items-center gap-1.5">
          {!labelled && <span className="text-muted-foreground">Effort</span>}
          <SelectValue />
        </span>
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {items.map((effort) => <SelectItem key={effort.value} value={effort.value}>{effort.label}</SelectItem>)}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
