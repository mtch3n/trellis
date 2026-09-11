import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, BookOpen, GripVertical, Plus, RefreshCw, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

interface CardInfo { id: string; ref: string; title: string; body: string; priority: string; version: number; owner?: string }
interface ColumnCardsInfo { name: string; cards: CardInfo[] }
interface CardDetail { card: CardInfo; notes: { id: string; actor: string; body: string; created_at: number }[]; activity: { seq: number; actor: string; action: string; field?: string }[] }

export function BoardPage() {
  const { projectKey, boardSlug } = useParams<{ projectKey: string; boardSlug: string }>()
  const [columns, setColumns] = useState<ColumnCardsInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [draft, setDraft] = useState({ title: '', body: '', priority: 'normal' })
  const [editing, setEditing] = useState<CardInfo | null>(null)
  const [detail, setDetail] = useState<CardDetail | null>(null)
  const [saving, setSaving] = useState(false)
  const [stealReason, setStealReason] = useState('')

  const loadBoard = async () => {
    if (!projectKey || !boardSlug) return
    setLoading(true)
    try {
      const response = await fetch(`/api/p/${projectKey}/b/${boardSlug}/cards`)
      if (!response.ok) throw new Error('Could not load board')
      setColumns(await response.json())
      setError(null)
    } catch (err) { setError(err instanceof Error ? err.message : 'Unknown error') } finally { setLoading(false) }
  }
  useEffect(() => { void loadBoard() }, [projectKey, boardSlug])
  useEffect(() => {
    if (!projectKey || !boardSlug) return
    const events = new EventSource(`/api/p/${projectKey}/b/${boardSlug}/events`)
    events.addEventListener('changed', () => { void loadBoard() })
    return () => events.close()
  }, [projectKey, boardSlug])
  const total = useMemo(() => columns.reduce((sum, column) => sum + column.cards.length, 0), [columns])

  const createCard = async (event: FormEvent) => {
    event.preventDefault()
    if (!projectKey || !boardSlug || !draft.title.trim()) return
    setSaving(true)
    try {
      const response = await fetch(`/api/p/${projectKey}/b/${boardSlug}/cards`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...draft, priority: { urgent: 0, high: 1, normal: 2, low: 3 }[draft.priority as 'urgent' | 'high' | 'normal' | 'low'] }) })
      if (!response.ok) throw new Error(await response.text())
      setDraft({ title: '', body: '', priority: 'normal' })
      await loadBoard()
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not create card') } finally { setSaving(false) }
  }

  const saveCard = async (event: FormEvent) => {
    event.preventDefault()
    if (!projectKey || !boardSlug || !editing) return
    setSaving(true)
    try {
      const priority = { urgent: 0, high: 1, normal: 2, low: 3 }[editing.priority as 'urgent' | 'high' | 'normal' | 'low']
      const response = await fetch(`/api/p/${projectKey}/b/${boardSlug}/cards/${encodeURIComponent(editing.ref)}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title: editing.title, body: editing.body, priority, if_version: editing.version }) })
      if (!response.ok) throw new Error(await response.text())
      setEditing(null)
      await loadBoard()
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not save card') } finally { setSaving(false) }
  }

  const moveCard = async (card: CardInfo, column: string, before = '') => {
    if (!projectKey || !boardSlug) return
    try {
      const response = await fetch(`/api/p/${projectKey}/b/${boardSlug}/cards/${encodeURIComponent(card.ref)}/move`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ column, before }) })
      if (!response.ok) throw new Error(await response.text())
      await loadBoard()
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not move card') }
  }

  const stealCard = async () => {
    if (!projectKey || !boardSlug || !editing || !stealReason.trim()) return
    setSaving(true)
    try {
      const response = await fetch(`/api/p/${projectKey}/b/${boardSlug}/cards/${encodeURIComponent(editing.ref)}/steal`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason: stealReason }) })
      if (!response.ok) throw new Error(await response.text())
      setStealReason('')
      await loadBoard()
      const refreshed = await fetch(`/api/p/${projectKey}/b/${boardSlug}/cards/${encodeURIComponent(editing.ref)}`)
      if (refreshed.ok) setDetail(await refreshed.json())
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not steal lease') } finally { setSaving(false) }
  }

  const openCard = async (card: CardInfo) => {
    setEditing(card)
    if (!projectKey || !boardSlug) return
    try {
      const response = await fetch(`/api/p/${projectKey}/b/${boardSlug}/cards/${encodeURIComponent(card.ref)}`)
      if (response.ok) setDetail(await response.json())
    } catch { /* the editor remains useful when activity is temporarily unavailable */ }
  }

  if (loading && columns.length === 0) return <LoadingBoard />
  if (error && columns.length === 0) return <main className="mx-auto min-h-svh max-w-7xl px-5 py-10"><p className="text-destructive">{error}</p><Button className="mt-4" variant="outline" onClick={() => void loadBoard()}>Retry</Button></main>

  return <main className="mx-auto min-h-svh max-w-7xl px-5 py-8 sm:px-8 lg:px-10">
    <header className="mb-8 flex flex-wrap items-end justify-between gap-5 border-b border-border pb-6"><div><Link to="/" className="mb-5 inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Projects</Link><p className="font-mono text-xs uppercase tracking-wide text-muted-foreground">{projectKey}</p><h1 className="mt-2 text-3xl font-semibold tracking-tight text-foreground">{boardSlug}</h1><p className="mt-2 text-sm text-muted-foreground">{total} active card{total === 1 ? '' : 's'}</p></div><div className="flex gap-2"><Link to={`/p/${projectKey}/b/${boardSlug}/kb`}><Button variant="outline" size="sm"><BookOpen className="size-4" />Knowledge</Button></Link><Button variant="outline" size="sm" onClick={() => void loadBoard()}><RefreshCw className="size-4" />Refresh</Button></div></header>
    {error && <div className="mb-5 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">{columns.map((column) => <section key={column.name} data-testid={`board-column-${column.name}`} className="min-h-72 rounded-lg border border-border/80 bg-muted/20 p-3" onDragOver={(event) => event.preventDefault()} onDrop={(event) => { const ref = event.dataTransfer.getData('text/plain'); const card = columns.flatMap((item) => item.cards).find((item) => item.ref === ref); if (card) void moveCard(card, column.name) }}><div className="mb-3 flex items-center justify-between px-1"><h2 className="text-sm font-semibold text-foreground">{column.name}</h2><span className="rounded-full bg-background px-2 py-0.5 font-mono text-xs text-muted-foreground">{column.cards.length}</span></div><div className="space-y-2">{column.cards.map((card) => <Card key={card.id} data-testid={`board-card-${card.ref}`} draggable onDragStart={(event) => event.dataTransfer.setData('text/plain', card.ref)} onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.stopPropagation(); const ref = event.dataTransfer.getData('text/plain'); const source = columns.flatMap((item) => item.cards).find((item) => item.ref === ref); if (source && source.ref !== card.ref) void moveCard(source, column.name, card.ref) }} onClick={() => void openCard(card)} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') void openCard(card) }} role="button" tabIndex={0} className="cursor-grab border-border bg-background shadow-none transition-transform hover:-translate-y-px active:cursor-grabbing"><CardContent className="p-3"><div className="flex items-start gap-2"><GripVertical className="mt-0.5 size-4 shrink-0 text-muted-foreground/60" /><div className="min-w-0 flex-1"><div className="font-mono text-xs text-muted-foreground">{card.ref}</div><div className="mt-1 line-clamp-2 text-sm font-medium text-foreground">{card.title}</div><div className="mt-3 flex items-center justify-between gap-2 text-xs text-muted-foreground"><span className="capitalize">{card.priority}</span>{card.owner && <span className="truncate">{card.owner}</span>}</div></div></div></CardContent></Card>)}</div></section>)}</div>
    <form onSubmit={createCard} className="mt-8 rounded-lg border border-border bg-background p-5"><div className="mb-4 flex items-center justify-between"><div><h2 className="font-semibold text-foreground">Add work</h2><p className="mt-1 text-sm text-muted-foreground">New cards start in the first column.</p></div><Plus className="size-5 text-muted-foreground" /></div><div className="grid gap-3 md:grid-cols-3"><Input aria-label="Card title" placeholder="Card title" value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /><Input aria-label="Card body" placeholder="Context or acceptance notes" value={draft.body} onChange={(event) => setDraft({ ...draft, body: event.target.value })} /><Button type="submit" disabled={saving || !draft.title.trim()}>{saving ? 'Saving…' : 'Create card'}</Button></div><div className="mt-3 flex gap-2">{['urgent', 'high', 'normal', 'low'].map((priority) => <Button key={priority} type="button" size="xs" variant={draft.priority === priority ? 'default' : 'outline'} onClick={() => setDraft({ ...draft, priority })}>{priority}</Button>)}</div></form>
    {editing && <div className="fixed inset-0 z-10 grid place-items-center bg-foreground/20 p-5" onMouseDown={() => { setEditing(null); setDetail(null) }}><form onSubmit={saveCard} onMouseDown={(event) => event.stopPropagation()} className="max-h-svh w-full max-w-lg overflow-y-auto rounded-lg border border-border bg-background p-6 shadow-lg"><div className="mb-5 flex items-start justify-between"><div><p className="font-mono text-xs text-muted-foreground">{editing.ref} · version {editing.version}</p><h2 className="mt-1 text-xl font-semibold text-foreground">Edit card</h2></div><Button type="button" size="icon" variant="ghost" aria-label="Close editor" onClick={() => { setEditing(null); setDetail(null) }}><X className="size-4" /></Button></div><div className="space-y-4"><label className="block text-sm font-medium text-foreground">Title<Input aria-label="Card title editor" className="mt-2" value={editing.title} onChange={(event) => setEditing({ ...editing, title: event.target.value })} /></label><label className="block text-sm font-medium text-foreground">Body<Textarea aria-label="Card body editor" className="mt-2" value={editing.body} onChange={(event) => setEditing({ ...editing, body: event.target.value })} /></label><div className="flex flex-wrap gap-2">{['urgent', 'high', 'normal', 'low'].map((priority) => <Button key={priority} type="button" size="xs" variant={editing.priority === priority ? 'default' : 'outline'} onClick={() => setEditing({ ...editing, priority })}>{priority}</Button>)}</div>{editing.owner && <div className="rounded-lg border border-border bg-muted/30 p-3"><p className="text-sm font-medium text-foreground">Lease held by {editing.owner}</p><div className="mt-2 flex gap-2"><Input aria-label="Steal reason" placeholder="Reason for taking over" value={stealReason} onChange={(event) => setStealReason(event.target.value)} /><Button type="button" variant="outline" disabled={saving || !stealReason.trim()} onClick={() => void stealCard()}>Steal lease</Button></div></div>}</div>{detail && <div className="mt-6 space-y-4 border-t border-border pt-5"><div><h3 className="text-sm font-semibold text-foreground">Notes</h3>{detail.notes.length === 0 ? <p className="mt-2 text-sm text-muted-foreground">No notes yet.</p> : <div className="mt-2 space-y-2">{detail.notes.map((note) => <div key={note.id} className="rounded-lg bg-muted/50 p-3 text-sm"><p className="text-foreground">{note.body}</p><p className="mt-2 font-mono text-xs text-muted-foreground">{note.actor}</p></div>)}</div>}</div><div><h3 className="text-sm font-semibold text-foreground">Activity</h3><div className="mt-2 space-y-1 text-xs text-muted-foreground">{detail.activity.slice(0, 8).map((event) => <p key={event.seq}><span className="font-mono">#{event.seq}</span> {event.actor} {event.action}{event.field ? ` · ${event.field}` : ''}</p>)}</div></div></div>}<div className="mt-6 flex justify-end gap-2"><Button type="button" variant="ghost" onClick={() => { setEditing(null); setDetail(null) }}>Cancel</Button><Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save changes'}</Button></div></form></div>}
  </main>
}

function LoadingBoard() { return <main className="mx-auto min-h-svh max-w-7xl px-5 py-10"><div className="h-10 w-56 animate-pulse rounded-lg bg-muted" /><div className="mt-8 grid gap-4 md:grid-cols-2 xl:grid-cols-4">{[1, 2, 3, 4].map((item) => <div className="h-64 animate-pulse rounded-lg bg-muted" key={item} />)}</div></main> }
