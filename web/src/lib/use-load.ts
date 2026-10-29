import { useEffect, useEffectEvent, useState } from 'react'

export interface Load<T> {
  data: T | undefined
  error: unknown
  loading: boolean
  reload: () => void
  set: (value: T) => void
}

interface Settled<T> {
  key: string
  tick: number
  data?: T
  error?: unknown
}

export function useLoad<T>(fetcher: () => Promise<T>, deps: unknown[]): Load<T> {
  const key = JSON.stringify(deps)
  const [tick, setTick] = useState(0)
  const [settled, setSettled] = useState<Settled<T>>({ key: '', tick: -1 })
  const fetch = useEffectEvent(fetcher)

  useEffect(() => {
    let live = true
    fetch().then(
      (data) => live && setSettled({ key, tick, data }),
      (error: unknown) => live && setSettled({ key, tick, error }),
    )
    return () => {
      live = false
    }
  }, [key, tick])

  const same = settled.key === key
  return {
    data: same ? settled.data : undefined,
    error: same ? settled.error : undefined,
    loading: !same || settled.tick !== tick,
    reload: () => setTick((n) => n + 1),
    set: (data) => setSettled((prev) => ({ ...prev, data, error: undefined })),
  }
}
