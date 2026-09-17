import type { ReactNode } from 'react'
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
import { Spinner } from '@/components/ui/spinner'

/**
 * Asking before something that cannot be taken back. The question is the
 * title, the description says what goes and what stays, and the confirming
 * button names the act. While the act runs, neither button can be pressed
 * and the dialog cannot be dismissed.
 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirm,
  cancel = 'Cancel',
  busy = false,
  onConfirm,
  onOpenChange,
}: {
  open: boolean
  title: ReactNode
  description: ReactNode
  /** The confirming button's words: "Delete template", not "OK". */
  confirm: string
  cancel?: string
  busy?: boolean
  onConfirm: () => void
  onOpenChange: (open: boolean) => void
}) {
  return (
    <AlertDialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
      <AlertDialogContent className="gap-5 p-6">
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription className="text-pretty">{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter className="mx-0 mb-0 border-0 bg-transparent p-0">
          <AlertDialogCancel variant="ghost" disabled={busy}>{cancel}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={busy} onClick={onConfirm}>
            {busy && <Spinner data-icon="inline-start" />}
            {confirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
