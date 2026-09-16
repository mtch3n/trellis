import type { HistoryEvent } from '@/components/wrappers/HistoryList'

/**
 * The events worth reading: a save that rewrote a value with itself, or a
 * rank shuffle behind a move, is not a change anyone asked about.
 */
export function meaningfulEvents(events: HistoryEvent[]) {
  return events.filter(
    (event) => event.action !== 'reordered' && !(event.old_value && event.old_value === event.new_value),
  )
}
