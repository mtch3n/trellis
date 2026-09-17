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
 * A card's secondary actions, behind one button in its action row: copying a
 * link to the card, and deleting it. Delete is confirmed and names the card,
 * and while an agent holds the card it is disabled with the reason shown. The
 * caller reports success or failure; the confirmation closes only on success.
 */
export function CardMenu({ cardRef, href, deleteDisabledReason, onDelete }: {
  cardRef: string
  /** The card page's path inside the app, e.g. /p/KEY/card/KEY-1. */
  href: string
  /** Set when the card cannot be deleted right now, e.g. an agent holds it. */
  deleteDisabledReason?: string
  onDelete: () => Promise<boolean>
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
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            disabled={Boolean(deleteDisabledReason)}
            onClick={() => setConfirming(true)}
          >
            Delete card
          </DropdownMenuItem>
          {deleteDisabledReason && (
            <p className="px-2 pb-1.5 text-xs text-muted-foreground">
              {deleteDisabledReason}
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
