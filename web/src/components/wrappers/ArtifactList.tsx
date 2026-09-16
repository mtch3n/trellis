import { useEffect, useRef, useState, type ReactNode } from 'react'
import { AudioLines, Download, FileArchive, FileCode, FileQuestion, FileText, Film, Image, Paperclip, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { IconButton } from '@/components/wrappers/IconButton'
import { bytes, sentence } from '@/lib/format'

export type ArtifactKind = 'image' | 'audio' | 'video' | 'document' | 'text' | 'archive'

/** A file attached to an entry, as the knowledge list returns it. */
export interface Artifact {
  name: string
  kind?: ArtifactKind
  mime?: string
  size?: number
  url?: string
  /** The entry names it, but no such artifact exists. Only `name` is set. */
  missing?: boolean
}

const ICON: Record<ArtifactKind, ReactNode> = {
  image: <Image data-icon="inline-start" />,
  audio: <AudioLines data-icon="inline-start" />,
  video: <Film data-icon="inline-start" />,
  document: <FileText data-icon="inline-start" />,
  text: <FileCode data-icon="inline-start" />,
  archive: <FileArchive data-icon="inline-start" />,
}

/** Text previews stop here; a log file can be enormous. */
const TEXT_LIMIT = 256 * 1024

/**
 * An entry's attachments, for the facts column. A row opens the preview; an
 * archive has nothing to preview, so its row downloads instead. A name the
 * entry mentions but nobody attached reads as a broken reference, not as an
 * error.
 */
export function ArtifactList({ artifacts }: { artifacts: Artifact[] }) {
  const [open, setOpen] = useState<Artifact | null>(null)

  return (
    <>
      <ul className="-mx-2 flex flex-col">
        {artifacts.map((artifact) => {
          const icon = artifact.kind ? ICON[artifact.kind] : <Paperclip data-icon="inline-start" />
          if (artifact.missing || !artifact.url) {
            return (
              <li
                key={artifact.name}
                className="flex items-center gap-2 px-2 py-1.5 text-sm text-muted-foreground"
                title="The entry names this file, but it is not attached"
              >
                <FileQuestion className="size-4 shrink-0" />
                <span className="min-w-0 flex-1 truncate line-through decoration-muted-foreground/50">{artifact.name}</span>
                <span className="shrink-0 text-xs">Missing</span>
              </li>
            )
          }
          const row = (
            <>
              {icon}
              <span className="min-w-0 flex-1 truncate text-left">{artifact.name}</span>
              {artifact.size !== undefined && (
                <span className="shrink-0 text-xs text-muted-foreground">{bytes(artifact.size)}</span>
              )}
            </>
          )
          return (
            <li key={artifact.name}>
              {artifact.kind === 'archive' ? (
                <Button
                  variant="ghost"
                  className="w-full justify-start gap-2 px-2 font-normal"
                  render={<a href={artifact.url} download={artifact.name} />}
                  nativeButton={false}
                >
                  {row}
                </Button>
              ) : (
                <Button
                  variant="ghost"
                  className="w-full justify-start gap-2 px-2 font-normal"
                  onClick={() => setOpen(artifact)}
                >
                  {row}
                </Button>
              )}
            </li>
          )
        })}
      </ul>

      <ArtifactPreview artifact={open} onOpenChange={(next) => { if (!next) setOpen(null) }} />
    </>
  )
}

/**
 * One attachment, previewed the way its kind can be shown without running it.
 * Artifacts are arbitrary files served from this origin, so an image is only
 * ever an `<img>` (an SVG there cannot run script), text is only ever text,
 * and media loads its metadata, not the whole recording.
 */
export function ArtifactPreview({
  artifact,
  onOpenChange,
}: {
  artifact: Artifact | null
  onOpenChange: (open: boolean) => void
}) {
  // Focus opens on the preview itself, not on the first button.
  const body = useRef<HTMLDivElement>(null)
  return (
    <Dialog open={artifact !== null} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        initialFocus={body}
        className="flex max-h-[92dvh] w-[92vw] max-w-4xl flex-col gap-0 p-0 sm:max-w-4xl"
      >
        <header className="flex h-14 shrink-0 items-center gap-4 px-5">
          <div className="min-w-0 flex-1">
            <DialogTitle className="truncate">{artifact?.name}</DialogTitle>
            <DialogDescription className="flex gap-3 text-xs">
              {artifact?.kind && <span>{sentence(artifact.kind)}</span>}
              {artifact?.size !== undefined && <span>{bytes(artifact.size)}</span>}
            </DialogDescription>
          </div>
          {artifact?.url && (
            <Button variant="outline" size="sm" render={<a href={artifact.url} download={artifact.name} />} nativeButton={false}>
              <Download data-icon="inline-start" />
              Download
            </Button>
          )}
          <IconButton label="Close" onClick={() => onOpenChange(false)}>
            <X />
          </IconButton>
        </header>
        <div ref={body} tabIndex={-1} className="min-h-0 flex-1 overflow-auto bg-card p-5 outline-none">
          {artifact?.url && <PreviewBody artifact={artifact} url={artifact.url} />}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function PreviewBody({ artifact, url }: { artifact: Artifact; url: string }) {
  switch (artifact.kind) {
    case 'image':
      return <img src={url} alt={artifact.name} className="mx-auto max-h-[72dvh] max-w-full object-contain" />
    case 'audio':
      return <audio controls preload="metadata" src={url} className="w-full" />
    case 'video':
      return <video controls preload="metadata" src={url} className="mx-auto max-h-[72dvh] w-full" />
    case 'document':
      // A same-origin frame runs whatever it loads, so it loads only when two
      // independent fields agree the file is a PDF. The server's own headers
      // are the first guard; this is the second. No sandbox: browsers refuse
      // to run their PDF viewer inside one.
      if (artifact.mime !== 'application/pdf') return <NoPreview />
      return (
        <iframe
          src={url}
          title={artifact.name}
          referrerPolicy="no-referrer"
          loading="lazy"
          className="h-[72dvh] w-full bg-background"
        />
      )
    case 'text':
      return <TextPreview url={url} />
    default:
      return <NoPreview />
  }
}

function NoPreview() {
  return (
    <p className="py-10 text-center text-sm text-muted-foreground">
      There is no preview for this file. Download it to open it.
    </p>
  )
}

/**
 * A text artifact, fetched only once the preview is open, shown as text and
 * nothing else, and capped so a large log does not stall the page.
 */
function TextPreview({ url }: { url: string }) {
  const [state, setState] = useState<{ text: string; clipped: boolean } | { error: string } | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    fetch(url, { signal: controller.signal, headers: { Range: `bytes=0-${TEXT_LIMIT - 1}` } })
      .then(async (response) => {
        if (!response.ok) throw new Error(`The file could not be read (${response.status}).`)
        const text = await response.text()
        const total = Number(response.headers.get('Content-Range')?.split('/')[1] ?? text.length)
        setState({ text, clipped: response.status === 206 && total > TEXT_LIMIT })
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setState({ error: err instanceof Error ? err.message : 'The file could not be read.' })
      })
    return () => controller.abort()
  }, [url])

  if (!state) {
    return (
      <div className="flex flex-col gap-2">
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    )
  }
  if ('error' in state) return <p className="text-sm text-muted-foreground">{state.error}</p>
  return (
    <>
      <pre className="text-meta leading-relaxed whitespace-pre-wrap break-words">{state.text}</pre>
      {state.clipped && (
        <p className="mt-4 text-xs text-muted-foreground">Showing the first {bytes(TEXT_LIMIT)}. Download the file for the rest.</p>
      )}
    </>
  )
}
