import { useState, type FormEvent } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, errorText, vaultPath } from '@/lib/api'
import { useLoad } from '@/lib/use-load'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/wrappers/PageHeader'

export function SearchPage() {
  const { projectKey = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const [text, setText] = useState(q)
  const hits = useLoad(() => (q ? api.search(projectKey, q) : Promise.resolve([])), [projectKey, q])

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setParams(text.trim() ? { q: text.trim() } : {})
  }

  return (
    <>
      <PageHeader title="Search the vault" />
      <form onSubmit={submit} className="mb-6 flex gap-2">
        <Input value={text} onChange={(event) => setText(event.target.value)} placeholder="Search entries" aria-label="Search" autoFocus />
        <Button type="submit">Search</Button>
      </form>
      {hits.loading && !hits.data && <Spinner />}
      {Boolean(hits.error) && <p className="text-sm text-danger">{errorText(hits.error)}</p>}
      {q && hits.data?.length === 0 && <p className="text-sm text-muted-foreground">Nothing matches “{q}”.</p>}
      <ul className="flex flex-col gap-3">
        {hits.data?.map((hit) => (
          <li key={`${hit.scope}:${hit.slug}`} className="rounded-lg border p-3">
            <div className="flex items-center gap-2">
              <Link to={vaultPath(projectKey, hit.slug)} className="font-medium hover:underline">{hit.title}</Link>
              <Badge variant="secondary">{hit.scope}</Badge>
              <span className="ml-auto text-xs text-muted-foreground">{hit.owner}</span>
            </div>
            {hit.summary && <p className="mt-1 text-sm text-muted-foreground">{hit.summary}</p>}
          </li>
        ))}
      </ul>
    </>
  )
}
