import { useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, boardColumns, errorText, type Card } from '@/lib/api'
import { notifyDone, notifyError } from '@/lib/notify'
import { moment, when } from '@/lib/time'
import { useLoad } from '@/lib/use-load'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'
import { NativeSelect } from '@/components/wrappers/NativeSelect'
import { PageHeader } from '@/components/wrappers/PageHeader'

function CardEditor({ card, onSave, onCancel }: { card: Card; onSave: (title: string, body: string) => void; onCancel: () => void }) {
  const [title, setTitle] = useState(card.title)
  const [body, setBody] = useState(card.body)
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault()
        onSave(title, body)
      }}
    >
      <Input value={title} onChange={(event) => setTitle(event.target.value)} aria-label="Title" />
      <Textarea value={body} onChange={(event) => setBody(event.target.value)} aria-label="Body" className="min-h-48 font-mono" />
      <div className="flex gap-2">
        <Button type="submit">Save</Button>
        <Button type="button" variant="outline" onClick={onCancel}>Cancel</Button>
      </div>
    </form>
  )
}

export function CardPage() {
  const { projectKey = '', cardRef = '' } = useParams()
  const loaded = useLoad(() => api.card(projectKey, cardRef), [projectKey, cardRef])
  const card = loaded.data
  const [editing, setEditing] = useState(false)
  const [comment, setComment] = useState('')

  const act = async (run: () => Promise<Card>, done?: string) => {
    try {
      await run()
      loaded.reload()
      if (done) notifyDone(done)
      return true
    } catch (err) {
      notifyError(err)
      return false
    }
  }

  if (loaded.loading && !card) return <Spinner className="mt-8" />
  if (loaded.error) return <p className="mt-8 text-sm text-danger">{errorText(loaded.error)}</p>
  if (!card) return null

  const addComment = async (event: FormEvent) => {
    event.preventDefault()
    if (await act(() => api.commentCard(projectKey, card.ref, comment.trim()))) setComment('')
  }

  return (
    <>
      <nav className="pt-4 text-sm text-muted-foreground">
        <Link to={`/p/${encodeURIComponent(projectKey)}/b/${encodeURIComponent(card.board)}`} className="hover:underline">
          Board {card.board}
        </Link>
      </nav>
      <PageHeader
        title={card.title}
        facts={[
          { label: 'Ref', value: card.ref },
          { label: 'Claimed by', value: card.claimed_by ?? '', tone: 'claimed' },
        ]}
        actions={
          <>
            <NativeSelect
              aria-label="Column"
              value={card.column}
              onChange={(event) => act(() => api.moveCard(projectKey, card.ref, event.target.value), `Moved to ${event.target.value}`)}
            >
              {boardColumns([card]).map((column) => (
                <option key={column} value={column}>{column}</option>
              ))}
            </NativeSelect>
            {card.claimed_by ? (
              <Button variant="outline" onClick={() => act(() => api.releaseCard(projectKey, card.ref), 'Released')}>Release</Button>
            ) : (
              <Button variant="outline" onClick={() => act(() => api.claimCard(projectKey, card.ref), 'Claimed')}>Claim</Button>
            )}
            {!editing && <Button variant="outline" onClick={() => setEditing(true)}>Edit</Button>}
          </>
        }
      />
      <div className="grid gap-8 lg:grid-cols-[1fr_18rem]">
        <div className="flex min-w-0 flex-col gap-8">
          {editing ? (
            <CardEditor
              card={card}
              onCancel={() => setEditing(false)}
              onSave={async (title, body) => {
                if (await act(() => api.editCard(projectKey, card.ref, { title, body, if_version: card.version }), 'Saved')) {
                  setEditing(false)
                }
              }}
            />
          ) : card.body ? (
            <MarkdownContent content={card.body} />
          ) : (
            <p className="text-sm text-muted-foreground">No description.</p>
          )}
          <section className="flex flex-col gap-3">
            <h2 className="text-sm font-medium">Comments</h2>
            {(card.comments ?? []).map((item) => (
              <article key={item.id} className="rounded-lg border p-3 text-sm">
                <p className="mb-1 text-xs text-muted-foreground">{item.actor} · {when(item.created_at)}</p>
                <MarkdownContent content={item.body} />
              </article>
            ))}
            <form onSubmit={addComment} className="flex flex-col gap-2">
              <Textarea value={comment} onChange={(event) => setComment(event.target.value)} placeholder="Write a comment" aria-label="Comment" />
              <Button type="submit" className="self-start" disabled={!comment.trim()}>Comment</Button>
            </form>
          </section>
        </div>
        <aside className="flex flex-col gap-3 text-sm">
          <h2 className="font-medium">Details</h2>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-muted-foreground">
            <dt>Created by</dt><dd className="text-foreground">{card.created_by}</dd>
            <dt>Created</dt><dd className="text-foreground">{when(card.created_at)}</dd>
            <dt>Updated</dt><dd className="text-foreground">{when(card.updated_at)}</dd>
            {card.claim_until && (<><dt>Claim until</dt><dd className="text-foreground">{moment(card.claim_until)}</dd></>)}
          </dl>
          {(card.blocked_by ?? []).length > 0 && (
            <div className="flex flex-wrap gap-1">
              <span className="text-muted-foreground">Blocked by</span>
              {card.blocked_by?.map((ref) => <Badge key={ref} variant="outline">{ref}</Badge>)}
            </div>
          )}
          <h2 className="mt-4 font-medium">History</h2>
          <ol className="flex flex-col gap-1 text-xs text-muted-foreground">
            {(card.events ?? []).map((event) => (
              <li key={event.id}>
                <span className="text-foreground">{event.kind}</span> by {event.actor} · {when(event.created_at)}
              </li>
            ))}
          </ol>
        </aside>
      </div>
    </>
  )
}
