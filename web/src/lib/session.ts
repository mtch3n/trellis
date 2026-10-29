import { createContext, useContext } from 'react'
import type { Me } from '@/lib/api'

export const SessionContext = createContext<Me | null>(null)

export function useSession() {
  const me = useContext(SessionContext)
  if (!me) throw new Error('useSession needs a signed-in session')
  return me
}
