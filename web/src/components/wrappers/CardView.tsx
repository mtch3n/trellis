import { useRef, useState, useCallback, type FormEvent, type MouseEvent } from 'react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'
import { ArtifactsEditor } from '@/components/wrappers/ArtifactsEditor'
import type { Artifact } from '@/components/wrappers/ArtifactList'
import { ChipEditor } from '@/components/wrappers/ChipEditor'
import { EntryLinksEditor, type CardLink, type EntryOption } from '@/components/wrappers/EntryLinksEditor'
import { RelationsEditor, type CardOption, type Relation } from '@/components/wrappers/RelationsEditor'
import { EditForm, InPlaceText } from '@/components/wrappers/EditInPlace'
import { Lamp } from '@/components/wrappers/Lamp'
import { MarkdownEditor } from '@/components/wrappers/MarkdownEditor'
import { CommentBox } from '@/components/wrappers/CommentBox'
import { CardTimeline, type CardComment, type CardEvent } from '@/components/wrappers/CardTimeline'
import { MetaFacts, MetaGroup, MetaPanel } from '@/components/wrappers/MetaPanel'
import { ago, sentence } from '@/lib/format'
import type { CardActions } from '@/lib/card-actions'
import { meaningfulEvents } from '@/lib/card-events'
import { useNow } from '@/lib/clock'
import { PRIORITIES, shortActor } from '@/lib/cards'

export interface CardInfo {
  id: string
  ref: string
  title: string
  body: string
  priority: string
  version: number
  claimed_by?: string
  /** When the claim runs out. A claim past its time no longer holds the card. */
  claim_until?: number
  /** Set while the card is archived: off the board, not deleted. */
  archived_at?: number
  created_at?: number
  updated_at?: number
  labels?: string[]
  tags?: string[]
  relations?: Relation[]
  /** The entries this card cites. */
  links?: CardLink[]
  /** The files linked to this card. */
  artifacts?: Artifact[]
}


const BODY_PLACEHOLDER = 'Context, or how you will know it is done'

export type CardMode = 'read' | 'edit' | 'create'

/** A new card, as the create form collects it. */
export interface CardDraft {
  title: string
  body: string
  priority: string
  /** The status (board column) the card starts in. */
  column?: string
  labels: string[]
  tags: string[]
}

/** Adding or removing one label or tag. */
export interface ChipChange {
  add?: string
  remove?: string
}

/** What Edit changes on an existing card: its words. */
export interface CardEdit {
  title: string
  body: string
}

/** A new card's chips live in the draft until it exists. */
function apply(values: string[], change: ChipChange) {
  const kept = change.remove ? values.filter((value) => value !== change.remove) : values
  return change.add && !kept.includes(change.add) ? [...kept, change.add] : kept
}

/** How long is left, in the same few words `ago` uses. */
function until(timestamp: number, now = Date.now()) {
  const minutes = Math.round((timestamp - now) / 60000)
  if (minutes < 1) return 'in under a minute'
  if (minutes < 60) return `in ${minutes} min`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `in ${hours} h`
  return `on ${new Date(timestamp).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}`
}

const stamp = (ms: number) => new Date(ms).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })

/**
 * One card, two columns: what it says on the left, what is true about it on the
 * right. The same component backs the dialog and the full page, so a card reads
 * identically wherever it is opened.
 *
 * The view draws no chrome of its own. The dialog and the page each put one
 * action row above it, and nothing repeats the title or the facts.
 *
 * Editing happens in place and covers the words only. Reading, the title and
 * body show a faint wash when pointed at, and a click on either starts
 * editing it. Editing, they become fields where they stand, in the same type,
 * and the Timeline stays put. The facts column has its own logic and
 * ignores Edit: status and priority apply the moment they change. Save,
 * Cancel and the markdown toggle belong to the parent's action row, in the
 * place Edit had, so starting an edit moves nothing.
 */
export function CardView({
  card,
  comments,
  events = [],
  columns,
  currentColumn,
  mode,
  saving,
  wide = false,
  source,
  onModeChange,
  onSave,
  onCreate,
  actions,
  labelOptions,
  entryOptions,
  storedArtifacts = [],
  me,
  base,
  cardOptions,
}: {
  card: CardInfo | null
  /** The card's comments, oldest first, as the card detail returns them. */
  comments: CardComment[]
  /** The card's event log, newest first, as the card detail returns it. */
  events?: CardEvent[]
  columns: string[]
  currentColumn?: string
  mode: CardMode
  saving: boolean
  wide?: boolean
  /** Show the body as markdown source. The parent owns the toggle, which sits with Save. */
  source: boolean
  onModeChange: (mode: CardMode) => void
  onSave: (edit: CardEdit) => Promise<void>
  onCreate: (draft: CardDraft) => Promise<void>
  /** Every write this card supports, shared with the board and the card page. */
  actions: CardActions
  /** The labels this project defines. Labels are picked, never invented. */
  labelOptions: string[]
  /** The entries this card could cite: this project's and the vault's. */
  entryOptions: EntryOption[]
  /** Every artifact the project holds, for linking one that is already here. */
  storedArtifacts?: Artifact[]
  /** Who the server writes as, so a claim this person holds reads as theirs. */
  me?: string
  /** The project's path in the app, for links to related cards. */
  base: string
  /** The board's cards, to relate this one to. */
  cardOptions: CardOption[]
}) {
  // Priority and status are only drafted for a card that does not exist yet.
  const initial = () => ({ title: card?.title ?? '', priority: 'normal', column: columns[0] ?? '', labels: [] as string[], tags: [] as string[] })
  const [draft, setDraft] = useState(initial)
  const [focus, setFocus] = useState<'title' | 'body'>('title')
  const [stealing, setStealing] = useState(false)
  const [reason, setReason] = useState('')
  const readBody = useRef<() => string>(() => card?.body ?? '')
  const registerBody = useCallback((read: () => string) => { readBody.current = read }, [])

  // A different card, or a different mode, starts from what the card says.
  // Reset while rendering, so the first edit frame never shows a stale draft.
  const [shown, setShown] = useState({ id: card?.id, mode })
  if (shown.id !== card?.id || shown.mode !== mode) {
    setShown({ id: card?.id, mode })
    setDraft(initial())
    if (mode === 'read') setFocus('title')
    setStealing(false)
    setReason('')
  }

  const ref = card?.ref ?? ''
  // A claim past its time no longer holds the card: the server lets the next
  // caller claim it, so the interface must not pretend otherwise. The
  // expiry is a wall-clock moment, so the view watches the clock.
  const now = useNow()
  const expired = Boolean(card?.claimed_by) && card?.claim_until !== undefined && card.claim_until < now
  // A claim this person holds is not a lock: they are the actor the server
  // will check, so the card is theirs to change.
  const mine = Boolean(card?.claimed_by) && card?.claimed_by === me
  const locked = Boolean(card?.claimed_by) && !mine && !expired
  const claimant = shortActor(card?.claimed_by)
  const changes = meaningfulEvents(events)
  // base is the project's path in the app (/p/KEY), which is where a cited
  // entry is opened from.
  const projectKey = base.split('/').pop() ?? ''
  const editing = mode !== 'read'
  const creating = mode === 'create'
  // A claimed card's column and priority wait for its claim.
  const fixed = locked && !creating
  const status = creating ? draft.column : (currentColumn ?? '')
  const priority = creating ? draft.priority : (card?.priority ?? 'normal')

  const canEdit = mode === 'read' && Boolean(card) && !locked
  // Reading, a click on the words starts editing them, unless it lands on a
  // link or finishes a text selection.
  const startEdit = (target: 'title' | 'body') => (event: MouseEvent<HTMLElement>) => {
    if (!canEdit) return
    if ((event.target as HTMLElement).closest('a')) return
    if (window.getSelection()?.toString()) return
    setFocus(target)
    onModeChange('edit')
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!draft.title.trim()) return
    const body = readBody.current()
    if (creating) {
      await onCreate({
        title: draft.title,
        body,
        priority: draft.priority,
        column: draft.column || undefined,
        labels: draft.labels,
        tags: draft.tags,
      })
      return
    }
    await onSave({ title: draft.title, body })
  }

  const content = (
    <>
      {editing ? (
        <InPlaceText
          label="Card title"
          placeholder="What needs doing"
          required
          className="text-title text-balance md:text-title"
          value={draft.title}
          focusOnMount={mode === 'edit' && focus === 'title'}
          onChange={(title) => setDraft({ ...draft, title })}
        />
      ) : (
        <h1 className={cn('text-title text-balance', canEdit && 'edit-hint')} onClick={startEdit('title')}>{card?.title}</h1>
      )}
      <Separator className="my-7" />
      {editing ? (
        <MarkdownEditor
          key={`${card?.id ?? 'new'}-${mode}`}
          value={card?.body ?? ''}
          source={source}
          autoFocus={mode === 'edit' && focus === 'body'}
          onRead={registerBody}
          placeholder={BODY_PLACEHOLDER}
          minHeight={creating ? 'min-h-80' : 'min-h-40'}
        />
      ) : (
        <div className={cn(canEdit && 'edit-hint')} onClick={startEdit('body')}>
          {card?.body ? (
            <MarkdownContent content={card.body} />
          ) : canEdit ? (
            <p className="text-body text-muted-foreground/55">{BODY_PLACEHOLDER}</p>
          ) : (
            <p className="text-sm text-muted-foreground">This card has no body.</p>
          )}
        </div>
      )}
    </>
  )

  return (
    <div
      className={cn(
        'grid items-start gap-10',
        wide ? 'justify-center gap-x-14 grid-cols-measure xl:grid-cols-entry' : 'lg:grid-cols-facts',
      )}
    >
      <div className="min-w-0">
        {editing ? <EditForm id="card-form" onSubmit={submit}>{content}</EditForm> : content}

        {/* One timeline: what was said and what changed, in the order it
            happened, with the box for saying something at its top. */}
        {!creating && card && (
          <section className="mt-10">
            <h2 className="flex items-baseline gap-3 text-heading">
              Timeline
              <span className="text-xs font-normal text-muted-foreground">{changes.length + comments.length}</span>
            </h2>
            <div className="mt-4">
              <CommentBox onSubmit={(body) => actions.comment(ref, body)} />
            </div>
            <div className="mt-6">
              <CardTimeline events={changes} comments={comments} />
            </div>
          </section>
        )}
      </div>

      <MetaPanel>
        {/* Status is the board column, named the way trackers name it. Status
            and priority apply the moment they change, editing or not; only a
            card being created drafts them. */}
        <MetaGroup label="Column">
          <Select
            items={columns.map((name) => ({ value: name, label: sentence(name) }))}
            value={status}
            onValueChange={(value) => {
              if (!value || value === status) return
              if (creating) setDraft({ ...draft, column: value })
              else void actions.move(ref, value)
            }}
          >
            <SelectTrigger
              aria-label="Column"
              className="w-full"
              disabled={fixed}
              title={fixed ? `Claimed by ${claimant}. Steal the claim to change its column.` : undefined}
            >
              <SelectValue placeholder="Column" />
            </SelectTrigger>
            <SelectContent>
              {columns.map((name) => (
                <SelectItem key={name} value={name}>{sentence(name)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </MetaGroup>

        <MetaGroup label="Priority">
          <Select
            items={PRIORITIES.map((value) => ({ value, label: sentence(value) }))}
            value={priority}
            onValueChange={(value) => {
              if (!value || value === priority) return
              if (creating) setDraft({ ...draft, priority: value })
              else void actions.priority(ref, value)
            }}
          >
            <SelectTrigger
              aria-label="Priority"
              className={cn('w-full', priority === 'urgent' && 'text-danger')}
              disabled={fixed}
              title={fixed ? `Claimed by ${claimant}. Steal the claim to change its priority.` : undefined}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PRIORITIES.map((value) => (
                <SelectItem key={value} value={value}>{sentence(value)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </MetaGroup>

        {/* Labels are the project's own vocabulary, so they are picked from
            it; tags are free words. Both apply as they change, like status
            and priority, and a new card carries them into its creation. */}
        <MetaGroup label="Labels">
          <ChipEditor
            name="label"
            values={creating ? draft.labels : (card?.labels ?? [])}
            options={labelOptions}
            placeholder="Label"
            disabledReason={fixed ? `Claimed by ${claimant}. Steal the claim to change its labels.` : undefined}
            onChange={(change) => {
              if (!creating) { void actions.chip(ref, 'labels', change); return }
              setDraft({ ...draft, labels: apply(draft.labels, change) })
            }}
          />
        </MetaGroup>

        <MetaGroup label="Tags">
          <ChipEditor
            name="tag"
            values={creating ? draft.tags : (card?.tags ?? [])}
            placeholder="Tag"
            disabledReason={fixed ? `Claimed by ${claimant}. Steal the claim to change its tags.` : undefined}
            onChange={(change) => {
              if (!creating) { void actions.chip(ref, 'tags', change); return }
              setDraft({ ...draft, tags: apply(draft.tags, change) })
            }}
          />
        </MetaGroup>

        {!creating && card && (
          <MetaGroup label="Relations" count={card.relations?.length || undefined}>
            <RelationsEditor
              relations={card.relations ?? []}
              cards={cardOptions.filter((option) => option.ref !== card.ref)}
              base={base}
              disabledReason={fixed ? `Claimed by ${claimant}. Steal the claim to change its relations.` : undefined}
              onAdd={(relation) => actions.relate(ref, relation)}
              onRemove={(relation) => actions.unrelate(ref, relation)}
            />
          </MetaGroup>
        )}

        {!creating && card && (
          <MetaGroup label="Details">
            <MetaFacts
              facts={[
                { label: 'Ref', value: card.ref, mono: true },
                { label: 'Version', value: `v${card.version}` },
                { label: 'Comments', value: String(comments.length) },
                ...(card.created_at !== undefined ? [{ label: 'Created', value: stamp(card.created_at) }] : []),
                ...(card.updated_at !== undefined ? [{ label: 'Updated', value: stamp(card.updated_at) }] : []),
              ]}
            />
          </MetaGroup>
        )}

        {!creating && card && (
          <MetaGroup label="Claim">
            {mine ? (
              <>
                <p className="flex items-center gap-2 text-sm">
                  <Lamp state="claimed" />
                  Claimed by you
                </p>
                {card.claim_until !== undefined && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {expired ? `Expired ${ago(card.claim_until, now)}` : `Runs out ${until(card.claim_until, now)}`}
                  </p>
                )}
                <Button size="sm" variant="outline" className="mt-3 w-full" onClick={() => void actions.release(ref)}>
                  Release it
                </Button>
              </>
            ) : locked ? (
              <>
                <p className="flex items-center gap-2 text-sm">
                  <Lamp state="claimed" />
                  Claimed by <span className="text-meta text-claimed">{claimant}</span>
                </p>
                {stealing ? (
                  <form
                    className="mt-3 flex flex-col gap-2"
                    onSubmit={async (event) => {
                      event.preventDefault()
                      if (!reason.trim()) return
                      await actions.steal(ref, reason.trim())
                      setStealing(false)
                      setReason('')
                    }}
                  >
                    <Input
                      aria-label="Reason for stealing the claim"
                      placeholder="Why you are taking it"
                      value={reason}
                      autoFocus
                      onChange={(event) => setReason(event.target.value)}
                    />
                    <div className="flex gap-2">
                      <Button type="submit" size="sm" disabled={!reason.trim() || saving}>Steal it</Button>
                      <Button type="button" size="sm" variant="ghost" onClick={() => setStealing(false)}>Cancel</Button>
                    </div>
                  </form>
                ) : (
                  <Button size="sm" variant="outline" className="mt-3 w-full" onClick={() => setStealing(true)}>
                    Steal the claim
                  </Button>
                )}
              </>
            ) : (
              <>
                <p className="text-sm text-muted-foreground">
                  {expired
                    ? `The claim ${shortActor(card.claimed_by)} had expired ${ago(card.claim_until ?? 0, now)}.`
                    : 'Not claimed. Any agent can claim it.'}
                </p>
                <Button size="sm" variant="outline" className="mt-3 w-full" onClick={() => void actions.claim(ref)}>
                  Claim it
                </Button>
              </>
            )}
          </MetaGroup>
        )}

        {/* What this card leans on, and what is filed with it. Both apply at
            once, like the facts above them. */}
        {!creating && card && (
          <MetaGroup label="Entries" count={card.links?.length || undefined}>
            <EntryLinksEditor
              links={card.links ?? []}
              entries={entryOptions}
              projectKey={projectKey}
              disabledReason={fixed ? `Claimed by ${claimant}. Steal the claim to change what it cites.` : undefined}
              onAdd={(target) => actions.link(ref, target)}
              onRemove={(target) => actions.unlink(ref, target)}
            />
          </MetaGroup>
        )}

        {!creating && card && (
          <MetaGroup label="Files" count={card.artifacts?.length || undefined}>
            <ArtifactsEditor
              artifacts={card.artifacts ?? []}
              stored={storedArtifacts}
              disabledReason={fixed ? `Claimed by ${claimant}. Steal the claim to change its files.` : undefined}
              onUpload={(file) => actions.upload(ref, file)}
              onLink={(name) => actions.linkArtifact(ref, name)}
              onRemove={(name) => actions.unlinkArtifact(ref, name)}
            />
          </MetaGroup>
        )}

      </MetaPanel>

    </div>
  )
}
