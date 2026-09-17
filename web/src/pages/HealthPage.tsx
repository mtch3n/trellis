import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { LifecycleDialog, type Lifecycle } from '@/components/wrappers/LifecycleDialog'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { Paged } from '@/components/wrappers/Paged'
import { readError } from '@/lib/api'
import { ago, sentence, templateLabel } from '@/lib/format'
import { shortActor } from '@/lib/cards'
import { vaultActions } from '@/lib/vault-actions'
import type { Entry } from '@/lib/entry'

interface HealthLine { what: string; count: number; fix: string }
interface Diagnostic { kind: string; entry: string; ref?: string; fix: string }
interface DuplicateCluster { slugs: string[]; shared_terms: string }
interface VaultHealth {
  health: HealthLine[]
  diagnostics: Diagnostic[]
  duplicates: DuplicateCluster[]
  cold: Entry[]
}

interface Nomination {
  slug: string
  title: string
  actor: string
  reason: string
  cited: number
  pinned: number
  reads_30d: number
  actors: number
  nominations: number
  created_at: number
}

interface Uptake { provenance: string; injected: number; opened: number }

/** The slug inside an entry address (`/KEY/vault/ops/db` -> `ops/db`). */
function slugOf(address: string) {
  if (!address.startsWith('/')) return address
  const [, , , ...rest] = address.split('/')
  return rest.join('/')
}

/**
 * What the vault's state is, and what wants deciding about it: the counts and
 * the diagnostics behind them, the entries nothing reads any more, the
 * nominations agents have put forward, and whether recall is paying off.
 *
 * Detection is free and runs on demand; every action here is a person's, which
 * is why promoting asks for the entry's name and a reason (§10.10). Nothing on
 * this page tidies anything by itself.
 */
export function HealthPage() {
  const { projectKey } = useParams<{ projectKey: string }>()
  const [health, setHealth] = useState<VaultHealth | null>(null)
  const [nominations, setNominations] = useState<Nomination[]>([])
  const [uptake, setUptake] = useState<Uptake[]>([])
  const [error, setError] = useState<string | null>(null)
  const [promoting, setPromoting] = useState<string | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!projectKey) return
    const read = async <T,>(url: string, fallback: T): Promise<T> => {
      const response = await fetch(url, { signal })
      if (!response.ok) throw new Error(await readError(response))
      return ((await response.json()) as T) ?? fallback
    }
    try {
      const [state, queue, recall] = await Promise.all([
        read<VaultHealth>(`/api/p/${projectKey}/health`, { health: [], diagnostics: [], duplicates: [], cold: [] }),
        read<Nomination[]>(`/api/p/${projectKey}/nominations`, []),
        read<Uptake[]>(`/api/p/${projectKey}/uptake`, []),
      ])
      setHealth(state)
      setNominations(queue)
      setUptake(recall)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not read the vault')
    }
  }, [projectKey])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const actions = vaultActions({
    projectKey: projectKey ?? '',
    board: null,
    refresh: async () => { await load() },
  })

  const vault = `/p/${projectKey}/vault`

  return (
    <main className="mx-auto w-full max-w-5xl px-6 pb-24 lg:px-8">
      <PageHeader
        title="Vault health"
        description="What the vault looks like before anyone decides to tidy it. Nothing here changes on its own."
        facts={[
          { label: 'Diagnostics', value: health?.diagnostics.length ?? 0 },
          { label: 'Nominations', value: nominations.length },
        ]}
        actions={<Button variant="outline" size="sm" render={<Link to={vault} />} nativeButton={false}>Back to the vault</Button>}
      />

      {error && (
        <Alert variant="destructive" className="mt-4">
          <AlertTitle>Could not read the vault</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!health && !error && (
        <div className="mt-6 flex flex-col gap-3">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      )}

      {health && (
        <>
          <section className="mt-10">
            <h2 className="text-heading">Counts</h2>
            {health.health.length === 0 ? (
              <p className="mt-3 text-sm text-muted-foreground">Nothing to report.</p>
            ) : (
              <dl className="mt-3 flex flex-wrap gap-x-10 gap-y-3">
                {health.health.map((line) => (
                  <div key={line.what} className="flex flex-col gap-0.5">
                    <dt className="text-xs text-muted-foreground">{sentence(line.what)}</dt>
                    <dd className="flex items-baseline gap-2">
                      <span className="text-sm font-medium">{line.count}</span>
                      {line.count > 0 && line.fix && <span className="text-meta text-muted-foreground">{line.fix}</span>}
                    </dd>
                  </div>
                ))}
              </dl>
            )}
          </section>

          <section className="mt-10">
            <h2 className="flex items-baseline gap-3 text-heading">
              Diagnostics
              <span className="text-xs font-normal text-muted-foreground">{health.diagnostics.length}</span>
            </h2>
            {health.diagnostics.length === 0 ? (
              <p className="mt-3 text-sm text-muted-foreground">
                No stubs, orphans, broken anchors or template violations.
              </p>
            ) : (
              <Paged items={health.diagnostics} label="Diagnostic pages">
                {(page) => (
                  <Table className="mt-3">
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-44">What</TableHead>
                        <TableHead>Entry</TableHead>
                        <TableHead>What to do</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {page.map((diagnostic, index) => (
                        <TableRow key={`${diagnostic.kind}-${diagnostic.entry}-${diagnostic.ref ?? index}`}>
                          <TableCell className="text-xs text-muted-foreground">
                            {sentence(diagnostic.kind.replaceAll('_', ' '))}
                          </TableCell>
                          <TableCell>
                            <Link
                              to={`${vault}/${encodeURIComponent(slugOf(diagnostic.entry))}`}
                              className="text-meta hover:underline"
                            >
                              {slugOf(diagnostic.entry)}
                            </Link>
                            {diagnostic.ref && (
                              <span className="ml-2 text-xs text-muted-foreground">{diagnostic.ref}</span>
                            )}
                          </TableCell>
                          <TableCell className="text-meta text-muted-foreground">{diagnostic.fix}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </Paged>
            )}
          </section>

          {health.duplicates.length > 0 && (
            <section className="mt-10">
              <h2 className="flex items-baseline gap-3 text-heading">
                Possible duplicates
                <span className="text-xs font-normal text-muted-foreground">{health.duplicates.length}</span>
              </h2>
              <ul className="mt-3 flex flex-col gap-3">
                {health.duplicates.map((cluster) => (
                  <li key={cluster.slugs.join('|')} className="flex flex-col gap-1">
                    <span className="flex flex-wrap gap-x-3 gap-y-1">
                      {cluster.slugs.map((slug) => (
                        <Link key={slug} to={`${vault}/${encodeURIComponent(slug)}`} className="text-meta hover:underline">
                          {slug}
                        </Link>
                      ))}
                    </span>
                    <span className="text-xs text-muted-foreground">Share: {cluster.shared_terms}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          <section className="mt-10">
            <h2 className="flex items-baseline gap-3 text-heading">
              Cold entries
              <span className="text-xs font-normal text-muted-foreground">{health.cold.length}</span>
            </h2>
            {health.cold.length === 0 ? (
              <p className="mt-3 text-sm text-muted-foreground">Everything here has been read or cited lately.</p>
            ) : (
              <ul className="mt-3 flex flex-col gap-2">
                {health.cold.map((entry) => (
                  <li key={entry.id} className="flex flex-wrap items-baseline gap-x-3">
                    <Link to={`${vault}/${encodeURIComponent(entry.slug)}`} className="text-sm hover:underline">
                      {entry.title}
                    </Link>
                    <span className="text-meta text-muted-foreground">{entry.slug}</span>
                    <span className="text-xs text-muted-foreground">{templateLabel(entry.template)}</span>
                    {entry.updated_at && (
                      <span className="text-xs text-muted-foreground">edited {ago(entry.updated_at)}</span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}

      <section className="mt-10">
        <h2 className="flex items-baseline gap-3 text-heading">
          Nominations
          <span className="text-xs font-normal text-muted-foreground">{nominations.length}</span>
        </h2>
        {nominations.length === 0 ? (
          <Empty className="mt-2">
            <EmptyHeader>
              <EmptyTitle>Nothing nominated</EmptyTitle>
              <EmptyDescription>
                An agent nominates an entry it needed and could not get from its own project.
                Promoting one is a person's call, so it waits here.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table className="mt-3">
            <TableHeader>
              <TableRow>
                <TableHead>Entry</TableHead>
                <TableHead>Why</TableHead>
                <TableHead className="w-20">Cited</TableHead>
                <TableHead className="w-20">Reads</TableHead>
                <TableHead className="w-28" aria-label="Promote" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {nominations.map((nomination) => (
                <TableRow key={nomination.slug}>
                  <TableCell>
                    <Link to={`${vault}/${encodeURIComponent(nomination.slug)}`} className="text-sm hover:underline">
                      {nomination.title}
                    </Link>
                    <span className="mt-0.5 block text-meta text-muted-foreground">
                      {nomination.slug} · {shortActor(nomination.actor)}
                    </span>
                  </TableCell>
                  <TableCell className="text-sm whitespace-normal">{nomination.reason}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{nomination.cited}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{nomination.reads_30d}</TableCell>
                  <TableCell>
                    <Button variant="outline" size="sm" onClick={() => setPromoting(nomination.slug)}>
                      Promote
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>

      {uptake.length > 0 && (
        <section className="mt-10">
          <h2 className="text-heading">Recall uptake</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            How often an entry that was read into a session was then opened, by how the entry was written down.
          </p>
          <Table className="mt-3">
            <TableHeader>
              <TableRow>
                <TableHead>Written down as</TableHead>
                <TableHead className="w-28">Injected</TableHead>
                <TableHead className="w-28">Opened</TableHead>
                <TableHead className="w-24">Share</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {uptake.map((row) => (
                <TableRow key={row.provenance}>
                  <TableCell className="text-sm">{sentence(row.provenance || 'unrecorded')}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{row.injected}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{row.opened}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {row.injected === 0 ? '—' : `${Math.round((row.opened / row.injected) * 100)}%`}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
      )}

      {promoting && (
        <LifecycleDialog
          act={'promote' as Lifecycle}
          slug={promoting}
          onOpenChange={(open) => { if (!open) setPromoting(null) }}
          onConfirm={(_, values) => actions.promote(promoting, values.confirm, values.reason)}
        />
      )}
    </main>
  )
}
