import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Search } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
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
  unverified?: boolean
}

/** Every project, and "everywhere", which is what a discovery search wants. */
const EVERYWHERE = 'all'

/** How many hits to ask for. */
const LIMITS = [20, 50, 100, 200]

/**
 * Search reaches across both halves of the product, cards and vault entries
 * alike, and by default across every project, because not knowing which
 * project holds the answer is the reason to search.
 *
 * The options are the CLI's: which method (the words as written, the meaning,
 * or both), which project, which label, and how many hits. Method and label
 * only appear where they mean something — vector search has to be configured,
 * and a label belongs to one project.
 */
export function SearchPage() {
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<SearchHit[]>([])
  const [searched, setSearched] = useState(false)
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [method, setMethod] = useState('default')
  const [project, setProject] = useState(EVERYWHERE)
  const [label, setLabel] = useState(EVERYWHERE)
  const [limit, setLimit] = useState('50')
  const [projects, setProjects] = useState<string[]>([])
  const [labels, setLabels] = useState<string[]>([])

  // The projects to scope to. A key is a name here, so it is shown as one.
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/projects', { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : []))
      .then((data: { key: string }[]) => setProjects(data.map((item) => item.key)))
      .catch(() => { /* the scope control simply stays at everywhere */ })
    return () => controller.abort()
  }, [])

  // A label belongs to one project, so the filter follows the scope.
  useEffect(() => {
    setLabel(EVERYWHERE)
    if (project === EVERYWHERE) { setLabels([]); return }
    const controller = new AbortController()
    fetch(`/api/p/${project}/labels`, { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : []))
      .then((data: { name: string }[]) => setLabels(data.map((item) => item.name)))
      .catch(() => { setLabels([]) })
    return () => controller.abort()
  }, [project])

  const search = async (event: FormEvent) => {
    event.preventDefault()
    const term = query.trim()
    if (!term) return
    setSearching(true)
    setError(null)
    try {
      const params = new URLSearchParams({ q: term, limit })
      if (method !== 'default') params.set('method', method)
      if (project !== EVERYWHERE) params.set('project', project)
      if (label !== EVERYWHERE) params.set('label', label)
      const response = await fetch(`/api/search?${params}`)
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

      <form onSubmit={search} className="mt-4 flex max-w-measure gap-2">
        <InputGroup>
          <InputGroupAddon>
            {searching ? <Spinner /> : <Search />}
          </InputGroupAddon>
          <InputGroupInput
            aria-label="Search cards and entries"
            placeholder="Cards and entries, across every project"
            value={query}
            autoFocus
            onChange={(event) => setQuery(event.target.value)}
          />
        </InputGroup>
        <Button type="submit" disabled={searching || !query.trim()}>
          Search
        </Button>
      </form>

      {/* The options, under the field they change: what to search, where, and
          how much of it to bring back. */}
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <SearchOption
          label="Method"
          value={method}
          onChange={setMethod}
          options={[
            { value: 'default', label: 'As configured' },
            { value: 'fts', label: 'The words as written' },
            { value: 'vector', label: 'By meaning' },
            { value: 'hybrid', label: 'Both' },
          ]}
        />
        <SearchOption
          label="Project"
          value={project}
          onChange={setProject}
          options={[{ value: EVERYWHERE, label: 'Every project' }, ...projects.map((key) => ({ value: key, label: key }))]}
        />
        {labels.length > 0 && (
          <SearchOption
            label="Label"
            value={label}
            onChange={setLabel}
            options={[{ value: EVERYWHERE, label: 'Any label' }, ...labels.map((name) => ({ value: name, label: name }))]}
          />
        )}
        <SearchOption
          label="Hits"
          value={limit}
          onChange={setLimit}
          options={LIMITS.map((count) => ({ value: String(count), label: `Up to ${count}` }))}
        />
      </div>

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
          <Table className="mt-3">
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
                    <Link to={pathOf(hit)} className="hover:underline">{hit.title}</Link>
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

/**
 * A hit's path inside the app. A search crosses projects, so the project is
 * part of the answer and therefore part of the link; a ref with no project is
 * this search's own scope.
 */
function pathOf(hit: SearchHit) {
  const key = hit.project
  if (hit.ref.startsWith('/')) {
    const [, project, , ...rest] = hit.ref.split('/')
    return `/p/${project}/vault/${encodeURIComponent(rest.join('/'))}`
  }
  if (hit.kind === 'entry') return `/p/${key}/vault/${encodeURIComponent(hit.ref)}`
  return `/p/${key}/card/${encodeURIComponent(hit.ref)}`
}

/** One search option: a label a reader can scan, then the control. */
function SearchOption({ label, value, options, onChange }: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
}) {
  return (
    <Select items={options} value={value} onValueChange={(next) => { if (next) onChange(next) }}>
      <SelectTrigger size="sm" aria-label={label} className="w-44">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}
