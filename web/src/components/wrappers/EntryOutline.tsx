import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent } from 'react'
import { cn } from '@/lib/utils'
import type { OutlineHeading } from '@/lib/outline'

/**
 * The entry's headings as a rail beside it, marking the sections on screen.
 * A heading's section runs to the next heading, and every section that shows
 * any of itself in the scrolling pane counts as in view, so the thumb on the
 * rail spans what is being read rather than jumping between single headings.
 * A click scrolls to the heading and leaves the address alone.
 */
export function EntryOutline({
  headings,
  scroller,
  className,
}: {
  headings: OutlineHeading[]
  /** The pane the entry scrolls in. */
  scroller: HTMLElement | null
  className?: string
}) {
  const [active, setActive] = useState<string[]>([])
  // `instant` on the first placement, so a fresh outline's thumb lands where it
  // belongs instead of sliding down from the top.
  const [thumb, setThumb] = useState<{ top: number; height: number; instant: boolean } | null>(null)
  const items = useRef(new Map<string, HTMLLIElement>())
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!scroller) return
    let frame = 0
    const measure = () => {
      frame = 0
      const view = scroller.getBoundingClientRect()
            const end = view.top - scroller.scrollTop + scroller.scrollHeight
      const starts = headings.map((heading) => find(scroller, heading.id)?.getBoundingClientRect().top ?? end)
      const next = headings
        .filter((_, i) => (starts[i + 1] ?? end) > view.top && starts[i] < view.bottom)
        .map((heading) => heading.id)
      setActive((current) => (current.join('\n') === next.join('\n') ? current : next))
    }
    const schedule = () => { if (!frame) frame = requestAnimationFrame(measure) }
    measure()
    scroller.addEventListener('scroll', schedule, { passive: true })
    window.addEventListener('resize', schedule)
    return () => {
      cancelAnimationFrame(frame)
      scroller.removeEventListener('scroll', schedule)
      window.removeEventListener('resize', schedule)
    }
  }, [headings, scroller])

  useLayoutEffect(() => {
    const first = active.length ? items.current.get(active[0]) : undefined
    const last = active.length ? items.current.get(active[active.length - 1]) : undefined
    setThumb((current) =>
      first && last
        ? { top: first.offsetTop, height: last.offsetTop + last.offsetHeight - first.offsetTop, instant: current === null }
        : null,
    )
    // A long outline scrolls itself to keep the first section read in sight.
    const rail = list.current
    if (!first || !rail) return
    if (first.offsetTop < rail.scrollTop) rail.scrollTop = first.offsetTop
    else if (first.offsetTop + first.offsetHeight > rail.scrollTop + rail.clientHeight) {
      rail.scrollTop = first.offsetTop + first.offsetHeight - rail.clientHeight
    }
  }, [active, headings])

  const go = (id: string) => (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault()
    const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    if (scroller) find(scroller, id)?.scrollIntoView({ behavior: still ? 'auto' : 'smooth', block: 'start' })
  }

  return (
    <nav aria-label="On this page" className={cn('flex flex-col gap-3', className)}>
      <h2 className="text-label text-muted-foreground">On this page</h2>
      <div ref={list} className="relative min-h-0 scrollbar-hidden overflow-y-auto">
        <div aria-hidden="true" className="absolute inset-y-0 left-0 w-px bg-border" />
        <div
          aria-hidden="true"
          className={cn(
            'absolute left-0 w-0.5 bg-foreground',
            thumb?.instant ? 'transition-none' : 'transition-all duration-200 ease-settle',
            !thumb && 'opacity-0',
          )}
          style={thumb ? { top: thumb.top, height: thumb.height } : undefined}
        />
        <ul className="flex flex-col">
          {headings.map((heading) => {
            const current = active.includes(heading.id)
            return (
              <li
                key={heading.id}
                ref={(node) => { if (node) items.current.set(heading.id, node); else items.current.delete(heading.id) }}
              >
                <a
                  href={`#${heading.id}`}
                  aria-current={current && heading.id === active[0] ? 'location' : undefined}
                  className={cn(
                    'block py-1 pr-2 text-sm leading-snug text-pretty text-muted-foreground transition-colors hover:text-foreground',
                    current && 'text-foreground',
                  )}
                  style={{ paddingLeft: `${0.875 + heading.depth * 0.75}rem` }}
                  onClick={go(heading.id)}
                >
                  {heading.text}
                </a>
              </li>
            )
          })}
        </ul>
      </div>
    </nav>
  )
}

/** A heading by its id, looked up inside the pane rather than the whole page. */
function find(scroller: HTMLElement, id: string) {
  return scroller.querySelector<HTMLElement>(`#${CSS.escape(id)}`)
}
