/**
 * What happened in a project, as marks on a few fixed lanes: when each card was
 * created, when work on it started, when it was finished, and when knowledge
 * was written. The lanes never grow with the project; only the marks do.
 */

/** One row of `GET /api/p/{key}/events`, oldest first. */
export interface ProjectEvent {
  seq: number
  ts: number
  actor: string
  kind: 'card' | 'knowledge'
  ref: string
  action: string
  field?: string
  old?: string
  new?: string
}

export interface TimelineCard {
  ref: string
  title: string
  createdAt: number
  owner?: string
}

export type LaneName = 'created' | 'started' | 'finished' | 'written'

export interface Mark {
  at: number
  lane: LaneName
  /** A card ref, or a knowledge ref (`KEY/slug`). */
  ref: string
  title: string
  actor?: string
  /** A card an agent holds right now, or an entry edited rather than written. */
  flag?: boolean
}

export function buildMarks({
  cards,
  events,
  titles,
  queue,
  doneColumns,
}: {
  cards: TimelineCard[]
  events: ProjectEvent[]
  /** Entry titles by knowledge ref. */
  titles: Map<string, string>
  /** The first column: a card leaving it has been started. */
  queue: string
  doneColumns: Set<string>
}): Record<LaneName, Mark[]> {
  const known = new Map(cards.map((card) => [card.ref, card]))
  const lanes: Record<LaneName, Mark[]> = { created: [], started: [], finished: [], written: [] }

  for (const card of cards) {
    lanes.created.push({ at: card.createdAt, lane: 'created', ref: card.ref, title: card.title })
  }

  const started = new Set<string>()
  for (const event of events) {
    if (event.kind === 'knowledge') {
      if (event.action !== 'created' && event.action !== 'edited') continue
      lanes.written.push({
        at: event.ts,
        lane: 'written',
        ref: event.ref,
        title: titles.get(event.ref) ?? event.ref,
        actor: event.actor,
        flag: event.action === 'edited',
      })
      continue
    }
    const card = known.get(event.ref)
    if (!card) continue
    const move = event.action === 'moved' && event.field === 'column'
    // Work starts the first time an agent claims the card or it leaves the
    // queue, whichever comes first.
    if (!started.has(card.ref) && (event.action === 'claimed' || (move && event.old === queue))) {
      started.add(card.ref)
      lanes.started.push({
        at: event.ts,
        lane: 'started',
        ref: card.ref,
        title: card.title,
        actor: event.actor,
        flag: Boolean(card.owner),
      })
    }
    // A card reopened and finished again is finished twice.
    if (move && doneColumns.has(event.new ?? '')) {
      lanes.finished.push({ at: event.ts, lane: 'finished', ref: card.ref, title: card.title, actor: event.actor })
    }
  }
  return lanes
}

export interface Cluster {
  key: string
  x: number
  marks: Mark[]
}

/**
 * Marks closer than `gap` pixels to a group's centre join it, so a burst of
 * work reads as one counted mark instead of a smear, and no two marks overlap.
 * The gap is the width of a counted mark. Zooming in pulls groups apart.
 */
export function clusterMarks(marks: Mark[], x: (t: number) => number, gap = 36): Cluster[] {
  const sorted = [...marks].sort((a, b) => a.at - b.at)
  const clusters: Cluster[] = []
  let group: Mark[] = []
  let sum = 0
  const settle = () => {
    if (group.length === 0) return
    clusters.push({ key: `${group[0].lane}-${group[0].at}-${group[0].ref}`, x: sum / group.length, marks: group })
    group = []
    sum = 0
  }
  for (const mark of sorted) {
    const px = x(mark.at)
    if (group.length > 0 && px - sum / group.length > gap) settle()
    group.push(mark)
    sum += px
  }
  settle()
  return clusters
}
