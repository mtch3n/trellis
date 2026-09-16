import { useState } from 'react'
import { Ellipsis, Link2, Trash2 } from 'lucide-react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Spinner } from '@/components/ui/spinner'
import { toast } from '@/components/ui/toast'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

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
            <Link2 />
            Copy link
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            disabled={Boolean(deleteDisabledReason)}
            onClick={() => setConfirming(true)}
          >
            <Trash2 />
            Delete card
          </DropdownMenuItem>
          {deleteDisabledReason && (
            <p className="px-2 pb-1.5 text-xs text-muted-foreground">
              {deleteDisabledReason}
            </p>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirming} onOpenChange={(next) => { if (!busy) setConfirming(next) }}>
        <AlertDialogContent className="gap-5 p-6">
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {cardRef}?</AlertDialogTitle>
            <AlertDialogDescription>
              The card and its notes are removed for good. The activity log keeps a record.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter className="mx-0 mb-0 border-0 bg-transparent p-0">
            <AlertDialogCancel variant="ghost" disabled={busy}>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={busy} onClick={() => void confirm()}>
              {busy ? <Spinner data-icon="inline-start" /> : <Trash2 data-icon="inline-start" />}
              Delete card
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
