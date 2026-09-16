import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface HeaderFact {
  label: string
  value: number | string
  tone?: 'held' | 'danger'
}

/**
 * One compact row: what you are looking at, what is exceptional about it, and
 * what you can do. Counts that a column or a section heading already prints do
 * not belong here, and a fact worth nothing at zero is not shown at zero.
 *
 * The dashboard is read-first, so the header is deliberately short. Every row it
 * does not take is a row of content above the fold.
 */
export function PageHeader({
  title,
  facts = [],
  actions,
  className,
}: {
  title: ReactNode
  facts?: HeaderFact[]
  actions?: ReactNode
  className?: string
}) {
  const shown = facts.filter((fact) => fact.value !== 0 && fact.value !== '')
  return (
    <header
      className={cn(
        // No rule under it: the content below is its own surface, and a line
        // between the two only repeats that.
        'flex min-h-16 shrink-0 flex-wrap items-center gap-x-8 gap-y-3 py-4',
        className,
      )}
    >
      <h1 className="text-title">{title}</h1>

      {shown.length > 0 && (
        <dl className="flex flex-wrap items-baseline gap-x-5 gap-y-1">
          {shown.map((fact) => (
            <div key={fact.label} className="flex items-baseline gap-1.5">
              <dt className="text-xs text-muted-foreground">{fact.label}</dt>
              <dd
                className={cn(
                  'text-xs font-medium',
                  fact.tone === 'held' && 'text-held',
                  fact.tone === 'danger' && 'text-danger',
                )}
              >
                {fact.value}
              </dd>
            </div>
          ))}
        </dl>
      )}

      {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
    </header>
  )
}
