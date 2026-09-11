import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, BookOpen } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'

interface Doc { id: string; slug: string; title: string; summary?: string; board?: string }

export function ProjectKnowledgePage() {
  const { projectKey } = useParams<{ projectKey?: string }>()
  const [docs, setDocs] = useState<Doc[]>([])
  const [error, setError] = useState<string | null>(null)
  const endpoint = projectKey ? `/api/p/${projectKey}/knowledge` : '/api/global/knowledge'
  useEffect(() => { fetch(endpoint).then(async (response) => { if (!response.ok) throw new Error(await response.text()); return response.json() }).then(setDocs).catch((err: unknown) => setError(err instanceof Error ? err.message : 'Could not load knowledge')) }, [endpoint])
  return <main className="mx-auto min-h-svh max-w-5xl px-5 py-8 sm:px-8"><Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Projects</Link><header className="mb-8 mt-6 flex items-end justify-between"><div><p className="font-mono text-xs uppercase tracking-wide text-muted-foreground">{projectKey ?? 'Global'} knowledge</p><h1 className="mt-2 text-3xl font-semibold tracking-tight text-foreground">Knowledge base</h1></div><BookOpen className="size-6 text-muted-foreground" /></header>{error && <p className="text-sm text-destructive">{error}</p>}<div className="grid gap-4 sm:grid-cols-2">{docs.map((doc) => <Card key={doc.id}><CardContent className="p-5"><h2 className="font-medium text-foreground">{doc.title}</h2><p className="mt-1 font-mono text-xs text-muted-foreground">{doc.slug}{doc.board ? ` · ${doc.board}` : ''}</p>{doc.summary && <p className="mt-3 text-sm text-muted-foreground">{doc.summary}</p>}</CardContent></Card>)}</div>{docs.length === 0 && !error && <p className="text-sm text-muted-foreground">No knowledge entries yet.</p>}</main>
}
