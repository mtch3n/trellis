import { useMemo, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, errorText, vaultPath } from '@/lib/api'
import { notifyDone, notifyError } from '@/lib/notify'
import { useSession } from '@/lib/session'
import { moment, when } from '@/lib/time'
import { useLoad } from '@/lib/use-load'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'
import { PageHeader } from '@/components/wrappers/PageHeader'

const SCOPES = ['personal', 'project', 'global']

interface Draft {
  slug: string
  title: string
  summary: string
  body: string
}

function EntryForm({ initial, withSlug, submitLabel, onSubmit, onCancel }: {
  initial: Draft
  withSlug: boolean
  submitLabel: string
  onSubmit: (draft: Draft) => void
  onCancel: () => void
}) {
  const [draft, setDraft] = useState(initial)
  const set = (field: keyof Draft) => (event: { target: { value: string } }) => setDraft({ ...draft, [field]: event.target.value })
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event: FormEvent) => {
        event.preventDefault()
        onSubmit(draft)
      }}
    >
      <div className="flex flex-col gap-2">
        <Label htmlFor="entry-title">Title</Label>
        <Input id="entry-title" value={draft.title} onChange={set('title')} />
      </div>
      {withSlug && (
        <div className="flex flex-col gap-2">
          <Label htmlFor="entry-slug">Slug (empty: made from the title)</Label>
          <Input id="entry-slug" value={draft.slug} onChange={set('slug')} className="font-mono" />
        </div>
      )}
      <div className="flex flex-col gap-2">
        <Label htmlFor="entry-summary">Summary</Label>
        <Input id="entry-summary" value={draft.summary} onChange={set('summary')} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="entry-body">Body</Label>
        <Textarea id="entry-body" value={draft.body} onChange={set('body')} className="min-h-64 font-mono" />
      </div>
      <div className="flex gap-2">
        <Button type="submit" disabled={!draft.title.trim()}>{submitLabel}</Button>
        <Button type="button" variant="outline" onClick={onCancel}>Cancel</Button>
      </div>
    </form>
  )
}

function EntryView({ projectKey, slug, slugs, onChanged }: { projectKey: string; slug: string; slugs: Set<string>; onChanged: () => void }) {
  const me = useSession()
  const loaded = useLoad(() => api.entry(projectKey, slug), [projectKey, slug])
  const entry = loaded.data
  const [editing, setEditing] = useState(false)
  const [reason, setReason] = useState('')

  if (loaded.loading && !entry) return <Spinner className="mt-8" />
  if (loaded.error) return <p className="mt-8 text-sm text-danger">{errorText(loaded.error)}</p>
  if (!entry) return null

  const share = async (run: () => Promise<unknown>, done: string) => {
    try {
      await run()
      notifyDone(done)
      setReason('')
      loaded.reload()
      onChanged()
    } catch (err) {
      notifyError(err)
    }
  }

  const mine = entry.owner === me.handle

  if (editing) {
    return (
      <EntryForm
        initial={{ slug: entry.slug, title: entry.title, summary: entry.summary ?? '', body: entry.body ?? '' }}
        withSlug={false}
        submitLabel="Save"
        onCancel={() => setEditing(false)}
        onSubmit={async (draft) => {
          try {
            loaded.set(await api.editEntry(projectKey, entry.slug, {
              title: draft.title, summary: draft.summary, body: draft.body, if_version: entry.version,
            }))
            setEditing(false)
            notifyDone('Saved')
            onChanged()
          } catch (err) {
            notifyError(err)
          }
        }}
      />
    )
  }

  return (
    <article className="flex min-w-0 flex-col gap-4">
      <PageHeader
        title={entry.title}
        description={entry.summary}
        actions={
          <>
            {mine && <Button variant="outline" onClick={() => setEditing(true)}>Edit</Button>}
            {mine && entry.scope === 'personal' && (
              <Button onClick={() => share(() => api.publish(projectKey, entry.slug), 'Published to the project')}>Publish</Button>
            )}
            {entry.scope === 'global' && (
              <Button variant="outline" onClick={() => share(() => api.verify(projectKey, entry.slug), 'Verified')}>Verify</Button>
            )}
            {entry.scope !== 'personal' && (
              <Button variant="outline" onClick={() => share(() => api.demote(projectKey, entry.slug), 'Demoted')}>Demote</Button>
            )}
          </>
        }
      />
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <Badge variant="secondary">{entry.scope}</Badge>
        <span className="font-mono">{entry.slug}</span>
        <span>by {entry.owner}</span>
        <span>updated {when(entry.updated_at)}</span>
        {entry.verify_by && <span>verify by {moment(entry.verify_by)}</span>}
        {(entry.labels ?? []).map((label) => <Badge key={label} variant="outline">{label}</Badge>)}
        {(entry.tags ?? []).map((tag) => <Badge key={tag} variant="outline">#{tag}</Badge>)}
      </div>
      {(entry.warnings ?? []).map((warning) => <p key={warning} className="text-sm text-danger">{warning}</p>)}
      {mine && entry.scope === 'project' && (
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            share(() => api.promote(projectKey, entry.slug, reason.trim()), 'Promoted to the org')
          }}
        >
          <Input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Why the whole org needs this" aria-label="Promote reason" />
          <Button type="submit" variant="outline" disabled={!reason.trim()}>Promote</Button>
        </form>
      )}
      {entry.body ? (
        <MarkdownContent content={entry.body} anchors wikilinks={{ slugs, pathOf: (target) => vaultPath(projectKey, target) }} />
      ) : (
        <p className="text-sm text-muted-foreground">No body.</p>
      )}
    </article>
  )
}

export function VaultPage() {
  const { projectKey = '', '*': slug = '' } = useParams()
  const navigate = useNavigate()
  const entries = useLoad(() => api.entries(projectKey), [projectKey])
  const [creating, setCreating] = useState(false)
  const slugs = useMemo(() => new Set((entries.data ?? []).map((entry) => entry.slug)), [entries.data])

  const create = async (draft: Draft) => {
    try {
      const entry = await api.newEntry(projectKey, {
        title: draft.title.trim(), slug: draft.slug.trim() || undefined, summary: draft.summary, body: draft.body,
      })
      setCreating(false)
      entries.reload()
      notifyDone('Entry created')
      navigate(vaultPath(projectKey, entry.slug))
    } catch (err) {
      notifyError(err)
    }
  }

  return (
    <div className="grid gap-8 pt-4 md:grid-cols-[16rem_1fr]">
      <aside className="flex flex-col gap-4">
        <Button onClick={() => setCreating(true)}>New entry</Button>
        {entries.loading && !entries.data && <Spinner />}
        {Boolean(entries.error) && <p className="text-sm text-danger">{errorText(entries.error)}</p>}
        {SCOPES.map((scope) => {
          const inScope = (entries.data ?? []).filter((entry) => entry.scope === scope)
          if (inScope.length === 0) return null
          return (
            <section key={scope} aria-label={scope}>
              <h2 className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">{scope}</h2>
              <ul className="flex flex-col">
                {inScope.map((entry) => (
                  <li key={entry.id}>
                    <Link
                      to={vaultPath(projectKey, entry.slug)}
                      onClick={() => setCreating(false)}
                      className={cn(
                        'block truncate rounded-md px-2 py-1 text-sm hover:bg-muted',
                        entry.slug === slug && !creating && 'bg-muted font-medium',
                      )}
                    >
                      {entry.title}
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )
        })}
      </aside>
      <div className="min-w-0">
        {creating ? (
          <>
            <PageHeader title="New entry" description="A new entry is personal. Publish it to share it with the project." />
            <EntryForm initial={{ slug: '', title: '', summary: '', body: '' }} withSlug submitLabel="Create" onSubmit={create} onCancel={() => setCreating(false)} />
          </>
        ) : slug ? (
          <EntryView key={slug} projectKey={projectKey} slug={slug} slugs={slugs} onChanged={entries.reload} />
        ) : (
          <PageHeader title="Vault" description="Pick an entry, or write a new one." />
        )}
      </div>
    </div>
  )
}
