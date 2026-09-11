import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'

interface Detail { card: { ref: string; title: string; body: string; priority: string; owner?: string }; notes: { id: string; actor: string; body: string }[]; activity: { seq: number; actor: string; action: string }[] }

export function CardPage() {
  const { projectKey, cardRef } = useParams<{ projectKey: string; cardRef: string }>()
  const [detail, setDetail] = useState<Detail | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => { fetch(`/api/p/${projectKey}/cards/${encodeURIComponent(cardRef ?? '')}`).then(async (response) => { if (!response.ok) throw new Error(await response.text()); return response.json() }).then(setDetail).catch((err: unknown) => setError(err instanceof Error ? err.message : 'Could not load card')) }, [projectKey, cardRef])
  return <main className="mx-auto min-h-svh max-w-3xl px-5 py-8 sm:px-8"><Link to={`/p/${projectKey}`} className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Project</Link>{error && <p className="mt-6 text-sm text-destructive">{error}</p>}{detail && <><header className="mb-8 mt-6"><p className="font-mono text-xs text-muted-foreground">{detail.card.ref} · {detail.card.priority}</p><h1 className="mt-2 text-3xl font-semibold tracking-tight text-foreground">{detail.card.title}</h1>{detail.card.owner && <p className="mt-2 text-sm text-muted-foreground">Held by {detail.card.owner}</p>}</header><Card><CardHeader><CardTitle className="text-base">Description</CardTitle></CardHeader><CardContent><MarkdownContent content={detail.card.body || '*No description.*'} /></CardContent></Card><Card className="mt-5"><CardHeader><CardTitle className="text-base">Notes and activity</CardTitle></CardHeader><CardContent className="space-y-5">{detail.notes.map((note) => <div key={note.id}><MarkdownContent content={note.body} /><p className="mt-2 text-xs text-muted-foreground">· {note.actor}</p></div>)}{detail.activity.map((event) => <p key={event.seq} className="font-mono text-xs text-muted-foreground">#{event.seq} {event.actor} {event.action}</p>)}</CardContent></Card></>}</main>
}
