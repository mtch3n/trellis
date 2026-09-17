import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import type { ProjectSummary } from '@/components/wrappers/AppShell'
import { readError } from '@/lib/api'

function score(project: ProjectSummary) {
  const cards = project.columns.reduce((total, column) => total + column.card_count, 0)
  return project.recent_changes * 1000 + cards
}

/**
 * One project is the working scope, so the root resolves to a project's
 * overview rather than to a list of every project on the machine. The busiest
 * project wins when the URL does not name one; switching is the shell's scope
 * control.
 */
export function RootRedirect() {
  const [projects, setProjects] = useState<ProjectSummary[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    const signal = controller.signal
    fetch('/api/projects', { signal })
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json()
      })
      .then(setProjects)
      .catch((err: unknown) => {
        if (err instanceof Error && err.name !== 'AbortError') setError(err.message)
      })
    return () => controller.abort()
  }, [])

  if (error) {
    return (
      <main className="mx-auto min-h-svh max-w-4xl px-6 py-10 lg:px-8">
        <Alert variant="destructive">
          <AlertTitle>Could not reach the daemon</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      </main>
    )
  }

  if (!projects) {
    return (
      <main className="mx-auto min-h-svh max-w-4xl px-6 py-10 lg:px-8">
        <div className="flex flex-col gap-2">
          <Skeleton className="h-9 w-64" />
          <Skeleton className="h-24 w-full" />
        </div>
      </main>
    )
  }

  const target = [...projects].sort((a, b) => score(b) - score(a))[0]

  if (!target) {
    return (
      <Empty className="min-h-svh">
        <EmptyHeader>
          <EmptyTitle>No project yet</EmptyTitle>
          <EmptyDescription>Run trellis init in a repository to create one.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return <Navigate to={`/p/${target.key}`} replace />
}
