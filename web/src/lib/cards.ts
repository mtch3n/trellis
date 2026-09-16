/** Card vocabulary shared by the board, the card views and the overview. */

export const PRIORITIES = ['urgent', 'high', 'normal', 'low'] as const
export const PRIORITY_NUMBERS: Record<(typeof PRIORITIES)[number], number> = {
  urgent: 0, high: 1, normal: 2, low: 3,
}

export function shortActor(owner?: string) {
  if (!owner) return null
  return owner.replace(/^agent:/, '').slice(0, 8)
}
