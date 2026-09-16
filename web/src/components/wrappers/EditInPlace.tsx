import { useEffect, useRef, type FormEvent, type ReactNode } from 'react'
import { Code, Plus, Save } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Kbd, KbdGroup } from '@/components/ui/kbd'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { Toggle } from '@/components/ui/toggle'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

/**
 * The form around fields edited in place. Ctrl or Cmd with Enter submits from
 * anywhere inside it, the body editor included.
 */
export function EditForm({
  id,
  onSubmit,
  children,
}: {
  id: string
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  children: ReactNode
}) {
  return (
    <form
      id={id}
      onSubmit={onSubmit}
      onKeyDown={(event) => {
        if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
          event.preventDefault()
          event.currentTarget.requestSubmit()
        }
      }}
    >
      {children}
    </form>
  )
}

/**
 * A line of text edited where it is read: a title, a summary. It wraps like
 * the text it replaces and grows with it, so nothing below moves. It is one
 * paragraph, so Enter submits instead of breaking the line.
 */
export function InPlaceText({
  label,
  value,
  placeholder,
  className,
  focusOnMount = false,
  required = false,
  onChange,
}: {
  label: string
  value: string
  placeholder?: string
  /** The type the read view uses, so the words keep their size and colour. */
  className?: string
  /** Start editing here, with the caret after the text as a click at the end would put it. */
  focusOnMount?: boolean
  /** The browser refuses to submit the form while this is empty, and says so. */
  required?: boolean
  onChange: (value: string) => void
}) {
  const field = useRef<HTMLTextAreaElement>(null)
  useEffect(() => {
    const node = field.current
    if (!focusOnMount || !node) return
    node.focus()
    node.setSelectionRange(node.value.length, node.value.length)
  }, [focusOnMount])

  return (
    // The wash is part of the field: a press on its margin lands in the text.
    <div
      className="edit-surface"
      onPointerDown={(event) => {
        if (event.target !== event.currentTarget) return
        event.preventDefault()
        field.current?.focus()
      }}
    >
      <Textarea
        ref={field}
        aria-label={label}
        rows={1}
        required={required}
        placeholder={placeholder}
        className={cn(
          'min-h-0 resize-none rounded-none border-0 bg-transparent p-0 shadow-none placeholder:text-muted-foreground/45 focus-visible:ring-0 dark:bg-transparent',
          className,
        )}
        value={value}
        onChange={(event) => onChange(event.target.value.replace(/\s*\n\s*/g, ' '))}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' || event.nativeEvent.isComposing) return
          event.preventDefault()
          // Ctrl or Cmd with Enter is the form's to handle.
          if (!event.metaKey && !event.ctrlKey) event.currentTarget.form?.requestSubmit()
        }}
      />
    </div>
  )
}

/**
 * Everything about an edit that is not the text: how the body is shown, and
 * committing or dropping the change. A compact cluster that takes the place
 * of Edit, where Edit was, so starting an edit moves nothing and Save is where
 * the pointer already is. The shortcut lives in Save's tooltip.
 */
export function EditActions({
  form,
  source,
  onSourceChange,
  label,
  creating = false,
  saving,
  onCancel,
}: {
  /** The id of the form this cluster submits. */
  form: string
  source: boolean
  onSourceChange: (source: boolean) => void
  label: string
  creating?: boolean
  saving: boolean
  onCancel?: () => void
}) {
  return (
    <div
      className="flex items-center gap-1.5"
      // The cluster sits outside the form, so the shortcut is honoured here too.
      onKeyDown={(event) => {
        if (event.key !== 'Enter' || !(event.metaKey || event.ctrlKey)) return
        event.preventDefault()
        const target = document.getElementById(form)
        if (target instanceof HTMLFormElement) target.requestSubmit()
      }}
    >
      <Tooltip>
        <TooltipTrigger
          render={
            <Toggle
              variant="outline"
              size="sm"
              aria-label="Markdown source"
              pressed={source}
              onPressedChange={onSourceChange}
            />
          }
        >
          <Code />
        </TooltipTrigger>
        <TooltipContent>Markdown source</TooltipContent>
      </Tooltip>
      {onCancel && (
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>Cancel</Button>
      )}
      <Tooltip>
        <TooltipTrigger render={<Button type="submit" form={form} size="sm" disabled={saving} />}>
          {saving ? <Spinner data-icon="inline-start" /> : creating ? <Plus data-icon="inline-start" /> : <Save data-icon="inline-start" />}
          {label}
        </TooltipTrigger>
        <TooltipContent>
          <KbdGroup>
            <Kbd>Ctrl</Kbd>
            <Kbd>Enter</Kbd>
          </KbdGroup>
        </TooltipContent>
      </Tooltip>
    </div>
  )
}
