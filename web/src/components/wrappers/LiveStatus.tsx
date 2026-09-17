import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { LiveStatusContext, type Live } from '@/lib/live-status'

/**
 * Connection state belongs in one place. The board owns the SSE stream but the
 * shell owns the lamp, so the board reports up rather than drawing a second
 * banner that says what the lamp already says.
 */
export function LiveStatusProvider({ children }: { children: ReactNode }) {
  const [live, setLiveState] = useState<Live>('unknown')
  const setLive = useCallback((next: Live) => setLiveState(next), [])
  const value = useMemo(() => ({ live, setLive }), [live, setLive])
  return <LiveStatusContext.Provider value={value}>{children}</LiveStatusContext.Provider>
}
