import { useEffect, useState } from 'react'

/**
 * The current time, refreshed on an interval. A claim runs out on a wall
 * clock, so a card that shows whether one is still live has to watch the
 * clock rather than read it once while rendering: the clock is the external
 * system here, and this is how a component synchronises with it.
 */
export function useNow(everyMs = 30_000) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), everyMs)
    return () => clearInterval(timer)
  }, [everyMs])
  return now
}
