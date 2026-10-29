import { useEffect, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, errorText, loginPath, setCsrf, type Me } from '@/lib/api'
import { SessionContext } from '@/lib/session'
import { Spinner } from '@/components/ui/spinner'

export function RequireSession({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [failure, setFailure] = useState('')
  const navigate = useNavigate()

  useEffect(() => {
    api.me().then(
      (value) => {
        setCsrf(value.csrf)
        setMe(value)
      },
      (err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          navigate(loginPath(window.location.pathname + window.location.search), { replace: true })
          return
        }
        setFailure(errorText(err))
      },
    )
  }, [navigate])

  if (failure) return <p className="p-8 text-sm text-danger">{failure}</p>
  if (!me) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Spinner />
      </div>
    )
  }
  return <SessionContext.Provider value={me}>{children}</SessionContext.Provider>
}
