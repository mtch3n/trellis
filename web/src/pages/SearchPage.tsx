import { useState, type FormEvent } from 'react'
import { Search } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { sentence } from '@/lib/format'
import { Paged } from '@/components/wrappers/Paged'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { readError } from '@/lib/api'

interface SearchHit {
  kind: string
  ref: string
  title: string
  project: string
  detail: string
  unreviewed?: boolean
}

/** Search reaches across both halves of the product, cards and knowledge alike. */
export function SearchPage() {
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<SearchHit[]>([])
  const [searched, setSearched] = useState(false)
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const search = async (event: FormEvent) => {
    event.preventDefault()
    const term = query.trim()
    if (!term) return
    setSearching(true)
    setError(null)
    try {
      const response = await fetch(`/api/search?q=${encodeURIComponent(term)}`)
      if (!response.ok) throw new Error(await readError(response))
      setHits(await response.json())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Search failed')
      setHits([])
    } finally {
      setSearching(false)
      setSearched(true)
    }
  }

  return (
    <main className="px-6 pb-24 lg:px-8">
      <PageHeader title="Search" />

      <form onSubmit={search} className="mt-4 flex max-w-2xl gap-2">
        <InputGroup>
          <InputGroupAddon>
            {searching ? <Spinner /> : <Search />}
          </InputGroupAddon>
          <InputGroupInput
            aria-label="Search cards and knowledge"
            placeholder="Cards and knowledge, across every project"
            value={query}
            autoFocus
            onChange={(event) => setQuery(event.target.value)}
          />
        </InputGroup>
        <Button type="submit" disabled={searching || !query.trim()}>
          Search
        </Button>
      </form>

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Search failed</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {searching && (
        <div className="mt-8 flex flex-col gap-2">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
        </div>
      )}

      {!searching && searched && hits.length === 0 && !error && (
        <Empty className="mt-16">
          <EmptyHeader>
            <EmptyTitle>No match</EmptyTitle>
            <EmptyDescription>Nothing in any project matches that.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!searching && hits.length > 0 && (
        <section className="mt-10">
          <h2 className="flex items-baseline gap-3 text-heading">
            Results
            <span className="text-xs font-normal text-muted-foreground">{hits.length}</span>
          </h2>
          <Paged items={hits} label="Result pages">
            {(page) => (
          <Table className="mt-2">
            <TableHeader>
              <TableRow>
                <TableHead className="w-28">Kind</TableHead>
                <TableHead className="w-36">Project</TableHead>
                <TableHead>Match</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {page.map((hit) => (
                <TableRow key={`${hit.project}-${hit.kind}-${hit.ref}`}>
                  <TableCell className="text-xs text-muted-foreground">{sentence(hit.kind)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{hit.project}</TableCell>
                  <TableCell>
                    <span>{hit.title}</span>
                    {hit.detail && (
                      <span className="mt-1 block max-w-measure text-xs whitespace-normal text-muted-foreground">{sentence(hit.detail)}</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
            )}
          </Paged>
        </section>
      )}
    </main>
  )
}
