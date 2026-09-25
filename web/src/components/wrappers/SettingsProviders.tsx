import { useCallback, useEffect, useState } from 'react'
import { Ellipsis } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Dialog, DialogContent, DialogFooter, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { toast } from '@/components/ui/toast'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/wrappers/ConfirmDialog'
import { EffortSelect } from '@/components/wrappers/EffortSelect'
import { PageHeader } from '@/components/wrappers/PageHeader'
import {
  deleteProvider, fetchProviders, providerLabel, saveProvider, setDefaultProvider, testProvider,
  type Provider, type ProviderInput, type ProviderKind, type ProvidersResponse,
} from '@/lib/providers'

/**
 * The providers the chat can talk to: APIs the daemon forwards to with the
 * key it holds, and agent programs it runs on this machine. The key is sent
 * once and never shown again; the table says only whether one is set.
 */
export function SettingsProviders() {
  const [data, setData] = useState<ProvidersResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<Provider | 'new' | null>(null)
  const [removing, setRemoving] = useState<Provider | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      setData(await fetchProviders(signal))
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not read the providers')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const test = async (provider: Provider) => {
    setBusy(provider.id)
    try {
      const result = await testProvider(provider.id)
      toast.add({
        title: result.ok ? `${providerLabel(provider, data?.kinds ?? [])} answered` : 'The provider did not answer',
        description: result.ok ? undefined : result.message,
        type: result.ok ? 'success' : 'error',
      })
    } catch (err) {
      toast.add({ title: 'Could not test the provider', description: err instanceof Error ? err.message : undefined, type: 'error' })
    } finally {
      setBusy(null)
    }
  }

  const makeDefault = async (provider: Provider) => {
    setBusy(provider.id)
    try {
      setData(await setDefaultProvider(provider.id))
    } catch (err) {
      toast.add({ title: 'The default was not changed', description: err instanceof Error ? err.message : undefined, type: 'error' })
    } finally {
      setBusy(null)
    }
  }

  const remove = async () => {
    if (!removing) return
    setBusy(removing.id)
    try {
      setData(await deleteProvider(removing.id))
      setRemoving(null)
    } catch (err) {
      toast.add({ title: 'The provider was not removed', description: err instanceof Error ? err.message : undefined, type: 'error' })
    } finally {
      setBusy(null)
    }
  }

  const kinds = data?.kinds ?? []

  return (
    <>
      <PageHeader
        title="Providers"
        description="The models and agents the chat can use."
        actions={data && data.providers.length > 0 && <Button size="sm" onClick={() => setEditing('new')}>Add provider</Button>}
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not read the providers</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {!data && !error && <Skeleton className="mt-6 h-24 w-full" />}

      {data && data.providers.length === 0 && (
        <Empty className="mt-10">
          <EmptyHeader>
            <EmptyTitle>No providers</EmptyTitle>
            <EmptyDescription>An API such as Azure OpenAI, or an agent on this machine such as Claude Code.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button size="sm" onClick={() => setEditing('new')}>Add provider</Button>
          </EmptyContent>
        </Empty>
      )}

      {data && data.providers.length > 0 && (
        <Table className="mt-6">
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Model</TableHead>
              <TableHead><span className="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.providers.map((provider) => {
              const kind = kinds.find((item) => item.kind === provider.kind)
              return (
                <TableRow key={provider.id} className="cursor-pointer" onClick={() => setEditing(provider)}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{provider.name || provider.id}</span>
                      {data.default_provider === provider.id && <Badge variant="secondary">Default</Badge>}
                      {kind?.needs_key && !provider.has_key && <Badge variant="destructive">No key</Badge>}
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{kind?.label ?? provider.kind}</TableCell>
                  <TableCell className="text-meta text-muted-foreground">{provider.model || 'Agent default'}</TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1" onClick={(event) => event.stopPropagation()}>
                      <Button variant="ghost" size="sm" disabled={busy !== null} onClick={() => void test(provider)}>
                        {busy === provider.id && <Spinner data-icon="inline-start" />}
                        Test
                      </Button>
                      <DropdownMenu>
                        <Tooltip>
                          <TooltipTrigger
                            render={<DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={`More actions for ${provider.name || provider.id}`} />} />}
                          >
                            <Ellipsis />
                          </TooltipTrigger>
                          <TooltipContent side="bottom">More actions</TooltipContent>
                        </Tooltip>
                        <DropdownMenuContent align="end" className="w-48">
                          <DropdownMenuGroup>
                            <DropdownMenuItem onClick={() => setEditing(provider)}>Edit</DropdownMenuItem>
                            <DropdownMenuItem disabled={data.default_provider === provider.id} onClick={() => void makeDefault(provider)}>
                              Make default
                            </DropdownMenuItem>
                          </DropdownMenuGroup>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" onClick={() => setRemoving(provider)}>Remove</DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}

      {editing && (
        <ProviderDialog
          provider={editing === 'new' ? null : editing}
          kinds={kinds}
          taken={data?.providers.map((provider) => provider.id) ?? []}
          onClose={() => setEditing(null)}
          onSaved={(next) => { setData(next); setEditing(null) }}
        />
      )}

      <ConfirmDialog
        open={removing !== null}
        busy={busy !== null}
        title={`Remove ${removing?.name || removing?.id}?`}
        description="Its settings and key are deleted."
        confirm="Remove provider"
        onOpenChange={(next) => { if (!next) setRemoving(null) }}
        onConfirm={() => void remove()}
      />
    </>
  )
}

/** The variable each kind's own tools read a key from, as an example. */
const KEY_VARIABLES: Record<string, string> = {
  openai: 'OPENAI_API_KEY',
  azure: 'AZURE_API_KEY',
  anthropic: 'ANTHROPIC_API_KEY',
  google: 'GOOGLE_GENERATIVE_AI_API_KEY',
}

/** A new provider's id: its name as lowercase words and hyphens, numbered past any already taken. */
function newId(name: string, kind: string, taken: string[]) {
  const base = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 36) || kind
  let id = base
  for (let n = 2; taken.includes(id); n++) id = `${base}-${n}`
  return id
}

/** Adding a provider, or changing one. A new provider picks its kind first. */
function ProviderDialog({ provider, kinds, taken, onClose, onSaved }: {
  provider: Provider | null
  kinds: ProviderKind[]
  taken: string[]
  onClose: () => void
  onSaved: (data: ProvidersResponse) => void
}) {
  const [form, setForm] = useState<ProviderInput>(() => ({
    name: provider?.name ?? '',
    kind: provider?.kind ?? kinds[0]?.kind ?? 'openai',
    base_url: provider?.base_url ?? '',
    model: provider?.model ?? '',
    effort: provider?.effort ?? '',
    api_key_env: provider?.api_key_env ?? '',
    command: provider?.command ?? '',
    dir: provider?.dir ?? '',
  }))
  const [key, setKey] = useState('')
  const [clearKey, setClearKey] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const kind = kinds.find((item) => item.kind === form.kind)
  const id = provider?.id ?? newId(form.name, form.kind, taken)
  const set = (field: keyof ProviderInput) => (event: React.ChangeEvent<HTMLInputElement>) =>
    setForm((current) => ({ ...current, [field]: event.target.value }))

  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      const input: ProviderInput = { ...form }
      if (key.trim()) input.api_key = key.trim()
      else if (clearKey) input.api_key = ''
      onSaved(await saveProvider(id, input))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'The provider was not saved')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(next) => { if (!busy && !next) onClose() }}>
      <DialogContent className="sm:max-w-lg">
        <DialogTitle>{provider ? `Edit ${provider.name || provider.id}` : 'Add provider'}</DialogTitle>

        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="provider-kind">Kind</FieldLabel>
            <Select
              items={kinds.map((item) => ({ value: item.kind, label: item.label }))}
              value={form.kind}
              disabled={provider !== null}
              onValueChange={(value) => { if (value) setForm((current) => ({ ...current, kind: value })) }}
            >
              <SelectTrigger id="provider-kind" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {kinds.map((item) => <SelectItem key={item.kind} value={item.kind}>{item.label}</SelectItem>)}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel htmlFor="provider-name">Name</FieldLabel>
            <Input id="provider-name" value={form.name} autoComplete="off" placeholder={kind?.label} onChange={set('name')} />
          </Field>

          {!kind?.local && (
            <>
              <Field>
                <FieldLabel htmlFor="provider-base-url">Base URL</FieldLabel>
                <Input
                  id="provider-base-url"
                  value={form.base_url}
                  autoComplete="off"
                  placeholder={kind?.default_base_url ?? (form.kind === 'azure' ? 'https://<resource>.openai.azure.com/openai/v1' : 'http://localhost:11434/v1')}
                  onChange={set('base_url')}
                />
                {!kind?.needs_base_url && <FieldDescription>Leave empty for the public endpoint.</FieldDescription>}
              </Field>
              <Field>
                <FieldLabel htmlFor="provider-model">{form.kind === 'azure' ? 'Deployment' : 'Model'}</FieldLabel>
                <Input id="provider-model" value={form.model} autoComplete="off" onChange={set('model')} />
              </Field>
              <Field>
                <FieldLabel htmlFor="provider-key">API key</FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id="provider-key"
                    type="password"
                    value={key}
                    autoComplete="off"
                    placeholder={provider?.has_key && !clearKey ? 'Saved' : ''}
                    onChange={(event) => { setKey(event.target.value); setClearKey(false) }}
                  />
                  {provider?.has_key && !key && !clearKey && (
                    <InputGroupAddon align="inline-end">
                      <InputGroupButton size="xs" onClick={() => setClearKey(true)}>Clear</InputGroupButton>
                    </InputGroupAddon>
                  )}
                </InputGroup>
              </Field>
              <Field>
                <FieldLabel htmlFor="provider-key-env">Key variable</FieldLabel>
                <Input
                  id="provider-key-env"
                  value={form.api_key_env}
                  autoComplete="off"
                  placeholder={KEY_VARIABLES[form.kind] ?? ''}
                  onChange={set('api_key_env')}
                />
                <FieldDescription>Overrides the key when set in the daemon's environment.</FieldDescription>
              </Field>
            </>
          )}

          {kind?.local && (
            <>
              <Field>
                <FieldLabel htmlFor="provider-command">Command</FieldLabel>
                <Input
                  id="provider-command"
                  value={form.command}
                  autoComplete="off"
                  placeholder={kind.default_command}
                  onChange={set('command')}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="provider-model">Model</FieldLabel>
                <Input id="provider-model" value={form.model} autoComplete="off" placeholder="Default" onChange={set('model')} />
              </Field>
              <Field>
                <FieldLabel htmlFor="provider-dir">Working directory</FieldLabel>
                <Input id="provider-dir" value={form.dir} autoComplete="off" placeholder="~" onChange={set('dir')} />
              </Field>
            </>
          )}
          <Field>
            <FieldLabel htmlFor="provider-effort">Default effort</FieldLabel>
            <EffortSelect
              id="provider-effort"
              value={form.effort}
              className="w-full"
              onChange={(effort) => setForm((current) => ({ ...current, effort }))}
            />
          </Field>
        </FieldGroup>

        {error && (
          <Alert variant="destructive">
            <AlertTitle>Not saved</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button variant="ghost" size="sm" disabled={busy} onClick={onClose}>Cancel</Button>
          <Button size="sm" disabled={busy} onClick={() => void submit()}>
            {busy && <Spinner data-icon="inline-start" />}
            {provider ? 'Save provider' : 'Add provider'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
