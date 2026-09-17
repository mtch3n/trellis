import { useState } from 'react'
import { Ellipsis } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { toast } from '@/components/ui/toast'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/wrappers/ConfirmDialog'

/**
 * A card's secondary actions, behind one button in its action row, the way a
 * tracker keeps them: copy a link to the card, read its history, take it off
 * the board or put it back, and delete it. Delete is confirmed and names the
 * card; while an agent holds the card, the actions the server would refuse are
 * disabled with the reason shown. The caller reports success or failure, so
 * the confirmation closes only once the card is gone.
 */
export function CardMenu({ cardRef, href, archived = false, disabledReason, onDelete, onArchive, onHistory, onGraph }: {
  cardRef: string
  /** The card page's path inside the app, e.g. /p/KEY/card/KEY-1. */
  href: string
  /** The card is off the board, so the action is to restore it. */
  archived?: boolean
  /** Set when another actor's claim stops the card being archived or deleted. */
  disabledReason?: string
  onDelete: () => Promise<boolean>
  /** Archiving takes the card off the board and releases its claim. */
  onArchive?: (archived: boolean) => Promise<boolean>
  /** Opens the card's revisions. Absent where there is nowhere to show them. */
  onHistory?: () => void
  /** Opens the graph walked from this card: what it waits on, and what waits on it. */
  onGraph?: () => void
}) {
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)

  const copyLink = async () => {
    try {
      const url = new URL(href, window.location.origin).href
      await navigator.clipboard.writeText(url)
      toast.add({ title: 'Link copied', type: 'success' })
    } catch {
      toast.add({ title: 'Could not copy the link', type: 'error' })
    }
  }

  const confirm = async () => {
    setBusy(true)
    const deleted = await onDelete()
    setBusy(false)
    if (deleted) setConfirming(false)
  }

  return (
    <>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger
            render={
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="More actions"
                  />
                }
              />
            }
          >
            <Ellipsis />
          </TooltipTrigger>
          <TooltipContent side="bottom">More actions</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuItem onClick={() => void copyLink()}>
            Copy link
          </DropdownMenuItem>
          {onHistory && (
            <DropdownMenuItem onClick={onHistory}>
              History
            </DropdownMenuItem>
          )}
          {onGraph && (
            <DropdownMenuItem onClick={onGraph}>
              Walk the graph
            </DropdownMenuItem>
          )}
          {onArchive && (
            <DropdownMenuItem
              disabled={Boolean(disabledReason)}
              onClick={() => void onArchive(!archived)}
            >
              {archived ? 'Restore to the board' : 'Archive card'}
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            disabled={Boolean(disabledReason)}
            onClick={() => setConfirming(true)}
          >
            Delete card
          </DropdownMenuItem>
          {disabledReason && (
            <p className="px-2 pb-1.5 text-xs text-muted-foreground">
              {disabledReason}
            </p>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <ConfirmDialog
        open={confirming}
        busy={busy}
        title={`Delete ${cardRef}?`}
        description="The card and its comments are removed for good. The event log keeps a record."
        confirm="Delete card"
        onOpenChange={setConfirming}
        onConfirm={() => void confirm()}
      />
    </>
  )
}
