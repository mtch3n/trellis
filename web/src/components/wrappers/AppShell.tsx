import { useEffect, useRef, useState, type ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { Lamp } from '@/components/wrappers/Lamp'
import { ProjectSwitcher } from '@/components/wrappers/ProjectSwitcher'
import { useLiveStatus } from '@/lib/live-status'
import { ThemeToggle } from '@/components/wrappers/ThemeToggle'

export type Section = 'overview' | 'board' | 'knowledge'

export interface ProjectSummary {
  key: string
  board_count: number
  in_progress: number
  stale_leases: number
  recent_changes: number
  boards: { name: string; slug: string }[]
  columns: { name: string; card_count: number; is_done: boolean }[]
}

function cardCount(project: ProjectSummary) {
  return project.columns.reduce((total, column) => total + column.card_count, 0)
}

/**
 * The shell is four slots and stays four slots: the mark, the project scope,
 * the sections (overview, board, vault), and connection status.
 *
 * Project is a scope control rather than a destination, because PRODUCT.md
 * makes one project the working scope and switching a deliberate act. A
 * landing page listing every project would make it an ambient one.
 */
export function AppShell({
  projectKey,
  section,
  children,
}: {
  projectKey?: string
  section?: Section
  children: ReactNode
}) {
  const [projects, setProjects] = useState<ProjectSummary[]>([])
  const [reachable, setReachable] = useState(true)
  const { live } = useLiveStatus()
  const navigate = useNavigate()
  const location = useLocation()
  // No rule under the shell at rest. Once content scrolls beneath it, a
  // hairline appears so the sticky bar keeps an edge.
  const top = useRef<HTMLDivElement>(null)
  const [scrolled, setScrolled] = useState(false)

  useEffect(() => {
    const marker = top.current
    if (!marker) return
    const observer = new IntersectionObserver(([entry]) => setScrolled(!entry.isIntersecting))
    observer.observe(marker)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const signal = controller.signal
    fetch('/api/projects', { signal })
      .then((response) => (response.ok ? response.json() : Promise.reject(new Error(String(response.status)))))
      .then((data: ProjectSummary[]) => { setProjects(data); setReachable(true) })
      .catch(() => { if (!signal.aborted) setReachable(false) })
    return () => controller.abort()
  }, [location.pathname])

  // One reading of connection state, shown once. The board used to repeat it
  // as a banner, which said nothing the lamp had not already said.
  const status = !reachable
    ? { lamp: 'alarm' as const, label: 'offline', tone: 'text-danger', title: 'The daemon is not responding' }
    : live === 'disconnected'
      ? { lamp: 'held' as const, label: 'stale', tone: 'text-held', title: 'Live updates dropped. Showing the last state received.' }
      : live === 'connected'
        ? { lamp: 'live' as const, label: 'live', tone: 'text-muted-foreground', title: 'Receiving live updates' }
        : { lamp: 'idle' as const, label: 'idle', tone: 'text-muted-foreground', title: 'No live stream on this screen' }

  const current = projects.find((project) => project.key === projectKey)
  // Ordered the way the picker is read: where you are, then what is live,
  // then everything that was initialised and never written to.
  const ordered = [...projects].sort((a, b) => {
    if (a.key === projectKey) return -1
    if (b.key === projectKey) return 1
    return cardCount(b) - cardCount(a)
  })

  const switchProject = (key: string) => {
    const target = projects.find((project) => project.key === key)
    const board = target?.boards[0]?.slug
    // Switching keeps the section you are in, so comparing two projects'
    // boards or vaults is one step each.
    if (section === 'knowledge') navigate(`/p/${key}/knowledge`)
    else if (section === 'board' && board) navigate(`/p/${key}/b/${board}`)
    else navigate(`/p/${key}`)
  }

  const tab =
    'relative flex items-center px-4 text-sm text-muted-foreground transition-colors duration-150 hover:text-foreground ' +
    'after:absolute after:inset-x-4 after:bottom-2.5 after:h-0.5 after:scale-x-0 after:bg-foreground after:transition-transform after:duration-200 after:ease-settle ' +
    'aria-[current=page]:text-foreground aria-[current=page]:after:scale-x-100'

  return (
    <>
      <div ref={top} aria-hidden="true" className="pointer-events-none absolute inset-x-0 top-0 h-px" />
      <div
        data-scrolled={scrolled || undefined}
        className="sticky top-0 z-20 flex h-shell items-stretch gap-6 border-b border-transparent bg-background px-6 transition-colors duration-200 data-scrolled:border-border lg:px-8"
      >
        <Link to="/" className="flex items-center text-sm font-semibold text-foreground">
          Trellis
        </Link>

        {projectKey && (
          <div className="flex items-center">
            <ProjectSwitcher
              current={projectKey}
              projects={ordered.map((project) => ({
                key: project.key,
                cards: cardCount(project),
                stale: project.stale_leases,
              }))}
              onSwitch={switchProject}
              onAll={() => navigate('/projects')}
            />
          </div>
        )}

        <nav className="-ml-2 flex">
          <Link
            to={projectKey ? `/p/${projectKey}` : '/'}
            aria-current={section === 'overview' ? 'page' : undefined}
            className={tab}
          >
            Overview
          </Link>
          <Link
            to={current?.boards[0] ? `/p/${current.key}/b/${current.boards[0].slug}` : '/'}
            aria-current={section === 'board' ? 'page' : undefined}
            className={tab}
          >
            Board
          </Link>
          <Link
            to={projectKey ? `/p/${projectKey}/knowledge` : '/'}
            aria-current={section === 'knowledge' ? 'page' : undefined}
            className={tab}
          >
            Vault
          </Link>
        </nav>

        <div className="ml-auto flex items-center gap-3">
          <span
            className={cn('flex items-center gap-2 text-xs', status.tone)}
            title={status.title}
          >
            <Lamp state={status.lamp} label={status.label} />
            {status.label}
          </span>
          <ThemeToggle />
        </div>
      </div>
      {children}
    </>
  )
}
