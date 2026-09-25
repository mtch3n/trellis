import type { LanguageModel } from 'ai'
import { readError } from '@/lib/api'

/** One kind a provider can be, as `GET /api/providers` describes it. */
export interface ProviderKind {
  kind: string
  label: string
  /** Run by the daemon as a program, not reached over an API. */
  local: boolean
  default_base_url?: string
  default_command?: string
  needs_base_url: boolean
  needs_key: boolean
}

/** A configured provider. The key itself never leaves the daemon. */
export interface Provider {
  id: string
  name: string
  kind: string
  base_url: string
  model: string
  /** How hard the model reasons by default: low, medium, high or xhigh. Empty is the model's own. */
  effort: string
  api_key_env: string
  command: string
  dir: string
  has_key: boolean
}

export interface ProvidersResponse {
  file: string
  default_provider: string
  providers: Provider[]
  kinds: ProviderKind[]
  efforts: string[]
}

/** What the editor sends. `api_key` left out keeps the stored key; empty removes it. */
export type ProviderInput = Omit<Provider, 'id' | 'has_key'> & { api_key?: string }

async function answer(response: Response) {
  if (!response.ok) throw new Error(await readError(response))
  return (await response.json()) as ProvidersResponse
}

const JSON_HEADERS = { 'Content-Type': 'application/json' }

export async function fetchProviders(signal?: AbortSignal) {
  return answer(await fetch('/api/providers', { signal }))
}

export async function saveProvider(id: string, input: ProviderInput) {
  return answer(await fetch(`/api/providers/${encodeURIComponent(id)}`, {
    method: 'PUT', headers: JSON_HEADERS, body: JSON.stringify(input),
  }))
}

export async function deleteProvider(id: string) {
  return answer(await fetch(`/api/providers/${encodeURIComponent(id)}`, { method: 'DELETE' }))
}

export async function setDefaultProvider(id: string) {
  return answer(await fetch('/api/providers', {
    method: 'PATCH', headers: JSON_HEADERS, body: JSON.stringify({ default_provider: id }),
  }))
}

export async function testProvider(id: string) {
  const response = await fetch(`/api/providers/${encodeURIComponent(id)}/test`, {
    method: 'POST', headers: JSON_HEADERS, body: '{}',
  })
  if (!response.ok) throw new Error(await readError(response))
  return (await response.json()) as { ok: boolean; message: string }
}

/** A provider's name as a person reads it. */
export function providerLabel(provider: Provider, kinds: ProviderKind[]) {
  if (provider.name) return provider.name
  const kind = kinds.find((item) => item.kind === provider.kind)
  return kind ? `${kind.label}${provider.model ? ` · ${provider.model}` : ''}` : provider.id
}

export function isLocal(provider: Provider, kinds: ProviderKind[]) {
  return kinds.find((item) => item.kind === provider.kind)?.local ?? false
}

/**
 * The AI SDK model for an API provider. Every request goes to the daemon's
 * proxy, which adds the real credential, so the key given here is a
 * placeholder the proxy replaces. Each kind's SDK loads the first time it is
 * used, so the page carries none of them until a chat needs one.
 */
export async function languageModel(provider: Provider): Promise<LanguageModel> {
  const baseURL = `${window.location.origin}/api/providers/${encodeURIComponent(provider.id)}/proxy`
  const apiKey = 'set-by-trellis'
  switch (provider.kind) {
    case 'openai':
      return (await import('@ai-sdk/openai')).createOpenAI({ baseURL, apiKey })(provider.model)
    case 'azure':
      return (await import('@ai-sdk/azure')).createAzure({ baseURL, apiKey })(provider.model)
    case 'anthropic':
      return (await import('@ai-sdk/anthropic')).createAnthropic({ baseURL, apiKey })(provider.model)
    case 'google':
      return (await import('@ai-sdk/google')).createGoogleGenerativeAI({ baseURL, apiKey })(provider.model)
    case 'openai-compatible':
      return (await import('@ai-sdk/openai-compatible')).createOpenAICompatible({ name: provider.id, baseURL, apiKey })(provider.model)
    default:
      throw new Error(`${provider.kind} is not an API provider`)
  }
}
