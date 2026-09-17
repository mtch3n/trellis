import { useRef, useState } from 'react'
import { Paperclip, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Spinner } from '@/components/ui/spinner'
import { ArtifactList, type Artifact } from '@/components/wrappers/ArtifactList'
import { bytes } from '@/lib/format'

/**
 * A card's files, in its facts column. A row previews the file, exactly as an
 * entry's artifacts do, so the two read the same; what a card adds is putting
 * one there: a file chosen from this machine, or one the project already
 * holds, since an artifact belongs to the project and may be cited by several
 * cards. Removing a row unlinks the file and leaves it stored.
 */
export function ArtifactsEditor({
  artifacts,
  stored,
  disabledReason,
  onUpload,
  onLink,
  onRemove,
}: {
  artifacts: Artifact[]
  /** Every artifact the project holds, for linking one that is already here. */
  stored: Artifact[]
  /** Set when the card cannot be changed right now, e.g. an agent holds it. */
  disabledReason?: string
  /** Resolves true when the file was stored and linked. */
  onUpload: (file: File) => Promise<boolean>
  onLink: (name: string) => Promise<boolean>
  onRemove: (name: string) => Promise<void>
}) {
  const picker = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const disabled = Boolean(disabledReason)
  const linked = new Set(artifacts.map((artifact) => artifact.name))
  const rest = stored.filter((artifact) => !linked.has(artifact.name))

  const upload = async (file: File | undefined) => {
    if (!file) return
    setBusy(true)
    await onUpload(file)
    setBusy(false)
  }

  return (
    <div className="flex flex-col gap-3">
      {artifacts.length === 0 && <p className="text-sm text-muted-foreground">No files.</p>}

      {artifacts.length > 0 && (
        <ArtifactList
          artifacts={artifacts}
          removeLabel={disabled ? undefined : 'Remove'}
          onRemove={disabled ? undefined : (artifact) => void onRemove(artifact.name)}
        />
      )}

      <div className="flex flex-wrap items-center gap-2">
        {/* The file input is the browser's own; the button in front of it is
            ours, so it reads like every other control here. */}
        <Input
          ref={picker}
          type="file"
          className="hidden"
          aria-hidden="true"
          tabIndex={-1}
          onChange={(event) => {
            const file = event.target.files?.[0]
            event.target.value = ''
            void upload(file)
          }}
        />
        <Button
          variant="ghost"
          size="xs"
          className="text-muted-foreground"
          disabled={disabled || busy}
          title={disabledReason}
          onClick={() => picker.current?.click()}
        >
          {busy ? <Spinner data-icon="inline-start" /> : <Plus data-icon="inline-start" />}
          Add a file
        </Button>

        {rest.length > 0 && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="xs"
                  className="text-muted-foreground"
                  disabled={disabled || busy}
                  title={disabledReason}
                />
              }
            >
              <Paperclip data-icon="inline-start" />
              Link a stored file
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-64">
              {rest.map((artifact) => (
                <DropdownMenuItem key={artifact.name} onClick={() => void onLink(artifact.name)}>
                  <span className="min-w-0 flex-1 truncate">{artifact.name}</span>
                  {artifact.size !== undefined && (
                    <span className="shrink-0 text-xs text-muted-foreground">{bytes(artifact.size)}</span>
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </div>
  )
}
