import { useMemo, useState, type ReactNode } from 'react'
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'

const PER_PAGE = 25

/** At most seven slots: first, last, a window around the current page, gaps. */
function slots(current: number, total: number): (number | 'gap')[] {
  if (total <= 7) return Array.from({ length: total }, (_, index) => index + 1)
  const pages = new Set([1, total, current, current - 1, current + 1])
  const kept = [...pages].filter((page) => page >= 1 && page <= total).sort((a, b) => a - b)
  const out: (number | 'gap')[] = []
  kept.forEach((page, index) => {
    if (index > 0 && page - kept[index - 1] > 1) out.push('gap')
    out.push(page)
  })
  return out
}

/**
 * Pagination for every list in the app.
 *
 * The API returns whole collections rather than pages (it takes a `limit` but
 * no offset or cursor), so the slicing is client-side and honest about it: the
 * count shown is the count in hand.
 *
 * A list that fits on one page renders no pager. Controls that never do
 * anything are furniture.
 */
export function Paged<T>({
  items,
  perPage = PER_PAGE,
  children,
  label = 'Pagination',
}: {
  items: T[]
  perPage?: number
  children: (page: T[]) => ReactNode
  label?: string
}) {
  const [page, setPage] = useState(1)
  const total = Math.max(Math.ceil(items.length / perPage), 1)

  // Filtering can shorten the list under the current page. Adjusted while
  // rendering, so a shortened list never paints an empty page first.
  if (page > total) setPage(1)

  const shown = useMemo(
    () => items.slice((page - 1) * perPage, page * perPage),
    [items, page, perPage],
  )

  if (items.length <= perPage) return <>{children(items)}</>

  const from = (page - 1) * perPage + 1
  const to = Math.min(page * perPage, items.length)

  return (
    <>
      {children(shown)}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-4">
        <p className="text-xs text-muted-foreground">
          {from} to {to} of {items.length}
        </p>

        <Pagination aria-label={label} className="mx-0 w-auto justify-end">
          <PaginationContent>
            <PaginationItem>
              <PaginationPrevious
                href="#"
                aria-disabled={page === 1}
                className={page === 1 ? 'pointer-events-none opacity-50' : undefined}
                onClick={(event) => { event.preventDefault(); setPage((current) => Math.max(current - 1, 1)) }}
              />
            </PaginationItem>

            {slots(page, total).map((slot, index) =>
              slot === 'gap' ? (
                <PaginationItem key={`gap-${index}`}><PaginationEllipsis /></PaginationItem>
              ) : (
                <PaginationItem key={slot}>
                  <PaginationLink
                    href="#"
                    isActive={slot === page}
                    onClick={(event) => { event.preventDefault(); setPage(slot) }}
                  >
                    {slot}
                  </PaginationLink>
                </PaginationItem>
              ),
            )}

            <PaginationItem>
              <PaginationNext
                href="#"
                aria-disabled={page === total}
                className={page === total ? 'pointer-events-none opacity-50' : undefined}
                onClick={(event) => { event.preventDefault(); setPage((current) => Math.min(current + 1, total)) }}
              />
            </PaginationItem>
          </PaginationContent>
        </Pagination>
      </div>
    </>
  )
}
