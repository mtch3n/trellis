/** Card vocabulary shared by the board, the card views and the overview. */

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
export function shortActor(owner?: string) {
  if (!owner) return null
  const [kind, ...rest] = owner.split(':')
  const name = rest.join(':')
  if (kind === 'agent') return name.slice(0, 8)
  if (kind === 'human') return name || owner
  return owner
}
