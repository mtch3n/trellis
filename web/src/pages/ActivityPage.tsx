import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Activity } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'

interface Event { seq: number; timestamp: number; actor: string; entity_type: string; action: string; field?: string; title: string; project: string }

export function ActivityPage() {
  const [events, setEvents] = useState<Event[]>([])
  const [error, setError] = useState<string | null>(null)
  useEffect(() => { fetch('/api/activity').then(async (response) => { if (!response.ok) throw new Error(await response.text()); return response.json() }).then(setEvents).catch((err: unknown) => setError(err instanceof Error ? err.message : 'Could not load activity')) }, [])
  return <main className="mx-auto min-h-svh max-w-5xl px-5 py-8 sm:px-8"><Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Projects</Link><header className="mb-8 mt-6 flex items-end justify-between"><div><p className="font-mono text-xs uppercase tracking-wide text-muted-foreground">Workspace feed</p><h1 className="mt-2 text-3xl font-semibold tracking-tight text-foreground">Recent activity</h1></div><Activity className="size-6 text-muted-foreground" /></header>{error && <p className="text-sm text-destructive">{error}</p>}<div className="space-y-3">{events.map((event) => <Card key={event.seq}><CardContent className="flex flex-wrap items-baseline gap-x-3 gap-y-1 p-4"><span className="font-mono text-xs text-muted-foreground">#{event.seq}</span><span className="text-sm font-medium text-foreground">{event.action}</span><span className="text-sm text-muted-foreground">{event.title}</span><span className="font-mono text-xs text-muted-foreground">{event.project} · {event.actor}{event.field ? ` · ${event.field}` : ''}</span></CardContent></Card>)}</div></main>
}
