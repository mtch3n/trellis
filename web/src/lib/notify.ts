import { toast } from '@/components/ui/toast'
import { errorText } from '@/lib/api'

export function notifyError(err: unknown) {
  toast.add({ title: errorText(err), type: 'error' })
}

export function notifyDone(title: string) {
  toast.add({ title, type: 'success' })
}
