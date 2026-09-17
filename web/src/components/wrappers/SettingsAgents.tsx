import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { Lamp } from '@/components/wrappers/Lamp'
import { readError } from '@/lib/api'
import { useNow } from '@/lib/clock'
import { ago } from '@/lib/format'

/** One card an agent claims. */
interface ClaimedCard {
  ref: string
  title: string
  project: string
  claim_until?: number
  expired: boolean
}

interface Agent {
  id: string
  handle: string
  kind: string
  cwd: string
  host: string
  pid: number
  first_seen: number
  last_seen: number
  claimed: ClaimedCard[]
}

/** Quiet for five minutes is what contention triage treats as idle. */
const QUIET_MS = 5 * 60 * 1000

/**
 * Who is working, and what they hold. This is the triage view for a contended
 * card: an agent that has been quiet for a while is one whose claim can be
 * taken, and an expired claim needs no taking at all.
 *
 * An actor with a claim but no agent row still appears, because the claim is
 * what holds the card — an unregistered writer would otherwise leave a card
 * claimed by nobody this page can name.
 */
export function SettingsAgents() {
  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch('/api/agents', { signal })
      if (!response.ok) throw new Error(await readError(response))
      setAgents((await response.json()) as Agent[])
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not read the agents')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  // Quiet is measured against a clock, so the page watches it rather than
  // reading it once while rendering.
  const now = useNow()

  return (
    <>
      <PageHeader
        title="Agents"
        description="Who has been working here, and what they claim. Trellis records an agent when it registers; the claim is what holds a card."
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not read the agents</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {agents === null && !error && <Skeleton className="mt-6 h-40 w-full" />}

      {agents?.length === 0 && (
        <Empty className="mt-10">
          <EmptyHeader>
            <EmptyTitle>No agents yet</EmptyTitle>
            <EmptyDescription>
              An agent registers itself at the start of a session. Until one does, this is empty
              and nothing is claimed.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {agents && agents.length > 0 && (
        <Table className="mt-6">
          <TableHeader>
            <TableRow>
              <TableHead className="w-7" aria-label="State" />
              <TableHead>Agent</TableHead>
              <TableHead className="w-28">Last seen</TableHead>
              <TableHead>Claims</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map((agent) => {
              const quiet = now - agent.last_seen > QUIET_MS
              return (
                <TableRow key={agent.id}>
                  <TableCell>
                    <Lamp state={agent.claimed.length > 0 ? 'claimed' : 'idle'} />
                  </TableCell>
                  <TableCell>
                    <span className="block text-sm">{agent.handle}</span>
                    <span className="block text-meta text-muted-foreground">
                      {agent.kind}{agent.host ? ` · ${agent.host}` : ''}{agent.pid ? ` · pid ${agent.pid}` : ''}
                    </span>
                    {agent.cwd && <span className="block text-meta text-muted-foreground">{agent.cwd}</span>}
                  </TableCell>
                  <TableCell className={quiet ? 'text-xs text-muted-foreground' : 'text-xs'}>
                    {agent.last_seen ? ago(agent.last_seen, now) : 'never'}
                  </TableCell>
                  <TableCell>
                    {agent.claimed.length === 0 ? (
                      <span className="text-xs text-muted-foreground">Nothing</span>
                    ) : (
                      <ul className="flex flex-col gap-1">
                        {agent.claimed.map((card) => (
                          <li key={card.ref} className="flex flex-wrap items-baseline gap-2">
                            <Link
                              to={`/p/${card.project}/card/${encodeURIComponent(card.ref)}`}
                              className="text-meta hover:underline"
                            >
                              {card.ref}
                            </Link>
                            <span className="min-w-0 truncate text-sm">{card.title}</span>
                            {card.expired ? (
                              <Badge variant="outline">Expired</Badge>
                            ) : (
                              card.claim_until && (
                                <span className="text-xs text-muted-foreground">
                                  until {new Date(card.claim_until).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}
                                </span>
                              )
                            )}
                          </li>
                        ))}
                      </ul>
                    )}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </>
  )
}
