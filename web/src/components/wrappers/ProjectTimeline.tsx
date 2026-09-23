import { useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Check, FastForward, Maximize2, Play, Plus, Triangle, ZoomIn, ZoomOut } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Toggle } from '@/components/ui/toggle'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { IconButton } from '@/components/wrappers/IconButton'
import { shortActor } from '@/lib/cards'
import { axisTicks, buildAxis, rulerLabels, skipped, type TimeAxis } from '@/lib/time-axis'
import { clusterMarks, type Cluster, type LaneName, type Mark } from '@/lib/timeline-marks'
import { cn } from '@/lib/utils'

/** The lane label column, in pixels: w-32. */
const LABEL = 128
/** Room after now, so the last mark is not flush with the edge. */
const TAIL = 40
const HOUR = 3_600_000
/** Silences longer than this are skipped when skipping is on. */
const QUIET = 2 * HOUR
const SKIP_WIDTH = 56
const MIN_RATE = 6
const MAX_RATE = 960

const LANES: ReadonlyArray<{ name: LaneName; label: string; verb: string; noun: [string, string] }> = [
  { name: 'created', label: 'Created', verb: 'created', noun: ['card', 'cards'] },
  { name: 'started', label: 'Started', verb: 'started', noun: ['card', 'cards'] },
  { name: 'finished', label: 'Done', verb: 'finished', noun: ['card', 'cards'] },
  { name: 'written', label: 'Entries', verb: 'written', noun: ['entry', 'entries'] },
]

const time = (ms: number) => new Date(ms).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
const day = (ms: number) => new Date(ms).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })

/**
 * What happened in a project, on four lanes that never grow: cards created,
 * cards started, cards finished, and entries written. Each event is a mark
 * at its moment; marks too close to tell apart merge into one with a count,
 * and pointing at any mark lists what it stands for, with links.
 *
 * Time is proportional while anything happens. A silence longer than two
 * hours becomes a narrow, labelled skip, so a night or a weekend costs almost
 * no width and still shows where it was; the skips can be turned off for true
 * time. It opens fitted to the width, or on now when there is more than fits.
 */
export function ProjectTimeline({
  marks,
  base,
  now,
}: {
  marks: Record<LaneName, Mark[]>
  base: string
  now: number
}) {
  const scroller = useRef<HTMLDivElement>(null)
  const [room, setRoom] = useState(0)
  const [skipQuiet, setSkipQuiet] = useState(true)
  const [zoom, setZoom] = useState<number | null>(null)

  useLayoutEffect(() => {
    const node = scroller.current
    if (!node) return
    const measure = () => setRoom(Math.max(node.clientWidth - LABEL - TAIL, 0))
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [])

  const instants = useMemo(
    () => [now, ...LANES.flatMap((lane) => marks[lane.name].map((mark) => mark.at))],
    [marks, now],
  )

  const skipAfter = skipQuiet ? QUIET : null
  // Fitting: at one pixel an hour, the spans' widths are their hours, and the
  // skips cost their fixed width whatever the rate.
  const fit = useMemo(() => {
    const unit = buildAxis(instants, { pxPerHour: 1, skipAfter, skipWidth: SKIP_WIDTH, minSpan: 0 })
    const hours = unit.spans.reduce((sum, span) => sum + (span.to - span.from) / HOUR, 0)
    const free = room - unit.skips.length * SKIP_WIDTH
    return clamp(hours > 0 ? free / hours : MAX_RATE)
  }, [instants, skipAfter, room])
  const rate = zoom ?? fit

  const axis = useMemo(
    () => buildAxis(instants, { pxPerHour: rate, skipAfter, skipWidth: SKIP_WIDTH }),
    [instants, rate, skipAfter],
  )
  const ticks = useMemo(() => axisTicks(axis, rate), [axis, rate])
  const clusters = useMemo(
    () => new Map(LANES.map((lane) => [lane.name, clusterMarks(marks[lane.name], axis.x)])),
    [marks, axis],
  )

  // A record wider than the strip opens on now.
  useLayoutEffect(() => {
    const node = scroller.current
    if (node) node.scrollLeft = node.scrollWidth
  }, [axis.width, room])

  const first = Math.min(...instants)
  const since = new Date(first).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })

  return (
    <section>
      <div className="flex items-center gap-2">
        <h2 className="text-heading">Timeline</h2>
        <span className="text-xs text-muted-foreground">Since {since}</span>
        <div className="ml-auto flex items-center gap-1">
          <Tooltip>
            <TooltipTrigger
              render={
                <Toggle
                  size="sm"
                  aria-label="Skip quiet time"
                  pressed={skipQuiet}
                  onPressedChange={(pressed) => { setSkipQuiet(pressed); setZoom(null) }}
                />
              }
            >
              <FastForward />
            </TooltipTrigger>
            <TooltipContent>Skip quiet stretches over two hours</TooltipContent>
          </Tooltip>
          <IconButton label="Zoom out" disabled={rate <= MIN_RATE} onClick={() => setZoom(clamp(rate / 1.5))}>
            <ZoomOut />
          </IconButton>
          <IconButton label="Zoom in" disabled={rate >= MAX_RATE} onClick={() => setZoom(clamp(rate * 1.5))}>
            <ZoomIn />
          </IconButton>
          <IconButton label="Fit to width" disabled={zoom === null} onClick={() => setZoom(null)}>
            <Maximize2 />
          </IconButton>
        </div>
      </div>

      {/* A region that takes focus, so the arrow keys scroll it too. */}
      <div
        ref={scroller}
        role="region"
        aria-label="Timeline"
        tabIndex={0}
        className="timeline-scroll mt-3 scroll-fade-x overflow-x-auto overscroll-x-contain pb-2 outline-none focus-visible:ring-1 focus-visible:ring-ring"
      >
        <div className="relative flex flex-col gap-1" style={{ width: LABEL + axis.width + TAIL }}>
          <Ruler axis={axis} ticks={ticks} now={now} />

          {LANES.map((lane) => (
            <Lane key={lane.name} label={lane.label} count={marks[lane.name].length}>
              {clusters.get(lane.name)?.map((cluster) => (
                <MarkCluster key={cluster.key} cluster={cluster} lane={lane} base={base} />
              ))}
            </Lane>
          ))}

          {/* Over the lanes: the skipped time, and the line where now is. */}
          <div className="pointer-events-none absolute top-12 bottom-0" style={{ left: LABEL, width: axis.width }}>
            {axis.skips.map((skip) => (
              <div key={skip.from} className="timeline-skip absolute inset-y-0" style={{ left: skip.x, width: skip.width }} />
            ))}
            <div className="absolute inset-y-0 w-px bg-foreground/40" style={{ left: axis.x(now) }} />
          </div>
        </div>
      </div>
    </section>
  )
}

function clamp(rate: number) {
  return Math.min(Math.max(rate, MIN_RATE), MAX_RATE)
}

/** Day and hour marks, each skip's length, and now. */
function Ruler({ axis, ticks, now }: { axis: TimeAxis; ticks: ReturnType<typeof axisTicks>; now: number }) {
  const nowX = axis.x(now)
  // Short spans sit close together, so their labels are laid out to never
  // touch: whatever does not fit is left out, never drawn over another.
  // The ticks name days the same way, so today's name can be matched.
  const today = new Date(now).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
  const labels = useMemo(() => rulerLabels(ticks, axis.skips, nowX, today), [ticks, axis.skips, nowX, today])
  return (
    <div className="flex h-11">
      <div className="sticky left-0 z-10 w-32 shrink-0 bg-background" />
      <div className="relative flex-1">
        {ticks.map((tick) => (
          <div key={tick.at} className="absolute inset-y-0 flex flex-col justify-end pb-1" style={{ left: tick.x }}>
            <span className={cn('pl-1.5 text-label whitespace-nowrap', !labels.days.has(tick.at) && 'invisible')}>
              {tick.day ?? '\u00a0'}
            </span>
            <span className="flex items-center gap-1.5 text-meta whitespace-nowrap text-muted-foreground">
              <span className="h-2.5 w-px bg-rule-strong" />
              {labels.times.has(tick.at) && tick.time}
            </span>
          </div>
        ))}
        {axis.skips.map((skip) => (
          <div
            key={skip.from}
            title={`${skipped(skip.to - skip.from)} of quiet skipped`}
            className="absolute inset-y-0 flex items-end justify-center gap-0.5 pb-1 text-xs whitespace-nowrap text-muted-foreground"
            style={{ left: skip.x, width: skip.width }}
          >
            {labels.skips.has(skip.from) && skipped(skip.to - skip.from)}
          </div>
        ))}
        <span
          className={cn(
            'absolute top-1 text-label',
            labels.now === 'center' && '-translate-x-1/2',
            labels.now === 'after' && 'ml-1.5',
            labels.now === 'before' && '-ml-1.5 -translate-x-full',
          )}
          style={{ left: nowX }}
        >
          Now
        </span>
      </div>
    </div>
  )
}

/** One lane: its name and count pinned at the left, and its track. */
function Lane({ label, count, children }: { label: string; count: number; children: ReactNode }) {
  return (
    <div className="flex h-9">
      <div className="sticky left-0 z-10 flex w-32 shrink-0 items-baseline gap-2 self-center bg-background pr-3">
        <span className="text-label">{label}</span>
        <span className="text-xs text-muted-foreground">{count}</span>
      </div>
      <div className="relative flex-1 bg-muted/50">{children}</div>
    </div>
  )
}

/** A lane's own symbol, so a mark says what it is without its label. */
function Glyph({ lane, flag }: { lane: LaneName; flag: boolean }) {
  const shape = 'size-3 shrink-0'
  switch (lane) {
    case 'created':
      return <Plus className={shape} />
    case 'started':
      return <Play className={cn(shape, 'fill-current', flag && 'text-claimed')} />
    case 'finished':
      return <Check className={shape} />
    case 'written':
      return <Triangle className={cn(shape, !flag && 'fill-current')} />
  }
}

/**
 * One mark, or several merged into one with a count. Pointing at it opens the
 * list of what it stands for, with links; the list stays open while the
 * pointer moves into it.
 */
function MarkCluster({
  cluster,
  lane,
  base,
}: {
  cluster: Cluster
  lane: (typeof LANES)[number]
  base: string
}) {
  const { marks } = cluster
  const single = marks.length === 1
  const from = marks[0].at
  const to = marks[marks.length - 1].at
  const noun = lane.noun[single ? 0 : 1]
  const when = day(from) === day(to)
    ? `${day(from)}, ${time(from)}${to - from >= 60_000 ? `–${time(to)}` : ''}`
    : `${day(from)} ${time(from)} – ${day(to)} ${time(to)}`
  const heading = `${marks.length} ${noun} ${lane.verb}, ${when}`

  return (
    <Popover>
      <PopoverTrigger
        openOnHover
        delay={120}
        closeDelay={150}
        render={
          <Button
            variant="ghost"
            size={single ? 'icon-xs' : 'xs'}
            aria-label={heading}
            className={cn(
              'absolute top-1/2 -translate-x-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground data-popup-open:text-foreground',
              !single && 'bg-background px-1.5 hover:bg-card data-popup-open:bg-card',
            )}
            style={{ left: cluster.x }}
          />
        }
      >
        <Glyph lane={lane.name} flag={marks.some((mark) => mark.flag)} />
        {!single && <span className="text-xs tabular-nums">{marks.length}</span>}
      </PopoverTrigger>
      <PopoverContent side="top" className="w-96 gap-1 p-1.5">
        <p className="px-1.5 pt-1 text-label text-muted-foreground">{heading}</p>
        <ul className="flex max-h-64 scroll-fade-y flex-col overflow-y-auto">
          {marks.map((mark) => (
            <li key={`${mark.ref}-${mark.at}`}>
              <Link
                to={
                  mark.lane === 'written'
                    ? `${base}/vault/${encodeURIComponent(mark.ref.slice(mark.ref.indexOf('/') + 1))}`
                    : `${base}/card/${encodeURIComponent(mark.ref)}`
                }
                className="flex items-baseline gap-2.5 px-1.5 py-1 transition-colors hover:bg-accent"
              >
                <span className="w-11 shrink-0 text-xs text-muted-foreground">{time(mark.at)}</span>
                <span className="min-w-0 flex-1 truncate text-sm">{mark.title}</span>
                {mark.lane === 'written' ? (
                  mark.flag && <span className="shrink-0 text-xs text-muted-foreground">Edited</span>
                ) : (
                  <span className="shrink-0 text-meta text-muted-foreground">{mark.ref}</span>
                )}
                {mark.lane === 'started' && mark.actor && (
                  <span className={cn('shrink-0 text-meta', mark.flag ? 'text-claimed' : 'text-muted-foreground')}>
                    {shortActor(mark.actor)}
                  </span>
                )}
              </Link>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}
