import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

interface SearchHit { kind: string; ref: string; title: string; project: string; detail: string; unreviewed?: boolean }

export function SearchPage() {
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<SearchHit[]>([])
  const [searched, setSearched] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const search = async (event: FormEvent) => {
    event.preventDefault()
    if (!query.trim()) return
    const response = await fetch(`/api/search?q=${encodeURIComponent(query.trim())}`)
    if (!response.ok) { setError(await response.text()); return }
    setHits(await response.json()); setSearched(true); setError(null)
  }

  return <main className="mx-auto min-h-svh max-w-5xl px-5 py-8 sm:px-8"><Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Projects</Link><header className="mb-8 mt-6"><p className="font-mono text-xs uppercase tracking-wide text-muted-foreground">Workspace search</p><h1 className="mt-2 text-3xl font-semibold tracking-tight text-foreground">Find work and knowledge</h1></header><form onSubmit={search} className="flex gap-2"><Input aria-label="Search Trellis" placeholder="Search cards and knowledge" value={query} onChange={(event) => setQuery(event.target.value)} /><Button type="submit" disabled={!query.trim()}><Search className="size-4" />Search</Button></form>{error && <p className="mt-5 text-sm text-destructive">{error}</p>}{searched && <div className="mt-6 space-y-3">{hits.length === 0 ? <p className="text-sm text-muted-foreground">No results.</p> : hits.map((hit) => <Card key={`${hit.kind}-${hit.project}-${hit.ref}`}><CardContent className="p-4"><p className="font-mono text-xs text-muted-foreground">{hit.project} · {hit.ref} · {hit.detail}</p><h2 className="mt-1 font-medium text-foreground">{hit.title}{hit.unreviewed && <span className="ml-2 text-xs text-amber-600">unreviewed</span>}</h2></CardContent></Card>)}</div>}</main>
}
