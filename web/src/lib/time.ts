import { ago } from '@/lib/format'

export function when(iso: string | undefined) {
  if (!iso) return ''
  const at = Date.parse(iso)
  return Number.isNaN(at) ? iso : ago(at)
}

export function moment(iso: string | undefined) {
  if (!iso) return ''
  const at = new Date(iso)
  return Number.isNaN(at.getTime()) ? iso : at.toLocaleString()
}
