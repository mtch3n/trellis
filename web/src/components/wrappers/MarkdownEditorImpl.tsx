import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import {
  Editor,
  rootCtx,
  defaultValueCtx,
  editorViewCtx,
  editorViewOptionsCtx,
  remarkStringifyOptionsCtx,
} from '@milkdown/kit/core'
import { commonmark } from '@milkdown/kit/preset/commonmark'
import { gfm } from '@milkdown/kit/preset/gfm'
import { history } from '@milkdown/kit/plugin/history'
import { listener, listenerCtx } from '@milkdown/kit/plugin/listener'
import { Selection } from '@milkdown/kit/prose/state'
import { Textarea } from '@/components/ui/textarea'

/**
 * Markdown editing, in place: rich text or the markdown source, chosen by the
 * caller, which owns the toggle so it can sit with the other edit controls
 * instead of pushing the text down a row.
 *
 * Built from Milkdown's kit rather than the batteries-included Crepe bundle:
 * Crepe eagerly imports a CodeMirror language mode per syntax it can highlight,
 * which was four megabytes of dist for a nicety, and this UI ships inside the
 * Go binary via `go:embed`. commonmark plus gfm is the grammar the vaowledge
 * base actually uses, and markdown shortcuts (`## `, `- `, `> `) still work.
 *
 * The source toggle is not a nicety either. Markdown files are the source of
 * truth here, and a ProseMirror round-trip normalises what it parses: a
 * four-space indented code block comes back fenced, and list and emphasis
 * markers settle on one spelling. Identical meaning, different bytes. Anyone
 * who cares about the exact bytes edits the source.
 *
 * `getMarkdown` is read on demand rather than on every keystroke, so typing
 * does not re-render the form around it.
 */
/**
 * Put wikilinks back the way they were written. The markdown serializer
 * escapes square brackets, so `[[slug]]` would be saved as `\[\[slug]]`: plain
 * text, and one link fewer in the vault. Links are how entries find each
 * other, so the rich view must not quietly unmake them. Escapes inside the
 * link (an `_` in a slug, the `|` before an alias) are undone too.
 */
function restoreWikilinks(markdown: string) {
  return markdown.replace(/\\\[\\\[((?:\\.|[^\]\n])+?)\\?\]\\?\]/g, (_, inner: string) => `[[${inner.replace(/\\(.)/g, '$1')}]]`)
}

export function MarkdownEditorImpl({
  value,
  source,
  onRead,
  placeholder,
  minHeight = 'min-h-64',
  autoFocus = false,
}: {
  value: string
  /** Show the markdown source instead of rich text. */
  source: boolean
  /** Registers a getter the parent calls when it is ready to save. */
  onRead: (read: () => string) => void
  placeholder?: string
  minHeight?: string
  /** Put the caret at the end of the body once the editor is up. */
  autoFocus?: boolean
}) {
  const [raw, setRaw] = useState(value)
  const host = useRef<HTMLDivElement>(null)
  const latest = useRef(value)
  const shown = useRef(source)
  // Focus is for the editor's first appearance only, not for every flip
  // between rich text and source.
  const focusOnCreate = useRef(autoFocus)

  // When the view flips, the surface about to mount starts from the text the
  // other one holds. A layout effect, so the stale text never paints, and
  // declared first, so the editor below is created from the synced text.
  useLayoutEffect(() => {
    if (shown.current === source) return
    shown.current = source
    if (source) setRaw(latest.current)
    else latest.current = raw
  }, [source, raw])

  useEffect(() => {
    onRead(() => (source ? raw : latest.current))
  }, [onRead, source, raw])

  useEffect(() => {
    if (source || !host.current) return
    const root = host.current
    let editor: Editor | null = null
    let destroyed = false

    void Editor.make()
      .config((ctx) => {
        ctx.set(rootCtx, root)
        ctx.set(defaultValueCtx, latest.current)
        ctx.update(editorViewOptionsCtx, (prev) => ({
          ...prev,
          attributes: {
            // Same typeset the reader gets, so the editor is the document.
            class: 'typeset typeset-notes milkdown-prose',
            'aria-label': placeholder ?? 'Markdown body',
            'data-placeholder': placeholder ?? '',
          },
        }))
        // Write lists and rules the way the files and templates already do.
        // The serializer's defaults, `*` for both, would rewrite every one
        // on the first rich-text save.
        ctx.update(remarkStringifyOptionsCtx, (prev) => ({ ...prev, bullet: '-' as const, rule: '-' as const }))
        ctx.get(listenerCtx).markdownUpdated((_ctx, markdown) => { latest.current = restoreWikilinks(markdown) })
      })
      .use(commonmark)
      .use(gfm)
      .use(history)
      .use(listener)
      .create()
      .then((instance) => {
        if (destroyed) { void instance.destroy(); return }
        editor = instance
        if (!focusOnCreate.current) return
        focusOnCreate.current = false
        instance.action((ctx) => {
          const view = ctx.get(editorViewCtx)
          view.dispatch(view.state.tr.setSelection(Selection.atEnd(view.state.doc)))
          view.focus()
        })
      })

    return () => {
      destroyed = true
      void editor?.destroy()
    }
  }, [source, placeholder])

  if (source) {
    return (
      <div className={cn('edit-surface flex', minHeight)}>
        <Textarea
          aria-label="Markdown source"
          placeholder={placeholder}
          className="min-h-0 flex-1 resize-none rounded-none border-0 bg-transparent p-0 text-meta leading-relaxed shadow-none focus-visible:ring-0 md:text-meta dark:bg-transparent"
          value={raw}
          onChange={(event) => setRaw(event.target.value)}
        />
      </div>
    )
  }

  // The same typeset the reader gets, on a wash that says it takes input. The
  // whole surface takes a click, and an empty body says what belongs in it.
  return (
    <div
      ref={host}
      className={cn('trellis-prosemirror edit-surface flex flex-col', minHeight)}
      onPointerDown={(event) => {
        if (event.target !== event.currentTarget) return
        event.preventDefault()
        event.currentTarget.querySelector<HTMLElement>('.milkdown-prose')?.focus()
      }}
    />
  )
}
