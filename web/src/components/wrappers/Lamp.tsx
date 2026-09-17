import { cn } from '@/lib/utils'

export type LampState = 'idle' | 'claimed' | 'alarm' | 'live'

const LABEL: Record<LampState, string> = {
  idle: 'Idle',
  claimed: 'Claimed',
  alarm: 'Blocked',
  live: 'Connected',
}

/**
 * The one state vocabulary in the interface. A row, a tile, a column and the
 * connection all report themselves through the same lamp, so a quiet board
 * reads as quiet without anyone having to learn a second legend.
 *
 * `live` is round because it is a connection light rather than a work state,
 * and it is the only place green appears.
 */
export function Lamp({
  state = 'idle',
  label,
  className,
}: {
  state?: LampState
  label?: string
  className?: string
}) {
  return (
    <span
      role="img"
      aria-label={label ?? LABEL[state]}
      data-state={state}
      className={cn(
        'inline-block size-2 shrink-0',
        state === 'idle' && 'shadow-lamp-idle',
        state === 'claimed' && 'bg-claimed',
        state === 'alarm' && 'lamp-alarm bg-danger',
        state === 'live' && 'lamp-live rounded-full bg-live',
        className,
      )}
    />
  )
}
