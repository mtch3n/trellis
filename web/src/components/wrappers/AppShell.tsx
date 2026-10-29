import type { ReactNode } from 'react'
import { NavLink, useLocation, useNavigate, useParams } from 'react-router-dom'
import { LogOut } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import { useSession } from '@/lib/session'
import { IconButton } from '@/components/wrappers/IconButton'
import { ThemeToggle } from '@/components/wrappers/ThemeToggle'
import { TrellisMark } from '@/components/wrappers/TrellisMark'

const SECTIONS: { path: string; label: string; also: string[] }[] = [
  { path: 'board', label: 'Board', also: ['b', 'card'] },
  { path: 'vault', label: 'Vault', also: [] },
  { path: 'search', label: 'Search', also: [] },
  { path: 'review', label: 'Review', also: [] },
]

function navClass(isActive: boolean) {
  return cn(
    'rounded-md px-2.5 py-1 text-sm text-muted-foreground transition-colors hover:text-foreground',
    isActive && 'bg-muted text-foreground',
  )
}

export function AppShell({ children }: { children: ReactNode }) {
  const { projectKey } = useParams<{ projectKey: string }>()
  const me = useSession()
  const navigate = useNavigate()
  const section = useLocation().pathname.split('/')[3] ?? ''

  const logout = async () => {
    await api.logout().catch(() => undefined)
    navigate('/login', { replace: true })
  }

  return (
    <div className="flex min-h-svh flex-col">
      <header className="flex h-12 shrink-0 items-center gap-4 border-b px-4">
        <NavLink to="/projects" className="flex items-center gap-2 font-medium" aria-label="Projects">
          <TrellisMark className="size-5" />
          <span>Trellis</span>
        </NavLink>
        {projectKey && (
          <>
            <NavLink to="/projects" className="font-mono text-sm text-muted-foreground hover:text-foreground">
              {projectKey}
            </NavLink>
            <nav className="flex items-center gap-1" aria-label="Sections">
              {SECTIONS.map((item) => (
                <NavLink
                  key={item.path}
                  to={`/p/${encodeURIComponent(projectKey)}/${item.path}`}
                  className={navClass(section === item.path || item.also.includes(section))}
                >
                  {item.label}
                </NavLink>
              ))}
            </nav>
          </>
        )}
        <div className="ml-auto flex items-center gap-2">
          <span className="text-sm text-muted-foreground">
            {me.handle}
            <span className="text-muted-foreground/60"> · {me.org}</span>
          </span>
          <ThemeToggle />
          <IconButton label="Sign out" onClick={logout}>
            <LogOut />
          </IconButton>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-6xl min-w-0 flex-1 flex-col px-4 pb-10">{children}</main>
    </div>
  )
}
