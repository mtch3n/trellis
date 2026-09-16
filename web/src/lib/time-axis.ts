/**
 * Time laid out left to right, with quiet stretches optionally skipped: the
 * axis spends its width where something happened and says how much time it
 * jumped over.
 */

/** A stretch of the axis drawn at true scale. */
export interface Span {
  from: number
  to: number
  x: number
  width: number
}

/** Quiet time the axis jumped over, drawn at a fixed width. */
export interface Skip {
  from: number
  to: number
  x: number
  width: number
}

export interface TimeAxis {
  start: number
  end: number
  width: number
  spans: Span[]
  skips: Skip[]
  x: (t: number) => number
}

const HOUR = 3_600_000

/**
 * Lay time out left to right. With `skipAfter` set, instants closer than that
 * share a span at true scale, padded a little either side, and every longer
 * silence becomes a fixed-width skip. Without it, the axis is one span.
 */
export function buildAxis(
  instants: number[],
  { pxPerHour, skipAfter, pad = 15 * 60_000, skipWidth = 56, minSpan = 24 }: {
    pxPerHour: number
    skipAfter: number | null
    pad?: number
    skipWidth?: number
    minSpan?: number
  },
): TimeAxis {
  const times = [...new Set(instants)].sort((a, b) => a - b)
  const start = (times[0] ?? 0) - pad
  const end = times[times.length - 1] ?? 0

  const ranges: Array<{ from: number; to: number }> = []
  if (skipAfter === null) {
    ranges.push({ from: start, to: end })
  } else {
    for (const t of times) {
      const last = ranges[ranges.length - 1]
      if (last && t - last.to <= skipAfter) last.to = Math.min(t + pad, end)
      else ranges.push({ from: t - pad, to: Math.min(t + pad, end) })
    }
  }

  const spans: Span[] = []
  const skips: Skip[] = []
  let x = 0
  ranges.forEach((range, index) => {
    if (index > 0) {
      const previous = ranges[index - 1]
      skips.push({ from: previous.to, to: range.from, x, width: skipWidth })
      x += skipWidth
    }
    const width = Math.max(((range.to - range.from) / HOUR) * pxPerHour, minSpan)
    spans.push({ ...range, x, width })
    x += width
  })

  // Every span and skip boundary is a knot; between knots, time is linear.
  const knots: Array<[number, number]> = []
  for (let i = 0; i < spans.length; i++) {
    knots.push([spans[i].from, spans[i].x], [spans[i].to, spans[i].x + spans[i].width])
  }

  const at = (t: number) => {
    if (knots.length === 0) return 0
    if (t <= knots[0][0]) return knots[0][1]
    const last = knots[knots.length - 1]
    if (t >= last[0]) return last[1]
    let low = 0
    let high = knots.length - 1
    while (high - low > 1) {
      const mid = (low + high) >> 1
      if (knots[mid][0] <= t) low = mid
      else high = mid
    }
    const [t0, x0] = knots[low]
    const [t1, x1] = knots[high]
    return t1 === t0 ? x0 : x0 + ((t - t0) / (t1 - t0)) * (x1 - x0)
  }

  return { start, end, width: x, spans, skips, x: at }
}

export interface Tick {
  at: number
  x: number
  time: string
  /** Set on the first tick of a day within a span. */
  day?: string
}

const STEPS = [1, 2, 3, 6, 12, 24]

/**
 * Hour ticks inside each span, spaced so their labels never collide. A tick
 * too close to the end of its span is dropped rather than let its label run
 * into the skip that follows; the day label moves to the next tick shown. A
 * span with no round hour to show is labelled with its own start instead.
 */
export function axisTicks(axis: TimeAxis, pxPerHour: number, minGap = 72, labelWidth = 48): Tick[] {
  const step = STEPS.find((hours) => hours * pxPerHour >= minGap) ?? 24
  const ticks: Tick[] = []
  let lastDay = ''
  for (const span of axis.spans) {
    const cursor = new Date(span.from)
    cursor.setMinutes(0, 0, 0)
    cursor.setHours(Math.ceil((cursor.getHours() + (span.from > cursor.getTime() ? 1 : 0)) / step) * step)
    let firstInSpan = true
    const before = ticks.length
    for (; cursor.getTime() <= span.to; cursor.setHours(cursor.getHours() + step)) {
      const at = cursor.getTime()
      const x = axis.x(at)
      const last = span === axis.spans[axis.spans.length - 1]
      if (!last && span.x + span.width - x < labelWidth) continue
      const day = cursor.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
      ticks.push({
        at,
        x,
        time: cursor.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }),
        day: day !== lastDay || firstInSpan ? day : undefined,
      })
      lastDay = day
      firstInSpan = false
    }
    // A span too short for a round hour still says when it was.
    if (ticks.length === before) {
      const start = new Date(span.from)
      lastDay = start.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
      ticks.splice(before, 0, {
        at: span.from,
        x: span.x,
        time: start.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }),
        day: lastDay,
      })
    }
  }
  return ticks
}

/** "11h", "3d 4h": how much quiet a skip jumped over. */
export function skipped(ms: number) {
  const hours = Math.round(ms / HOUR)
  if (hours < 24) return `${Math.max(hours, 1)}h`
  const days = Math.floor(hours / 24)
  const rest = hours % 24
  return rest ? `${days}d ${rest}h` : `${days}d`
}
