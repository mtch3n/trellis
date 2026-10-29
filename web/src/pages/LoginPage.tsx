import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, errorText } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { TrellisMark } from '@/components/wrappers/TrellisMark'

export function LoginPage() {
  const [token, setToken] = useState('')
  const [failure, setFailure] = useState('')
  const [busy, setBusy] = useState(false)
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const next = params.get('next') ?? ''

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setFailure('')
    try {
      await api.login(token.trim())
      navigate(next.startsWith('/') && !next.startsWith('//') ? next : '/projects', { replace: true })
    } catch (err) {
      setFailure(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <TrellisMark className="mb-2 size-8" />
          <CardTitle>Sign in to Trellis</CardTitle>
          <CardDescription>
            Paste a person token. An admin issues one with <code>trellis admin token issue --kind person</code>.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="token">Person token</Label>
              <Textarea
                id="token"
                value={token}
                onChange={(event) => setToken(event.target.value)}
                autoComplete="off"
                spellCheck={false}
                className="font-mono"
                autoFocus
              />
            </div>
            {failure && <p className="text-sm text-danger" role="alert">{failure}</p>}
            <Button type="submit" disabled={busy || !token.trim()}>
              {busy ? 'Signing in…' : 'Sign in'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
