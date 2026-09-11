import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, ArrowUpRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

interface Board { name: string; slug: string }

export function ProjectPage() {
  const { projectKey } = useParams<{ projectKey: string }>()
  const [boards, setBoards] = useState<Board[]>([])
  const [error, setError] = useState<string | null>(null)
  useEffect(() => { fetch(`/api/p/${projectKey}/boards`).then(async (response) => { if (!response.ok) throw new Error(await response.text()); return response.json() }).then(setBoards).catch((err: unknown) => setError(err instanceof Error ? err.message : 'Could not load project')) }, [projectKey])
  return <main className="mx-auto min-h-svh max-w-4xl px-5 py-8 sm:px-8"><Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Projects</Link><header className="mb-8 mt-6"><p className="font-mono text-xs uppercase tracking-wide text-muted-foreground">Project</p><h1 className="mt-2 text-3xl font-semibold tracking-tight text-foreground">{projectKey}</h1></header>{error && <p className="text-sm text-destructive">{error}</p>}<div className="grid gap-4 sm:grid-cols-2">{boards.map((board) => <Card key={board.slug}><CardContent className="flex items-center justify-between p-5"><div><h2 className="font-medium text-foreground">{board.name}</h2><p className="mt-1 font-mono text-xs text-muted-foreground">{board.slug}</p></div><div className="flex gap-2"><Link to={`/p/${projectKey}/b/${board.slug}`}><Button size="sm">Open<ArrowUpRight className="size-4" /></Button></Link><Link to={`/p/${projectKey}/b/${board.slug}/kb`}><Button size="sm" variant="outline">Knowledge</Button></Link></div></CardContent></Card>)}</div></main>
}
