import { Suspense, lazy } from 'react'
import { cn } from '@/lib/utils'
import type { MarkdownEditorImpl } from './MarkdownEditorImpl'

const LazyEditor = lazy(() =>
  import('./MarkdownEditorImpl').then((module) => ({ default: module.MarkdownEditorImpl })),
)

/**
 * The editor is loaded only when someone edits.
 *
 * ProseMirror and its markdown pipeline are most of the bundle, and this
 * dashboard is read-first: opening a card or an entry should not parse an
 * editor the reader never opens. While it loads, the field is already there:
 * the same empty wash, so nothing shifts when the text arrives.
 */
export function MarkdownEditor(props: React.ComponentProps<typeof MarkdownEditorImpl>) {
  return (
    <Suspense fallback={<div className={cn('edit-surface', props.minHeight ?? 'min-h-64')} />}>
      <LazyEditor {...props} />
    </Suspense>
  )
}
