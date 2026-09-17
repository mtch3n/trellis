import { useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

export interface MetaFact {
  label: string
  value: ReactNode
  tone?: 'held' | 'danger'
  /** Identifiers (refs, slugs, agent ids) set in mono; words, times and numbers stay sans. */
  mono?: boolean
  /** A value too long for one line sits under its label instead of beside it. */
  stacked?: boolean
}

/**
 * The right column. One shape for every surface that shows an artefact: the
 * card dialog, the card page and the knowledge entry all describe their subject
 * the same way, so moving between them costs no re-reading.
 */
export function MetaPanel({ children, className }: { children: ReactNode; className?: string }) {
  return <aside className={cn('flex flex-col gap-8', className)}>{children}</aside>
}

/**
 * One labelled section. A collapsible group remembers whether it was open per
 * browser, so a reader who never looks at a file path does not keep seeing it.
 */
export function MetaGroup({
  label,
  count,
  children,
  className,
  collapsible = false,
}: {
  label: string
  count?: number
  children: ReactNode
  className?: string
  collapsible?: boolean
}) {
  if (collapsible) {
    return <CollapsibleGroup label={label} count={count} className={className}>{children}</CollapsibleGroup>
  }
  return (
    <section className={className}>
      <h2 className="flex items-baseline gap-2 text-label text-muted-foreground">
        {label}
        {count !== undefined && <span className="font-normal">{count}</span>}
      </h2>
      <div className="mt-3">{children}</div>
    </section>
  )
}

function CollapsibleGroup({
  label,
  count,
  children,
  className,
}: {
  label: string
  count?: number
  children: ReactNode
  className?: string
}) {
  const key = `trellis.meta.${label}`
  const [open, setOpen] = useState(() => {
    try { return localStorage.getItem(key) !== 'closed' } catch { return true }
  })
  const toggle = (next: boolean) => {
    setOpen(next)
    try { localStorage.setItem(key, next ? 'open' : 'closed') } catch { /* private mode */ }
  }
  return (
    <Collapsible open={open} onOpenChange={toggle} render={<section className={className} />}>
      <h2>
        <CollapsibleTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="-mx-2 h-7 w-full justify-start gap-1.5 px-2 text-label text-muted-foreground hover:text-foreground aria-expanded:bg-transparent aria-expanded:text-muted-foreground aria-expanded:hover:bg-muted aria-expanded:hover:text-foreground"
            />
          }
        >
          <ChevronRight
            data-icon="inline-start"
            className="transition-transform duration-200 ease-settle group-aria-expanded/button:rotate-90"
          />
          {label}
          {count !== undefined && <span className="font-normal">{count}</span>}
        </CollapsibleTrigger>
      </h2>
      {/* The clip reaches past the column by the rows' hover bleed, so a
          focused control at a row's edge never scrolls the group sideways. */}
      <CollapsibleContent className="-mx-2 h-(--collapsible-panel-height) overflow-hidden px-2 transition-all duration-200 ease-settle data-ending-style:h-0 data-starting-style:h-0">
        <div className="pt-2">{children}</div>
      </CollapsibleContent>
    </Collapsible>
  )
}

/**
 * Label and value on one line, aligned by whitespace rather than a rule under
 * every row.
 */
export function MetaFacts({ facts }: { facts: MetaFact[] }) {
  return (
    <dl className="flex flex-col gap-2">
      {facts.map((fact) => (
        <div
          key={fact.label}
          className={cn('flex gap-4', fact.stacked ? 'flex-col gap-0.5' : 'items-baseline justify-between')}
        >
          <dt className="shrink-0 text-xs text-muted-foreground">{fact.label}</dt>
          <dd
            className={cn(
              'min-w-0',
              fact.stacked ? 'break-all' : 'text-right',
              fact.mono ? 'text-meta' : 'text-sm',
              fact.tone === 'held' && 'text-held',
              fact.tone === 'danger' && 'text-danger',
            )}
          >
            {fact.value}
          </dd>
        </div>
      ))}
    </dl>
  )
}
