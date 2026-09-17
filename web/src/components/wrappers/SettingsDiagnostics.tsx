import { useCallback, useEffect, useState } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { MetaFacts } from '@/components/wrappers/MetaPanel'
import { Lamp } from '@/components/wrappers/Lamp'
import { readError } from '@/lib/api'
import { sentence } from '@/lib/format'

/** One check, as `trellis doctor` reports it. */
interface Check {
  name: string
  status: 'ok' | 'warn' | 'fail'
  detail: string
  fix?: string
}

interface Version {
  version: string
  commit: string
  date: string
  os: string
  arch: string
  latest?: string
  /** The command that installs the newer release. */
  update?: string
}

/**
 * What `trellis doctor` says, in the browser: the binary, the storage root,
 * the database, the configuration and the search backend, each with the
 * command that fixes it. A warning is something that will surprise someone
 * later; only a failure means something is broken right now.
 *
 * The version sits here too, with the release check as a button rather than a
 * page load, because checking asks GitHub. A newer release is reported as the
 * command to run: a web page must not be able to replace the binary serving
 * it.
 */
export function SettingsDiagnostics() {
  const [checks, setChecks] = useState<Check[] | null>(null)
  const [version, setVersion] = useState<Version | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [checking, setChecking] = useState(false)
  const [releaseError, setReleaseError] = useState<string | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const [report, build] = await Promise.all([
        fetch('/api/doctor', { signal }),
        fetch('/api/version', { signal }),
      ])
      if (!report.ok) throw new Error(await readError(report))
      if (!build.ok) throw new Error(await readError(build))
      setChecks(((await report.json()) as { checks: Check[] }).checks)
      setVersion((await build.json()) as Version)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not run the checks')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const checkRelease = async () => {
    setChecking(true)
    setReleaseError(null)
    try {
      const response = await fetch('/api/version?check=1')
      if (!response.ok) throw new Error(await readError(response))
      setVersion((await response.json()) as Version)
    } catch (err) {
      setReleaseError(err instanceof Error ? err.message : 'Could not reach GitHub')
    } finally {
      setChecking(false)
    }
  }

  const failed = (checks ?? []).filter((check) => check.status === 'fail').length
  const warned = (checks ?? []).filter((check) => check.status === 'warn').length

  return (
    <>
      <PageHeader
        title="Diagnostics"
        description="What this installation looks like, as trellis doctor reports it. A warning works today and will surprise you later; a failure is broken now."
        facts={[
          { label: 'Failing', value: failed, tone: 'danger' },
          { label: 'Warnings', value: warned, tone: 'claimed' },
        ]}
        actions={
          <Button variant="outline" size="sm" onClick={() => void load()}>Run again</Button>
        }
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not run the checks</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {checks === null && !error && <Skeleton className="mt-6 h-48 w-full" />}

      {checks && (
        <ul className="mt-6 flex flex-col">
          {checks.map((check) => (
            <li key={check.name} className="flex flex-wrap items-baseline gap-x-4 gap-y-1 py-2.5">
              <span className="flex w-40 shrink-0 items-center gap-2">
                <Lamp state={check.status === 'fail' ? 'alarm' : check.status === 'warn' ? 'claimed' : 'idle'} />
                <span className="text-sm">{sentence(check.name)}</span>
              </span>
              <span className="min-w-0 flex-1 text-sm text-pretty text-muted-foreground">{check.detail}</span>
              {check.fix && <span className="text-meta text-muted-foreground">{check.fix}</span>}
            </li>
          ))}
        </ul>
      )}

      <Separator className="my-8" />

      <section>
        <h2 className="text-heading">Version</h2>
        {version ? (
          <div className="mt-3 max-w-md">
            <MetaFacts
              facts={[
                { label: 'Running', value: version.version, mono: true },
                { label: 'Built', value: version.date },
                { label: 'Commit', value: version.commit, mono: true },
                { label: 'Platform', value: `${version.os}/${version.arch}`, mono: true },
                ...(version.latest ? [{ label: 'Latest release', value: version.latest, mono: true }] : []),
              ]}
            />
          </div>
        ) : (
          !error && <Skeleton className="mt-3 h-20 w-full max-w-md" />
        )}

        {version?.update ? (
          <p className="mt-4 text-sm text-pretty">
            A newer release is out. Install it from a terminal:{' '}
            <span className="text-meta">{version.update}</span>
          </p>
        ) : version?.latest ? (
          <p className="mt-4 text-sm text-muted-foreground">This is the latest release.</p>
        ) : null}

        {releaseError && <p className="mt-4 text-sm text-destructive">{releaseError}</p>}

        <Button variant="outline" size="sm" className="mt-4" disabled={checking} onClick={() => void checkRelease()}>
          {checking && <Spinner data-icon="inline-start" />}
          Check for a newer release
        </Button>
      </section>
    </>
  )
}
