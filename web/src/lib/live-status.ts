import { createContext, useContext } from 'react'

export type Live = 'connected' | 'disconnected' | 'unknown'

interface LiveStatusValue {
  live: Live
  setLive: (live: Live) => void
}

export const LiveStatusContext = createContext<LiveStatusValue>({ live: 'unknown', setLive: () => {} })

/** The connection state, and the setter the screen owning the stream reports through. */
export function useLiveStatus() {
  return useContext(LiveStatusContext)
}
