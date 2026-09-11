import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Activity, ArrowUpRight, Plus, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

interface BoardInfo { name: string; slug: string }
interface ColumnInfo { name: string; card_count: number; is_done: boolean }
interface ProjectInfo { key: string; name: string; board_count: number; in_progress: number; stale_leases: number; recent_changes: number; boards: BoardInfo[]; columns: ColumnInfo[] }

export function ProjectsPage() {
  const [projects, setProjects] = useState<ProjectInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [boardName, setBoardName] = useState('')
  const [creatingFor, setCreatingFor] = useState<string | null>(null)

  const loadProjects = async () => {
    try {
      const response = await fetch('/api/projects')
      if (!response.ok) throw new Error('Could not load projects')
      setProjects(await response.json())
      setError(null)
    } catch (err) { setError(err instanceof Error ? err.message : 'Unknown error') } finally { setLoading(false) }
  }
  useEffect(() => { void loadProjects() }, [])

  const createBoard = async (projectKey: string) => {
    if (!boardName.trim()) return
    setCreatingFor(projectKey)
    try {
      const response = await fetch(`/api/p/${projectKey}/boards`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: boardName.trim() }) })
      if (!response.ok) throw new Error('Could not create board')
      setBoardName('')
      await loadProjects()
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not create board') } finally { setCreatingFor(null) }
  }

  if (loading) return <PageFrame><LoadingRows /></PageFrame>
  if (error) return <PageFrame><ErrorState message={error} onRetry={() => void loadProjects()} /></PageFrame>
  if (projects.length === 0) return <PageFrame><EmptyState /></PageFrame>

  return <PageFrame>
    <header className="mb-10 flex flex-wrap items-end justify-between gap-6"><div><p className="mb-3 font-mono text-xs uppercase tracking-wide text-muted-foreground">Workspace atlas</p><h1 className="text-4xl font-semibold tracking-tight text-foreground">Projects</h1><p className="mt-3 max-w-xl text-base text-muted-foreground">A quiet view of the work moving through every repository.</p></div><div className="flex items-center gap-2"><Link to="/search"><Button variant="outline" size="sm"><Search className="size-4" />Search</Button></Link><Link to="/activity"><Button variant="outline" size="sm"><Activity className="size-4" />Activity</Button></Link><div className="hidden rounded-full border border-border bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground sm:block">{projects.length} project{projects.length === 1 ? '' : 's'}</div></div></header>
    <div className="grid gap-5 lg:grid-cols-2">{projects.map((project) => { const firstBoard = project.boards[0]; return <Card key={project.key} className="overflow-hidden border-border/80 shadow-none transition-colors hover:border-foreground/30"><CardHeader className="border-b border-border/70 bg-muted/20"><div className="flex items-start justify-between gap-4"><div><CardTitle className="text-xl">{project.name}</CardTitle><CardDescription className="mt-1 font-mono text-xs">{project.key} · {project.board_count} board{project.board_count === 1 ? '' : 's'}</CardDescription></div>{firstBoard && <Link to={`/p/${project.key}/b/${firstBoard.slug}`} aria-label={`Open ${project.name}`}><ArrowUpRight className="size-5 text-muted-foreground transition-colors hover:text-foreground" /></Link>}</div></CardHeader><CardContent className="space-y-5 p-6"><div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-4">{project.columns.map((column) => <div key={column.name} className="bg-background px-3 py-4 text-center"><div className="text-2xl font-semibold tracking-tight text-foreground">{column.card_count}</div><div className="mt-1 truncate text-xs text-muted-foreground">{column.name}</div></div>)}</div><div className="grid grid-cols-3 gap-3 text-xs"><div><div className="font-mono text-base font-semibold text-foreground">{project.in_progress}</div><div className="text-muted-foreground">in progress</div></div><div><div className="font-mono text-base font-semibold text-foreground">{project.stale_leases}</div><div className="text-muted-foreground">stale leases</div></div><div><div className="font-mono text-base font-semibold text-foreground">{project.recent_changes}</div><div className="text-muted-foreground">changes / 24h</div></div></div><div className="flex flex-wrap gap-2">{project.boards.map((board) => <Link key={board.slug} to={`/p/${project.key}/b/${board.slug}`}><Button variant="outline" size="sm">{board.name}</Button></Link>)}</div><div className="flex items-center gap-2 border-t border-border/70 pt-4"><Input aria-label={`New board for ${project.name}`} placeholder="New board name" value={creatingFor === project.key ? boardName : ''} onChange={(event) => { setCreatingFor(project.key); setBoardName(event.target.value) }} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void createBoard(project.key) } }} /><Button size="sm" variant="secondary" disabled={creatingFor === project.key && !boardName.trim()} onClick={() => void createBoard(project.key)}><Plus className="size-4" />Add</Button></div></CardContent></Card> })}</div>
  </PageFrame>
}

function PageFrame({ children }: { children: ReactNode }) { return <main className="mx-auto min-h-svh w-full max-w-7xl px-5 py-10 sm:px-8 lg:px-10">{children}</main> }
function LoadingRows() { return <div className="space-y-4" aria-label="Loading projects"><div className="h-10 w-48 animate-pulse rounded-lg bg-muted" /><div className="h-52 animate-pulse rounded-lg bg-muted" /></div> }
function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) { return <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-6"><h1 className="font-semibold text-foreground">Could not load Trellis</h1><p className="mt-2 text-sm text-muted-foreground">{message}</p><Button className="mt-4" variant="outline" onClick={onRetry}>Retry</Button></div> }
function EmptyState() { return <div className="grid min-h-svh place-items-center"><div className="max-w-md text-center"><p className="font-mono text-xs uppercase tracking-wide text-muted-foreground">No signal yet</p><h1 className="mt-3 text-3xl font-semibold tracking-tight text-foreground">Create a project from the CLI</h1><p className="mt-3 text-muted-foreground">Run Trellis inside a Git repository, then return here for the board view.</p><code className="mt-6 inline-flex rounded-lg bg-muted px-3 py-2 text-sm text-foreground">trellis init</code></div></div> }
