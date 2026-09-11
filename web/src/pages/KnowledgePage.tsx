import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, BookOpen, GitBranch, Save } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'

interface Knowledge { id: string; slug: string; title: string; summary?: string; body?: string; type?: string; version: number }
interface Graph { root: string; nodes: { id: string; type: string; ref: string; title: string; depth: number; done?: boolean }[]; edges: { from: string; to: string; rel: string }[] }
interface Label { name: string; description?: string }

interface GraphPosition { id: string; x: number; y: number }

function layoutGraph(graph: Graph): GraphPosition[] {
  const width = 640
  const height = 300
  const positions = graph.nodes.map((node, index) => {
    const angle = (index / Math.max(graph.nodes.length, 1)) * Math.PI * 2
    return { id: node.id, x: width / 2 + Math.cos(angle) * 150, y: height / 2 + Math.sin(angle) * 95 }
  })
  const edges = graph.edges.flatMap((edge) => {
    const from = positions.findIndex((position) => position.id === edge.from)
    const to = positions.findIndex((position) => position.id === edge.to)
    return from >= 0 && to >= 0 ? [[from, to] as const] : []
  })
  for (let iteration = 0; iteration < 40; iteration += 1) {
    const forces = positions.map(() => ({ x: 0, y: 0 }))
    for (let i = 0; i < positions.length; i += 1) for (let j = i + 1; j < positions.length; j += 1) {
      const dx = positions[i].x - positions[j].x
      const dy = positions[i].y - positions[j].y
      const distance = Math.max(Math.hypot(dx, dy), 1)
      const force = 1800 / (distance * distance)
      forces[i].x += (dx / distance) * force; forces[i].y += (dy / distance) * force
      forces[j].x -= (dx / distance) * force; forces[j].y -= (dy / distance) * force
    }
    for (const [from, to] of edges) {
      const dx = positions[to].x - positions[from].x
      const dy = positions[to].y - positions[from].y
      const distance = Math.max(Math.hypot(dx, dy), 1)
      const force = (distance - 125) * 0.018
      forces[from].x += (dx / distance) * force; forces[from].y += (dy / distance) * force
      forces[to].x -= (dx / distance) * force; forces[to].y -= (dy / distance) * force
    }
    positions.forEach((position, index) => {
      position.x = Math.min(width - 35, Math.max(35, position.x + forces[index].x))
      position.y = Math.min(height - 35, Math.max(35, position.y + forces[index].y))
    })
  }
  return positions
}

export function KnowledgePage() {
  const { projectKey, boardSlug } = useParams<{ projectKey: string; boardSlug: string }>()
  const base = `/api/p/${projectKey}/b/${boardSlug}`
  const [docs, setDocs] = useState<Knowledge[]>([])
  const [selected, setSelected] = useState<Knowledge | null>(null)
  const [draft, setDraft] = useState({ title: '', summary: '', body: '', template: 'note' })
  const [labels, setLabels] = useState<Label[]>([])
  const [merge, setMerge] = useState({ from: '', into: '' })
  const [graph, setGraph] = useState<Graph | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const positions = useMemo(() => graph ? layoutGraph(graph) : [], [graph])

  const load = async () => {
    try {
      const [docsResponse, labelsResponse] = await Promise.all([fetch(`${base}/knowledge`), fetch(`/api/p/${projectKey}/labels`)]);
      if (!docsResponse.ok) throw new Error('Could not load knowledge base')
      setDocs(await docsResponse.json())
      if (labelsResponse.ok) setLabels(await labelsResponse.json())
      setError(null)
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not load knowledge base') }
  }
  useEffect(() => { void load() }, [projectKey, boardSlug])

  const choose = async (doc: Knowledge) => {
    setSelected(doc)
    setDraft({ title: doc.title, summary: doc.summary ?? '', body: doc.body ?? '', template: doc.type ?? 'note' })
    setGraph(null)
    const response = await fetch(`${base}/graph/${encodeURIComponent(doc.slug)}?depth=2`)
    if (response.ok) setGraph(await response.json())
  }

  const save = async (event: FormEvent) => {
    event.preventDefault()
    setSaving(true)
    try {
      const url = selected ? `${base}/knowledge/${encodeURIComponent(selected.slug)}` : `${base}/knowledge`
      const response = await fetch(url, { method: selected ? 'PATCH' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...draft, version: selected?.version }) })
      if (!response.ok) throw new Error(await response.text())
      const doc = await response.json() as Knowledge
      setSelected(doc)
      setDraft({ title: doc.title, summary: doc.summary ?? '', body: doc.body ?? '', template: doc.type ?? 'note' })
      await load()
    } catch (err) { setError(err instanceof Error ? err.message : 'Could not save document') } finally { setSaving(false) }
  }

  const mergeLabels = async (event: FormEvent) => {
    event.preventDefault()
    const response = await fetch(`/api/p/${projectKey}/labels/merge`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(merge) })
    if (!response.ok) setError(await response.text()); else { setMerge({ from: '', into: '' }); await load() }
  }

  return <main className="mx-auto min-h-svh max-w-7xl px-5 py-8 sm:px-8 lg:px-10">
    <header className="mb-8 flex flex-wrap items-end justify-between gap-5 border-b border-border pb-6"><div><Link to={`/p/${projectKey}/b/${boardSlug}`} className="mb-5 inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Board</Link><p className="font-mono text-xs uppercase tracking-wide text-muted-foreground">{projectKey}</p><h1 className="mt-2 text-3xl font-semibold tracking-tight text-foreground">Knowledge base</h1></div><BookOpen className="size-6 text-muted-foreground" /></header>
    {error && <p className="mb-5 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</p>}
    <div className="grid gap-5 lg:grid-cols-3">
      <aside className="space-y-3"><Card><CardHeader><CardTitle className="text-base">Documents</CardTitle></CardHeader><CardContent className="space-y-2">{docs.length === 0 ? <p className="text-sm text-muted-foreground">No documents yet.</p> : docs.map((doc) => <Button key={doc.id} variant={selected?.id === doc.id ? 'secondary' : 'ghost'} className="h-auto w-full justify-start whitespace-normal text-left" onClick={() => void choose(doc)}><span><span className="block text-sm">{doc.title}</span><span className="block font-mono text-xs text-muted-foreground">{doc.slug}</span></span></Button>)}</CardContent></Card><Card><CardHeader><CardTitle className="text-base">Merge labels</CardTitle></CardHeader><CardContent><form onSubmit={mergeLabels} className="space-y-2"><Input aria-label="Label to merge" placeholder="from" value={merge.from} onChange={(event) => setMerge({ ...merge, from: event.target.value })} /><Input aria-label="Label destination" placeholder="into" value={merge.into} onChange={(event) => setMerge({ ...merge, into: event.target.value })} /><Button type="submit" size="sm" disabled={!merge.from || !merge.into}>Merge</Button>{labels.length > 0 && <p className="text-xs text-muted-foreground">Available: {labels.map((label) => label.name).join(', ')}</p>}</form></CardContent></Card></aside>
      <section className="space-y-5 lg:col-span-2"><Card><CardHeader><CardTitle className="text-base">{selected ? `Edit ${selected.title}` : 'New document'}</CardTitle></CardHeader><CardContent><form onSubmit={save} className="space-y-4"><Input aria-label="Document title" placeholder="Title" value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /><Input aria-label="Document summary" placeholder="Summary" value={draft.summary} onChange={(event) => setDraft({ ...draft, summary: event.target.value })} /><Textarea aria-label="Document body" className="min-h-64" placeholder="Markdown body" value={draft.body} onChange={(event) => setDraft({ ...draft, body: event.target.value })} /><div className="flex justify-end"><Button type="submit" disabled={saving || !draft.title.trim()}><Save className="size-4" />{saving ? 'Saving…' : selected ? 'Save document' : 'Create document'}</Button></div></form></CardContent></Card><Card><CardHeader><CardTitle className="text-base">Preview</CardTitle></CardHeader><CardContent><MarkdownContent content={draft.body || '*Start writing Markdown to see a preview.*'} /></CardContent></Card>{graph && <Card><CardHeader><CardTitle className="flex items-center gap-2 text-base"><GitBranch className="size-4" />Linked graph</CardTitle></CardHeader><CardContent><svg className="h-72 w-full rounded-lg border border-border bg-muted/20" viewBox="0 0 640 300" role="img" aria-label="Knowledge links graph">{graph.edges.map((edge, index) => { const from = positions.find((position) => position.id === edge.from); const to = positions.find((position) => position.id === edge.to); if (!from || !to) return null; return <g key={`${edge.from}-${edge.to}-${index}`}><line x1={from.x} y1={from.y} x2={to.x} y2={to.y} stroke="currentColor" strokeOpacity="0.25" /><text x={(from.x + to.x) / 2} y={(from.y + to.y) / 2 - 5} textAnchor="middle" className="fill-muted-foreground text-xs">{edge.rel}</text></g> })}{graph.nodes.map((node) => { const position = positions.find((candidate) => candidate.id === node.id); if (!position) return null; return <g key={node.id}><circle cx={position.x} cy={position.y} r="25" className="fill-background stroke-border" /><text x={position.x} y={position.y - 2} textAnchor="middle" className="fill-foreground text-xs">{node.type}</text><text x={position.x} y={position.y + 11} textAnchor="middle" className="fill-muted-foreground text-xs">{node.ref.slice(0, 14)}</text></g> })}</svg><div className="mt-3 space-y-2">{graph.nodes.map((node) => <div key={node.id} className="rounded-lg border border-border px-3 py-2"><span className="font-mono text-xs text-muted-foreground">{node.type} · depth {node.depth}</span><p className="text-sm text-foreground">{node.title}</p><p className="font-mono text-xs text-muted-foreground">{node.ref}</p></div>)}</div>{graph.edges.length > 0 && <p className="mt-3 text-xs text-muted-foreground">{graph.edges.length} relationship{graph.edges.length === 1 ? '' : 's'} traversed.</p>}</CardContent></Card>}</section>
    </div>
  </main>
}
