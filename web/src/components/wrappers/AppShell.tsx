import { lazy, Suspense, useEffect, useRef, useState, type ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { ChevronsUpDown, MessageSquareText, Settings } from 'lucide-react'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Lamp } from '@/components/wrappers/Lamp'
import { GuardedLink } from '@/components/wrappers/NavigationGuard'
import { ProjectSwitcher } from '@/components/wrappers/ProjectSwitcher'
import { defaultBoard } from '@/lib/boards'
import { useLiveStatus } from '@/lib/live-status'
import { useNavigationGuard } from '@/lib/navigation-guard'
import { ThemeToggle } from '@/components/wrappers/ThemeToggle'
import { TrellisMark } from '@/components/wrappers/TrellisMark'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Kbd } from '@/components/ui/kbd'

const CHAT_OPEN_KEY = 'trellis.chat.open'

// The chat and the AI SDK behind it load the first time it opens.
const ChatPanel = lazy(() => import('@/components/wrappers/ChatPanel').then((module) => ({ default: module.ChatPanel })))

export type Section = 'overview' | 'board' | 'vault' | 'settings'

export interface ProjectSummary {
  key: string
  description: string
  board_count: number
  in_progress: number
  expired_claims: number
  recent_changes: number
  boards: { name: string; slug: string; is_default?: boolean }[]
  columns: { name: string; card_count: number; is_done: boolean }[]
}

function cardCount(project: ProjectSummary) {
  return project.columns.reduce((total, column) => total + column.card_count, 0)
}

/**
 * The shell is four slots and stays four slots: the mark, the project scope,
 * the sections (overview, board, vault), and connection status with the
 * machine-wide controls, settings last.
 *
 * Every way out of a screen goes through the navigation guard, so a screen
 * with unsaved changes can ask before it is left.
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
  const { guard } = useNavigationGuard()
  const location = useLocation()
  const { boardSlug } = useParams<{ projectKey: string; boardSlug?: string }>()
  // No rule under the shell at rest. Once content scrolls beneath it, a
  // hairline appears so the sticky bar keeps an edge.
  const top = useRef<HTMLDivElement>(null)
  const [scrolled, setScrolled] = useState(false)
  // The chat stays open or closed across pages and reloads, the way a
  // docked tool window does.
  const [chatOpen, setChatOpen] = useState(() => localStorage.getItem(CHAT_OPEN_KEY) === '1')
  // Once opened, the panel stays mounted, so closing it keeps its scroll and draft.
  const [chatLoaded, setChatLoaded] = useState(chatOpen)
  const [chatBusy, setChatBusy] = useState(false)
  if (chatOpen && !chatLoaded) setChatLoaded(true)
  const toggleChat = (next: boolean) => {
    setChatOpen(next)
    localStorage.setItem(CHAT_OPEN_KEY, next ? '1' : '0')
  }

  useEffect(() => {
    if (!projectKey) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === '.' && (event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey) {
        event.preventDefault()
        setChatOpen((open) => {
          localStorage.setItem(CHAT_OPEN_KEY, open ? '0' : '1')
          return !open
        })
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [projectKey])

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
  // as a banner, which said nothing the lamp had not already said. A screen
  // with no live stream shows no lamp at all: idle says nothing worth reading.
  const status = !reachable
    ? { lamp: 'alarm' as const, label: 'Offline', tone: 'text-danger', title: 'The daemon is not responding' }
    : live === 'disconnected'
      ? { lamp: 'claimed' as const, label: 'Stale', tone: 'text-claimed', title: 'Live updates dropped. Showing the last state received.' }
      : live === 'connected'
        ? { lamp: 'live' as const, label: 'Live', tone: 'text-muted-foreground', title: 'Receiving live updates' }
        : null

  const current = projects.find((project) => project.key === projectKey)
  // Ordered the way the picker is read: where you are, then what is live,
  // then everything that was initialised and never written to.
  const ordered = [...projects].sort((a, b) => {
    if (a.key === projectKey) return -1
    if (b.key === projectKey) return 1
    return cardCount(b) - cardCount(a)
  })

  // The board on screen, else the one the project opens on, else the first.
  // A slug in the URL that names no board falls through rather than emptying
  // the switcher.
  const pickBoard = (boards: ProjectSummary['boards']) =>
    (boardSlug ? boards.find((board) => board.slug === boardSlug) : undefined) ?? defaultBoard(boards)

  const switchProject = (key: string) => {
    const target = projects.find((project) => project.key === key)
    const board = pickBoard(target?.boards ?? [])
    // Switching keeps the section you are in, so comparing two projects'
    // boards or vaults is one step each.
    guard(() => {
      if (section === 'vault') navigate(`/p/${key}/vault`)
      else if (section === 'board' && board) navigate(`/p/${key}/b/${board.slug}`)
      else navigate(`/p/${key}`)
    })
  }

  const tab =
    'relative flex items-center px-2 text-sm text-muted-foreground transition-colors duration-150 hover:text-foreground sm:px-4 ' +
    'after:absolute after:inset-x-2 sm:after:inset-x-4 after:bottom-2.5 after:h-0.5 after:scale-x-0 after:bg-foreground after:transition-transform after:duration-200 after:ease-settle ' +
    'aria-[current=page]:text-foreground aria-[current=page]:after:scale-x-100'

  const selectedBoard = current ? pickBoard(current.boards) : undefined

  return (
    <>
      <div ref={top} aria-hidden="true" className="pointer-events-none absolute inset-x-0 top-0 h-px" />
      <div
        data-scrolled={scrolled || undefined}
        className="sticky top-0 z-20 flex h-shell items-stretch gap-2 border-b border-transparent bg-background px-4 transition-colors duration-200 data-scrolled:border-border sm:gap-6 sm:px-6 lg:px-8"
      >
        {/* On a narrow screen the project scope stands in for the mark, so every control still fits. */}
        <GuardedLink
          to="/"
          className={cn('flex items-center gap-2 text-sm font-semibold text-foreground', projectKey && 'max-sm:hidden')}
        >
          <TrellisMark />
          Trellis
        </GuardedLink>

        {projectKey && (
          <div className="flex items-center gap-3">
            <ProjectSwitcher
              current={projectKey}
              projects={ordered.map((project) => ({
                key: project.key,
                description: project.description,
                cards: cardCount(project),
                expired: project.expired_claims,
              }))}
              onSwitch={switchProject}
              onAll={() => guard(() => navigate('/projects'))}
            />
            {current && current.boards.length > 1 && (
              <BoardSwitcher
                current={selectedBoard?.slug ?? ''}
                boards={current.boards}
                onSwitch={(slug) => guard(() => navigate(`/p/${current.key}/b/${slug}`))}
              />
            )}
          </div>
        )}

        <nav className="-ml-2 flex">
          <GuardedLink
            to={projectKey ? `/p/${projectKey}` : '/'}
            aria-current={section === 'overview' ? 'page' : undefined}
            className={tab}
          >
            Overview
          </GuardedLink>
          <GuardedLink
            to={selectedBoard ? `/p/${current?.key}/b/${selectedBoard.slug}` : '/'}
            aria-current={section === 'board' ? 'page' : undefined}
            className={tab}
          >
            Board
          </GuardedLink>
          <GuardedLink
            to={projectKey ? `/p/${projectKey}/vault` : '/'}
            aria-current={section === 'vault' ? 'page' : undefined}
            className={tab}
          >
            Vault
          </GuardedLink>
        </nav>

        <div className="ml-auto flex items-center gap-1 sm:gap-3">
          {status && (
            <span
              className={cn('flex items-center gap-2 text-xs', status.tone)}
              title={status.title}
            >
              <Lamp state={status.lamp} label={status.label} />
              {/* The lamp carries the label for screen readers; on a narrow screen it
                  also stands alone on screen, so the controls beside it fit. */}
              <span aria-hidden="true" className="max-sm:hidden">{status.label}</span>
            </span>
          )}
          {projectKey && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Chat"
                    aria-pressed={chatOpen}
                    className="relative aria-pressed:bg-muted aria-pressed:text-foreground"
                    onClick={() => toggleChat(!chatOpen)}
                  />
                }
              >
                <MessageSquareText />
                {/* A reply still arriving behind a closed panel. */}
                {chatBusy && !chatOpen && (
                  <span aria-hidden="true" className="lamp-live absolute top-1 right-1 size-1.5 rounded-full bg-foreground" />
                )}
              </TooltipTrigger>
              <TooltipContent side="bottom">Chat <Kbd>Ctrl .</Kbd></TooltipContent>
            </Tooltip>
          )}
          <ThemeToggle />
          {/* A link, so it is announced as one; the tooltip names it, as on every icon-only control. */}
          <Tooltip>
            <TooltipTrigger
              render={
                <GuardedLink
                  to="/settings"
                  aria-label="Settings"
                  aria-current={section === 'settings' ? 'page' : undefined}
                  className={cn(
                    buttonVariants({ variant: 'ghost', size: 'icon-sm' }),
                    'aria-[current=page]:bg-muted aria-[current=page]:text-foreground',
                  )}
                />
              }
            >
              <Settings />
            </TooltipTrigger>
            <TooltipContent side="bottom">Settings</TooltipContent>
          </Tooltip>
        </div>
      </div>
      {projectKey ? (
        <>
          {/* On a wide screen the page makes room, so nothing hides under the chat. */}
          <div className={cn(chatOpen && 'lg:pr-chat')}>{children}</div>
          {chatLoaded && (
            <Suspense fallback={null}>
              <ChatPanel projectKey={projectKey} open={chatOpen} onClose={() => toggleChat(false)} onBusyChange={setChatBusy} />
            </Suspense>
          )}
        </>
      ) : children}
    </>
  )
}

/**
 * Which board of this project is open, and a way to another. A project
 * usually has one, so this appears only when there are more; the tabs stay a
 * fixed set of sections, and the scope controls sit together on the left.
 */
function BoardSwitcher({
  current,
  boards,
  onSwitch,
}: {
  current: string
  boards: ProjectSummary['boards']
  onSwitch: (slug: string) => void
}) {
  const currentBoard = boards.find((board) => board.slug === current)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="sm" className="gap-2 px-2" aria-label={`Board ${current}. Switch board`} />}
      >
        <span className="truncate">{currentBoard?.name ?? current}</span>
        <ChevronsUpDown data-icon="inline-end" className="text-muted-foreground" />
      </DropdownMenuTrigger>

      <DropdownMenuContent sideOffset={6} className="w-60 p-1.5">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Boards</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={current} onValueChange={(slug: string) => { if (slug !== current) onSwitch(slug) }}>
            {boards.map((board) => (
              <DropdownMenuRadioItem key={board.slug} value={board.slug} className="gap-3 py-1.5" title={board.slug}>
                <span className="truncate">{board.name}</span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
