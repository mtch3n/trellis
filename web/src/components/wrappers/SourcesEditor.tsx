import { useState, type KeyboardEvent } from 'react'
import { Link } from 'react-router-dom'
import { ExternalLink, Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { IconButton } from '@/components/wrappers/IconButton'
import { normalizeSource, sourceTarget } from '@/lib/sources'

/**
 * What an entry's claims rest on, changed where it is read. A source naming
 * something Trellis holds, a card or another entry, opens it; a URL opens in
 * a new tab; a file pointer or a line of prose is shown as written.
 *
 * Adding and removing apply at once, like the other facts beside them. A
 * strict template can refuse either, and a refused source stays in the box
 * so it can be corrected rather than retyped.
 */
export function SourcesEditor({
  sources,
  projectKey,
  onChange,
}: {
  sources: string[]
  projectKey: string
  /** Resolves true when the server kept the new list. */
  onChange: (sources: string[]) => Promise<boolean>
}) {
  const [adding, setAdding] = useState(false)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)

  const add = async () => {
    const source = normalizeSource(typed, projectKey)
    if (!source) { setAdding(false); return }
    if (sources.includes(source)) { setTyped(''); return }
    setBusy(true)
    const kept = await onChange([...sources, source])
    setBusy(false)
    if (kept) setTyped('')
  }

  const type = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      void add()
    } else if (event.key === 'Escape') {
      event.stopPropagation()
      setAdding(false)
      setTyped('')
    }
  }

  return (
    <div className="flex flex-col gap-2">
      {sources.length === 0 && !adding && (
        <p className="text-xs text-muted-foreground">No sources yet.</p>
      )}

      {sources.length > 0 && (
        <ul className="-mx-2 flex flex-col">
          {sources.map((source) => {
            const target = sourceTarget(source, projectKey)
            return (
              <li key={source} className="group/source relative">
                {target.kind === 'route' ? (
                  <Link to={target.to} className="block py-1.5 pr-8 pl-2 text-meta break-all transition-colors hover:bg-accent/50">
                    {target.label}
                  </Link>
                ) : target.kind === 'url' ? (
                  <a
                    href={target.href}
                    target="_blank"
                    rel="noreferrer"
                    className="flex items-baseline gap-1.5 py-1.5 pr-8 pl-2 text-sm break-all transition-colors hover:bg-accent/50"
                  >
                    <span className="min-w-0">{target.label}</span>
                    <ExternalLink className="size-3 shrink-0 translate-y-0.5 text-muted-foreground" />
                  </a>
                ) : (
                  <p className="py-1.5 pr-8 pl-2 text-sm break-words text-muted-foreground">{source}</p>
                )}
                <IconButton
                  label={`Remove source ${source}`}
                  size="icon-xs"
                  side="left"
                  disabled={busy}
                  className="absolute top-1 right-1 opacity-0 transition-opacity group-hover/source:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
                  onClick={() => void onChange(sources.filter((item) => item !== source))}
                >
                  <X />
                </IconButton>
              </li>
            )
          })}
        </ul>
      )}

      {adding ? (
        <Input
          autoFocus
          aria-label="New source"
          placeholder={`${projectKey}-12, [[an-entry]] or https://…`}
          className="h-7"
          spellCheck={false}
          // Read-only rather than disabled while saving, so the cursor stays for the next one.
          readOnly={busy}
          value={typed}
          onChange={(event) => setTyped(event.target.value)}
          onKeyDown={type}
          onBlur={() => { if (!typed.trim()) setAdding(false) }}
        />
      ) : (
        <Button
          variant="ghost"
          size="xs"
          className="self-start text-muted-foreground"
          onClick={() => setAdding(true)}
        >
          <Plus data-icon="inline-start" />
          Add source
        </Button>
      )}
    </div>
  )
}
