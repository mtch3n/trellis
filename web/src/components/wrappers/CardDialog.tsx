import { useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Maximize2, Pencil, X } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import type { CardComment, CardEvent } from '@/components/wrappers/CardTimeline'
import { IconButton } from '@/components/wrappers/IconButton'
import { EditActions } from '@/components/wrappers/EditInPlace'
import { CardMenu } from '@/components/wrappers/CardMenu'
import type { CardOption } from '@/components/wrappers/RelationsEditor'
import {
  CardView,
  type CardDraft,
  type CardEdit,
  type CardInfo,
  type CardMode,
} from '@/components/wrappers/CardView'
import type { Artifact } from '@/components/wrappers/ArtifactList'
import type { EntryOption } from '@/components/wrappers/EntryLinksEditor'
import type { CardActions } from '@/lib/card-actions'
import { shortActor } from '@/lib/cards'

/**
 * A card, read without leaving the board.
 *
 * The dialog is the quick look. One row of actions sits above the card and
 * never scrolls: the ref, then Edit (or Save and Cancel in its place), more
 * actions, the card's own page, and close. Expanding does not grow the box, it
 * opens the card's own page, which keeps the app shell and gives the card a
 * real URL.
 *
 * The box hangs from a fixed point near the top rather than from the centre,
 * so when an edit grows the card, the card grows downward and nothing already
 * on screen moves.
 */
export function CardDialog({
  open,
  card,
  comments,
  events,
  columns,
  currentColumn,
  startIn = 'read',
  saving,
  onOpenChange,
  onSave,
  onCreate,
  actions,
  labelOptions,
  entryOptions,
  storedArtifacts,
  onDelete,
  onArchive,
  onHistory,
  onGraph,
  me,
  cardOptions,
}: {
  open: boolean
  card: CardInfo | null
  comments: CardComment[]
  events: CardEvent[]
  columns: string[]
  currentColumn?: string
  startIn?: CardMode
  saving: boolean
  onOpenChange: (open: boolean) => void
  /** Resolves true when saved; the dialog then returns to reading the card. */
  onSave: (edit: CardEdit) => Promise<boolean>
  onCreate: (draft: CardDraft) => Promise<void>
  /** Every write this card supports, shared with the board and the card page. */
  actions: CardActions
  /** The labels this project defines. */
  labelOptions: string[]
  /** The entries this card could cite. */
  entryOptions: EntryOption[]
  /** Every artifact the project holds, for linking one that is already here. */
  storedArtifacts?: Artifact[]
  onDelete: () => Promise<boolean>
  onArchive: (archived: boolean) => Promise<boolean>
  onHistory: () => void
  /** Opens the graph walked from this card. */
  onGraph: () => void
  /** Who the server writes as; a card this person holds stays editable. */
  me?: string
  /** The board's cards, to relate this one to. */
  cardOptions: CardOption[]
}) {
  const [mode, setMode] = useState<CardMode>(startIn)
  const [source, setSource] = useState(false)
  const navigate = useNavigate()
  const { projectKey } = useParams<{ projectKey: string }>()

  const changeMode = (next: CardMode) => { setMode(next); setSource(false) }

  // Each opening starts from startIn. Adjusted while rendering, so the first
  // frame of a reopened dialog is already in the right mode.
  const [opened, setOpened] = useState({ open, startIn })
  if (opened.open !== open || opened.startIn !== startIn) {
    setOpened({ open, startIn })
    if (open) changeMode(startIn)
  }

  const locked = Boolean(card?.claimed_by) && card?.claimed_by !== me
  // Reading, focus lands on the scrolling body itself, which moves nothing
  // and lets the arrow keys scroll. Writing, it lands in the title.
  const scroller = useRef<HTMLDivElement>(null)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        initialFocus={() =>
          mode === 'read'
            ? scroller.current
            : (scroller.current?.querySelector<HTMLElement>('[aria-label="Card title"]') ?? scroller.current)
        }
        className="top-[6dvh] flex max-h-[88dvh] translate-y-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl"
      >
        <DialogTitle className="sr-only">
          {mode === 'create' ? 'New card' : (card?.ref ?? 'Card')}
        </DialogTitle>
        <DialogDescription className="sr-only">
          {mode === 'create' ? 'Create a card on this board' : `Card ${card?.ref}`}
        </DialogDescription>

        {/* One row of actions that never scrolls away. Save and Cancel take
            Edit's place, so starting an edit moves nothing. */}
        <header className="flex h-14 shrink-0 items-center gap-3 pr-4 pl-10">
          {mode === 'create' ? (
            <span className="text-sm text-muted-foreground">New card</span>
          ) : (
            <span className="text-meta text-muted-foreground">{card?.ref}</span>
          )}
          <div className="ml-auto flex items-center gap-1.5">
            {mode === 'read' && card && (
              <Button
                variant="outline"
                size="sm"
                disabled={locked}
                onClick={() => changeMode('edit')}
                title={locked ? 'Claimed by an agent. Steal the claim to edit.' : undefined}
              >
                <Pencil data-icon="inline-start" />
                Edit
              </Button>
            )}
            {mode === 'edit' && (
              <EditActions form="card-form" source={source} onSourceChange={setSource} label="Save card" saving={saving} onCancel={() => changeMode('read')} />
            )}
            {card && mode !== 'create' && (
              <CardMenu
                cardRef={card.ref}
                href={`/p/${projectKey}/card/${encodeURIComponent(card.ref)}`}
                archived={Boolean(card.archived_at)}
                disabledReason={card.claimed_by ? `Claimed by ${shortActor(card.claimed_by)}. Steal the claim first.` : undefined}
                onDelete={onDelete}
                onArchive={onArchive}
                onHistory={onHistory}
                onGraph={onGraph}
              />
            )}
            {card && (
              <IconButton
                label="Open as a page"
                onClick={() => {
                  onOpenChange(false)
                  navigate(`/p/${projectKey}/card/${encodeURIComponent(card.ref)}`)
                }}
              >
                <Maximize2 />
              </IconButton>
            )}
            <IconButton label="Close" onClick={() => onOpenChange(false)}>
              <X />
            </IconButton>
          </div>
        </header>

        <div ref={scroller} tabIndex={-1} className="min-h-0 flex-1 overflow-y-auto px-10 pt-2 pb-10 outline-none">
          <CardView
            card={card}
            comments={comments}
            events={events}
            columns={columns}
            currentColumn={currentColumn}
            mode={mode}
            saving={saving}
            source={source}
            onModeChange={changeMode}
            onSave={async (edit) => { if (await onSave(edit)) changeMode('read') }}
            onCreate={onCreate}
            actions={actions}
            labelOptions={labelOptions}
            entryOptions={entryOptions}
            storedArtifacts={storedArtifacts}
            me={me}
            base={`/p/${projectKey}`}
            cardOptions={cardOptions}
          />
          {/* A new card is a form to finish, so its commit sits at its end. */}
          {mode === 'create' && (
            <div className="mt-8 flex justify-end">
              <EditActions form="card-form" source={source} onSourceChange={setSource} label="Create card" saving={saving} />
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
