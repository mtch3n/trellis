import { Link } from 'react-router-dom'
import { api, errorText } from '@/lib/api'
import { useLoad } from '@/lib/use-load'
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/wrappers/PageHeader'

export function ProjectsPage() {
  const projects = useLoad(() => api.projects(), [])

  return (
    <>
      <PageHeader title="Projects" />
      {projects.loading && !projects.data && <Spinner />}
      {Boolean(projects.error) && <p className="text-sm text-danger">{errorText(projects.error)}</p>}
      {projects.data?.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>No projects</EmptyTitle>
            <EmptyDescription>An admin adds you to a project with <code>trellis admin project add-member</code>.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {projects.data?.map((project) => (
          <Link key={project.id} to={`/p/${encodeURIComponent(project.key)}/board`}>
            <Card className="transition-colors hover:bg-muted/50">
              <CardHeader>
                <CardTitle className="font-mono">{project.key}</CardTitle>
                <CardDescription>{project.name || 'No name'}</CardDescription>
              </CardHeader>
            </Card>
          </Link>
        ))}
      </div>
    </>
  )
}
