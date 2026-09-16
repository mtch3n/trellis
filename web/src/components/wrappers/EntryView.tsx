import { useCallback, useRef, useState, type FormEvent, type MouseEvent } from 'react'
import { Separator } from '@/components/ui/separator'
import { EditForm, InPlaceText } from '@/components/wrappers/EditInPlace'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'
import { MarkdownEditor } from '@/components/wrappers/MarkdownEditor'
import type { KnowledgeEntry } from '@/pages/KnowledgePage'

export interface EntryDraft { title: string; summary: string; body: string }

/**
 * One knowledge entry, read or edited. Content only: the facts about it live in
 * the meta column, the same way a card's do.
 *
 * Editing happens in place. Reading, the title, summary and body show a faint
 * wash when pointed at, and a click on one starts editing it. Editing, they
 * become fields where they stand, in the same type. Edit, Save and Cancel live
 * in the page's action row, so an edit never moves the words.
 */
export function EntryView({
  entry,
  editing,
  source,
  onEditingChange,
  onSave,
}: {
  entry: KnowledgeEntry
  editing: boolean
  /** Show the body as markdown source. The page owns the toggle, which sits with Save. */
  source: boolean
  onEditingChange: (editing: boolean) => void
  onSave: (draft: EntryDraft) => Promise<void>
}) {
  const [draft, setDraft] = useState({ title: entry.title, summary: entry.summary ?? '' })
  const [focus, setFocus] = useState<'title' | 'summary' | 'body'>('title')
  const readBody = useRef<() => string>(() => entry.body ?? '')
  const registerBody = useCallback((read: () => string) => { readBody.current = read }, [])

  // Another entry, or the other mode, starts from what the entry says.
  const [shown, setShown] = useState({ id: entry.id, editing })
  if (shown.id !== entry.id || shown.editing !== editing) {
    setShown({ id: entry.id, editing })
    setDraft({ title: entry.title, summary: entry.summary ?? '' })
    if (!editing) setFocus('title')
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!draft.title.trim()) return
    await onSave({ ...draft, body: readBody.current() })
  }

  // Reading, a click on the words starts editing them, unless it lands on a
  // link or finishes a text selection.
  const startEdit = (target: 'title' | 'summary' | 'body') => (event: MouseEvent<HTMLElement>) => {
    if (editing) return
    if ((event.target as HTMLElement).closest('a')) return
    if (window.getSelection()?.toString()) return
    setFocus(target)
    onEditingChange(true)
  }

  const content = (
    <>
      <header>
        {editing ? (
          <InPlaceText
            label="Title"
            required
            className="text-title text-balance md:text-title"
            value={draft.title}
            focusOnMount={focus === 'title'}
            onChange={(title) => setDraft({ ...draft, title })}
          />
        ) : (
          <h1 className="edit-hint text-title text-balance" onClick={startEdit('title')}>{entry.title}</h1>
        )}
      </header>
      {editing ? (
        <div className="mt-3">
          <InPlaceText
            label="Summary"
            placeholder="One sentence a future session can act on"
            className="text-body text-pretty text-muted-foreground md:text-body"
            value={draft.summary}
            focusOnMount={focus === 'summary'}
            onChange={(summary) => setDraft({ ...draft, summary })}
          />
        </div>
      ) : (
        entry.summary && (
          <div className="mt-3">
            <p className="edit-hint text-body text-pretty text-muted-foreground" onClick={startEdit('summary')}>
              {entry.summary}
            </p>
          </div>
        )
      )}
      <Separator className="my-7" />
      {editing ? (
        <MarkdownEditor
          key={entry.id}
          value={entry.body ?? ''}
          source={source}
          onRead={registerBody}
          placeholder="Write the entry"
          minHeight="min-h-64"
          autoFocus={focus === 'body'}
        />
      ) : (
        <div className="edit-hint" onClick={startEdit('body')}>
          <MarkdownContent content={entry.body ?? ''} />
        </div>
      )}
    </>
  )

  return (
    <article className="min-w-0">
      {editing ? <EditForm id="entry-form" onSubmit={submit}>{content}</EditForm> : content}
    </article>
  )
}
