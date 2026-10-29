import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError, boardColumns, errorText, type Card } from '@/lib/api'
import { notifyError } from '@/lib/notify'
import { useSession } from '@/lib/session'
import { useLoad } from '@/lib/use-load'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { NativeSelect } from '@/components/wrappers/NativeSelect'
import { PageHeader } from '@/components/wrappers/PageHeader'

export function BoardRedirect() {
  const { projectKey = '' } = useParams()
  const me = useSession()
  return <Navigate to={`/p/${encodeURIComponent(projectKey)}/b/${encodeURIComponent(me.handle)}`} replace />
}

function CardTile({ projectKey, card, columns, onMoved }: { projectKey: string; card: Card; columns: string[]; onMoved: () => void }) {
  const move = async (column: string) => {
    try {
      await api.moveCard(projectKey, card.ref, column)
      onMoved()
    } catch (err) {
      notifyError(err)
    }
  }
  return (
    <li className="flex flex-col gap-2 rounded-lg border bg-card p-3 text-sm">
      <Link to={`/p/${encodeURIComponent(projectKey)}/card/${encodeURIComponent(card.ref)}`} className="font-medium hover:underline">
        {card.title}
      </Link>
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="font-mono">{card.ref}</span>
        {card.claimed_by && <Badge variant="outline">{card.claimed_by}</Badge>}
        <NativeSelect
          aria-label={`Move ${card.ref}`}
          value={card.column}
          onChange={(event) => move(event.target.value)}
          className="ml-auto h-7 text-xs"
        >
          {columns.map((column) => (
            <option key={column} value={column}>{column}</option>
          ))}
        </NativeSelect>
      </div>
    </li>
  )
}

export function BoardPage() {
  const { projectKey = '', boardName = '' } = useParams()
  const navigate = useNavigate()
  const me = useSession()
  const boards = useLoad(() => api.boards(projectKey), [projectKey])
  const cards = useLoad(() => api.cards(projectKey, boardName), [projectKey, boardName])
  const [title, setTitle] = useState('')
  const [busy, setBusy] = useState(false)

  const missing = cards.error instanceof ApiError && cards.error.status === 404
  const list = missing ? [] : (cards.data ?? [])
  const columns = boardColumns(list)
  const names = [...new Set([me.handle, ...(boards.data ?? []).map((board) => board.name), boardName])]

  const create = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      await api.newCard(projectKey, boardName, title.trim(), '')
      setTitle('')
      cards.reload()
      boards.reload()
    } catch (err) {
      notifyError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title={`Board ${boardName}`}
        actions={
          <NativeSelect
            aria-label="Board"
            value={boardName}
            onChange={(event) => navigate(`/p/${encodeURIComponent(projectKey)}/b/${encodeURIComponent(event.target.value)}`)}
          >
            {names.map((name) => (
              <option key={name} value={name}>{name}</option>
            ))}
          </NativeSelect>
        }
      />
      <form onSubmit={create} className="mb-6 flex gap-2">
        <Input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="New card title" aria-label="New card title" />
        <Button type="submit" disabled={busy || !title.trim()}>Add card</Button>
      </form>
      {cards.loading && !cards.data && <Spinner />}
      {Boolean(cards.error) && !missing && <p className="text-sm text-danger">{errorText(cards.error)}</p>}
      {missing && <p className="mb-4 text-sm text-muted-foreground">This board has no cards yet. Add one above.</p>}
      <div className="grid gap-4 md:grid-cols-4">
        {columns.map((column) => {
          const inColumn = list.filter((card) => card.column === column)
          return (
            <section key={column} className="flex flex-col gap-2 rounded-xl bg-muted/40 p-3" aria-label={column}>
              <h2 className="flex items-center justify-between text-sm font-medium">
                {column}
                <span className="text-xs text-muted-foreground">{inColumn.length}</span>
              </h2>
              <ul className="flex flex-col gap-2">
                {inColumn.map((card) => (
                  <CardTile key={card.id} projectKey={projectKey} card={card} columns={columns} onMoved={cards.reload} />
                ))}
              </ul>
            </section>
          )
        })}
      </div>
    </>
  )
}
