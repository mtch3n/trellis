/** Card vocabulary shared by the board, the card views and the overview. */
import type { CardInfo } from '@/components/wrappers/CardView'
import type { CardComment, CardEvent } from '@/components/wrappers/CardTimeline'
import type { Relation } from '@/components/wrappers/RelationsEditor'
import type { CardLink } from '@/components/wrappers/EntryLinksEditor'
import type { Artifact } from '@/components/wrappers/ArtifactList'

/**
 * One card as its detail route answers: the card, and beside it everything
 * that is about the card rather than in it.
 */
export interface CardDetail {
  card: CardInfo
  comments?: CardComment[]
  events?: CardEvent[]
  relations?: Relation[]
  links?: CardLink[]
  artifacts?: Artifact[]
}

/**
 * The card with what sits beside it folded in, which is how the views read
 * it: a card carries its own relations, citations and files.
 */
export function withDetail(detail: CardDetail): CardInfo {
  return {
    ...detail.card,
    relations: detail.relations ?? [],
    links: detail.links ?? [],
    artifacts: detail.artifacts ?? [],
  }
}

export const PRIORITIES = ['urgent', 'high', 'normal', 'low'] as const
export const PRIORITY_NUMBERS: Record<(typeof PRIORITIES)[number], number> = {
  urgent: 0, high: 1, normal: 2, low: 3,
}

/**
 * Who did something, in the space a row can spare. An agent id is a hash, and
 * eight characters tell two of them apart; a person's name is a name, so it
 * stays whole, and anything else keeps its kind because the kind is the
 * interesting part (`cli:4821`, `daemon:2`).
 */
export function shortActor(actor?: string) {
  if (!actor) return null
  const [kind, ...rest] = actor.split(':')
  const name = rest.join(':')
  if (kind === 'agent') return name.slice(0, 8)
  if (kind === 'human') return name || actor
  return actor
}

/** Each card relation and its inverse, in the order a reader weighs them. */
export const RELATIONS = [
  { value: 'blocked_by', label: 'Blocked by' },
  { value: 'blocks', label: 'Blocks' },
  { value: 'resolved_by', label: 'Resolved by' },
  { value: 'resolves', label: 'Resolves' },
  { value: 'duplicate_of', label: 'Duplicate of' },
  { value: 'duplicated_by', label: 'Duplicated by' },
  { value: 'relates_to', label: 'Related to' },
] as const
