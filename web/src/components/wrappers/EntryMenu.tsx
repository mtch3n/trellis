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
 * An entry's secondary actions, behind one button in its action row, the way a
 * card's sit in `CardMenu`: copy a link, read its history, pin it for the
 * sessions that come after, and the lifecycle acts that move it between a
 * project and the global vault. Deleting is last, confirmed, and says that the
 * file goes with the row.
 *
 * Which lifecycle acts appear follows the entry, not the reader: a project
 * entry can be promoted, a global one demoted or verified.
 */
export function EntryMenu({ slug, href, global = false, pinned = false, onDelete, onHistory, onGraph, onPin, onUnpin, onPromote, onDemote, onVerify }: {
  slug: string
  /** The entry's path inside the app, for Copy link. */
  href: string
  /** The entry lives in the global vault rather than in this project. */
  global?: boolean
  pinned?: boolean
  onDelete: () => Promise<boolean>
  onHistory: () => void
  /** Opens the graph walked from this entry. */
  onGraph: () => void
  onPin: () => void
  onUnpin: () => void
  /** Opens the promote dialog, which asks for the name and a reason. */
  onPromote: () => void
  onDemote: () => void
  onVerify: () => void
}) {
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(new URL(href, window.location.origin).href)
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
                render={<Button variant="ghost" size="icon-sm" aria-label="More actions" />}
              />
            }
          >
            <Ellipsis />
          </TooltipTrigger>
          <TooltipContent side="bottom">More actions</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuItem onClick={() => void copyLink()}>Copy link</DropdownMenuItem>
          <DropdownMenuItem onClick={onHistory}>History</DropdownMenuItem>
          <DropdownMenuItem onClick={onGraph}>Walk the graph</DropdownMenuItem>
          {pinned ? (
            <DropdownMenuItem onClick={onUnpin}>Unpin</DropdownMenuItem>
          ) : (
            <DropdownMenuItem onClick={onPin}>Pin for later sessions</DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          {global ? (
            <>
              <DropdownMenuItem onClick={onVerify}>Verify it still holds</DropdownMenuItem>
              <DropdownMenuItem onClick={onDemote}>Demote to its project</DropdownMenuItem>
            </>
          ) : (
            <DropdownMenuItem onClick={onPromote}>Promote to the global vault</DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => setConfirming(true)}>
            Delete entry
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <ConfirmDialog
        open={confirming}
        busy={busy}
        title={`Delete ${slug}?`}
        description="The markdown file is removed with the row. Links to it become stubs, and the event log keeps a record."
        confirm="Delete entry"
        onOpenChange={setConfirming}
        onConfirm={() => void confirm()}
      />
    </>
  )
}
