import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, errorText, vaultPath, type Nomination } from '@/lib/api'
import { notifyDone, notifyError } from '@/lib/notify'
import { moment, when } from '@/lib/time'
import { useLoad } from '@/lib/use-load'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/wrappers/PageHeader'

function NominationRow({ projectKey, nomination, onDone }: { projectKey: string; nomination: Nomination; onDone: () => void }) {
  const [reason, setReason] = useState('')

  const run = async (action: () => Promise<unknown>, done: string) => {
    try {
      await action()
      notifyDone(done)
      onDone()
    } catch (err) {
      notifyError(err)
    }
  }

  return (
    <li className="flex flex-col gap-3 rounded-lg border p-4">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <Link to={vaultPath(projectKey, nomination.slug)} className="font-medium hover:underline">{nomination.title}</Link>
        <span className="font-mono text-xs text-muted-foreground">{nomination.slug}</span>
        <span className="ml-auto text-xs text-muted-foreground">
          {nomination.nominations} {nomination.nominations === 1 ? 'nomination' : 'nominations'} · cited {nomination.cited} · pinned {nomination.pinned}
        </span>
      </div>
      <p className="text-sm">
        <span className="text-muted-foreground">{nomination.actor}, {when(nomination.created_at)}: </span>
        {nomination.reason}
      </p>
      <form
        className="flex gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          run(() => api.promote(projectKey, nomination.slug, reason.trim()), `Promoted ${nomination.slug}`)
        }}
      >
        <Input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Reason to promote" aria-label={`Reason to promote ${nomination.slug}`} />
        <Button type="submit" disabled={!reason.trim()}>Promote</Button>
        <Button type="button" variant="outline" onClick={() => run(() => api.demote(projectKey, nomination.slug), `Demoted ${nomination.slug}`)}>
          Demote
        </Button>
      </form>
    </li>
  )
}

export function ReviewPage() {
  const { projectKey = '' } = useParams()
  const nominations = useLoad(() => api.nominations(projectKey), [projectKey])
  const unverified = useLoad(() => api.unverified(projectKey), [projectKey])

  const run = async (action: () => Promise<unknown>, done: string) => {
    try {
      await action()
      notifyDone(done)
      unverified.reload()
    } catch (err) {
      notifyError(err)
    }
  }

  return (
    <>
      <PageHeader title="Review" description="Promote nominated project entries to the org, and verify global entries past their verify date." />
      <section className="mb-10 flex flex-col gap-3" aria-label="Nominations">
        <h2 className="text-sm font-medium">Nominations</h2>
        {nominations.loading && !nominations.data && <Spinner />}
        {Boolean(nominations.error) && <p className="text-sm text-danger">{errorText(nominations.error)}</p>}
        {nominations.data?.length === 0 && <p className="text-sm text-muted-foreground">No entry waits for a promote.</p>}
        <ul className="flex flex-col gap-3">
          {nominations.data?.map((nomination) => (
            <NominationRow key={nomination.slug} projectKey={projectKey} nomination={nomination} onDone={nominations.reload} />
          ))}
        </ul>
      </section>
      <section className="flex flex-col gap-3" aria-label="Unverified">
        <h2 className="text-sm font-medium">Unverified global entries</h2>
        {unverified.loading && !unverified.data && <Spinner />}
        {Boolean(unverified.error) && <p className="text-sm text-danger">{errorText(unverified.error)}</p>}
        {unverified.data?.length === 0 && <p className="text-sm text-muted-foreground">Every global entry is verified.</p>}
        <ul className="flex flex-col gap-3">
          {unverified.data?.map((entry) => (
            <li key={entry.id} className="flex flex-wrap items-center gap-3 rounded-lg border p-4">
              <Link to={vaultPath(projectKey, entry.slug)} className="font-medium hover:underline">{entry.title}</Link>
              <span className="text-xs text-muted-foreground">verify by {moment(entry.verify_by)}</span>
              <div className="ml-auto flex gap-2">
                <Button onClick={() => run(() => api.verify(projectKey, entry.slug), `Verified ${entry.slug}`)}>Verify</Button>
                <Button variant="outline" onClick={() => run(() => api.demote(projectKey, entry.slug), `Demoted ${entry.slug}`)}>Demote</Button>
              </div>
            </li>
          ))}
        </ul>
      </section>
    </>
  )
}
