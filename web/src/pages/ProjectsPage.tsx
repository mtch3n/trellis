import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Ellipsis, KanbanSquare, LayoutDashboard, Trash2 } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { toast } from '@/components/ui/toast'
import type { ProjectSummary } from '@/components/wrappers/AppShell'
import { DeleteProjectDialog } from '@/components/wrappers/DeleteProjectDialog'
import { Lamp } from '@/components/wrappers/Lamp'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { Paged } from '@/components/wrappers/Paged'

function cards(project: ProjectSummary) {
  return project.columns.reduce((total, column) => total + column.card_count, 0)
}

function column(project: ProjectSummary, name: string) {
  return project.columns.find((item) => item.name === name)?.card_count ?? 0
}

/**
 * Every project on this machine, and the one place a project is deleted.
 * Deleting is kept off the switcher on purpose: it is a management act, not
 * something to reach while moving between projects.
 */
export function ProjectsPage() {
  const [projects, setProjects] = useState<ProjectSummary[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<ProjectSummary | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch('/api/projects', { signal })
      if (!response.ok) throw new Error(await response.text())
      setProjects(await response.json())
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not load projects')
      setProjects([])
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  if (!projects) {
    return (
      <main className="px-6 pb-24 lg:px-8">
        <div className="flex min-h-16 items-center py-4">
          <Skeleton className="h-7 w-44" />
        </div>
        <div className="mt-10 flex flex-col gap-3">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      </main>
    )
  }

  const live = projects.filter((project) => cards(project) > 0).sort((a, b) => cards(b) - cards(a))
  const untouched = projects.filter((project) => cards(project) === 0)
  const stale = projects.reduce((total, project) => total + project.stale_leases, 0)

  return (
    <main className="px-6 pb-24 lg:px-8">
      <PageHeader
        title="Projects"
        facts={[
          { label: 'Stale leases', value: stale, tone: 'held' },
          { label: 'Projects', value: projects.length },
        ]}
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not load projects</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Group title="Live" projects={live} empty="No project has a card yet." onDelete={setDeleting} />
      <Group title="Initialised, never used" projects={untouched} empty="Every project has been written to." onDelete={setDeleting} />

      <DeleteProjectDialog
        projectKey={deleting?.key ?? null}
        cards={deleting ? cards(deleting) : 0}
        onOpenChange={(open) => { if (!open) setDeleting(null) }}
        onDeleted={(key) => {
          setDeleting(null)
          toast.add({ title: `Deleted ${key}`, type: 'success' })
          void load()
        }}
      />
    </main>
  )
}

function Group({
  title,
  projects,
  empty,
  onDelete,
}: {
  title: string
  projects: ProjectSummary[]
  empty: string
  onDelete: (project: ProjectSummary) => void
}) {
  return (
    <section className="mt-10">
      <h2 className="flex items-baseline gap-3 text-heading">
        {title}
        <span className="text-xs font-normal text-muted-foreground">{projects.length}</span>
      </h2>

      {projects.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">{empty}</p>
      ) : (
        <Paged items={projects} label={`${title} pages`}>
          {(page) => (
            <Table className="mt-3">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-7" aria-label="State" />
                  <TableHead className="w-56">Project</TableHead>
                  <TableHead>Board</TableHead>
                  <TableHead className="w-24 text-right">Backlog</TableHead>
                  <TableHead className="w-24 text-right">Review</TableHead>
                  <TableHead className="w-20 text-right">Done</TableHead>
                  <TableHead className="w-36">State</TableHead>
                  <TableHead className="w-10" aria-label="Actions" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {page.map((project) => {
                  const board = project.boards[0]
                  return (
                    <TableRow key={project.key}>
                      <TableCell><Lamp state={project.stale_leases > 0 ? 'held' : 'idle'} /></TableCell>
                      <TableCell>
                        <Link to={`/p/${project.key}`} className="underline-offset-4 hover:underline">
                          {project.key}
                        </Link>
                      </TableCell>
                      <TableCell>
                        {board ? (
                          <Link to={`/p/${project.key}/b/${board.slug}`} className="underline-offset-4 hover:underline">
                            {board.name}
                          </Link>
                        ) : (
                          <span className="text-muted-foreground">No board</span>
                        )}
                      </TableCell>
                      <TableCell className="text-right text-muted-foreground">{column(project, 'backlog')}</TableCell>
                      <TableCell className="text-right text-muted-foreground">{column(project, 'review')}</TableCell>
                      <TableCell className="text-right text-muted-foreground">{column(project, 'done')}</TableCell>
                      <TableCell>
                        {project.stale_leases > 0 ? (
                          <span className="text-held">{project.stale_leases} stale</span>
                        ) : (
                          <span className="text-muted-foreground">{cards(project) === 0 ? 'Empty' : 'Clear'}</span>
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        <ProjectActions project={project} onDelete={() => onDelete(project)} />
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </Paged>
      )}
    </section>
  )
}

function ProjectActions({ project, onDelete }: { project: ProjectSummary; onDelete: () => void }) {
  const navigate = useNavigate()
  const board = project.boards[0]
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon-sm" aria-label={`Actions for ${project.key}`} />}
      >
        <Ellipsis />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuItem onClick={() => navigate(`/p/${project.key}`)}>
          <LayoutDashboard />
          Overview
        </DropdownMenuItem>
        {board && (
          <DropdownMenuItem onClick={() => navigate(`/p/${project.key}/b/${board.slug}`)}>
            <KanbanSquare />
            Board
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2 />
          Delete project
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
