import { Chat } from '@ai-sdk/react'
import {
  convertToModelMessages, DefaultChatTransport, isStepCount, smoothStream, streamText, toUIMessageStream,
  type ChatTransport, type UIMessage, type UIMessageChunk,
} from 'ai'
import { boardTools, instructions } from '@/lib/chat-tools'
import { languageModel, type Provider } from '@/lib/providers'

/** What each assistant message remembers: who answered, and the agent's session. */
export interface ChatMetadata {
  provider?: string
  session?: string
}

export type ChatMessage = UIMessage<ChatMetadata>

/** An effort the chat can ask for; empty is the provider's own default. */
export type Effort = '' | 'low' | 'medium' | 'high' | 'xhigh'

/** What the next turn is sent with. The panel changes it; the transport reads it at send time. */
export interface ChatSettings {
  provider: Provider | null
  local: boolean
  effort: Effort
}

/** How many tool steps one turn may take before the model must answer. */
const MAX_STEPS = 12

/**
 * Providers send text in bursts of uneven size. Releasing it a word at a time
 * makes the reply read as steady typing. A segmenter finds the words, so
 * Chinese and Japanese, which put no spaces between them, stream as smoothly.
 */
const SMOOTHING = { chunking: new Intl.Segmenter(undefined, { granularity: 'word' }) }

/** A message's text, for a transcript. */
function textOf(message: ChatMessage) {
  return message.parts.flatMap((part) => (part.type === 'text' ? [part.text] : [])).join('\n').trim()
}

/**
 * Carries a conversation to whichever provider is chosen for the turn. An
 * API model runs here in the browser, through the daemon's proxy, with the
 * board as tools. A local agent runs in the daemon, which streams its turn
 * back in the same message format.
 */
class TrellisTransport implements ChatTransport<ChatMessage> {
  private readonly projectKey: string
  private readonly settings: ChatSettings

  constructor(projectKey: string, settings: ChatSettings) {
    this.projectKey = projectKey
    this.settings = settings
  }

  async sendMessages({ messages, abortSignal, ...rest }: Parameters<ChatTransport<ChatMessage>['sendMessages']>[0]) {
    const { provider, local, effort } = this.settings
    if (!provider) throw new Error('Choose a provider first.')
    return local
      ? this.sendToAgent(provider, effort, messages, { ...rest, abortSignal })
      : this.sendToModel(provider, effort, messages, abortSignal)
  }

  async reconnectToStream() {
    return null
  }

  private async sendToModel(provider: Provider, effort: Effort, messages: ChatMessage[], abortSignal?: AbortSignal) {
    const tools = boardTools(this.projectKey, provider.id)
    // A local agent's tool calls were its own and mean nothing to this model;
    // what it said stays in the conversation.
    const history = messages.map((message) => ({
      ...message,
      parts: message.parts.filter((part) => part.type !== 'dynamic-tool' && part.type !== 'reasoning'),
    }))
    const result = streamText({
      model: await languageModel(provider),
      instructions: instructions(this.projectKey),
      messages: await convertToModelMessages(history, { tools, ignoreIncompleteToolCalls: true }),
      tools,
      stopWhen: isStepCount(MAX_STEPS),
      experimental_transform: smoothStream(SMOOTHING),
      reasoning: effort || (provider.effort as Effort) || 'provider-default',
      abortSignal,
    })
    return toUIMessageStream<typeof tools, ChatMessage>({
      stream: result.stream,
      tools,
      sendReasoning: true,
      messageMetadata: ({ part }) => (part.type === 'start' ? { provider: provider.id } : undefined),
      onError: (error) => (error instanceof Error ? error.message : String(error)),
    }) as ReadableStream<UIMessageChunk>
  }

  private sendToAgent(
    provider: Provider,
    effort: Effort,
    messages: ChatMessage[],
    options: Omit<Parameters<ChatTransport<ChatMessage>['sendMessages']>[0], 'messages'>,
  ) {
    const last = messages.at(-1)
    const earlier = messages.slice(0, -1)
    // The agent keeps its own conversation; the last reply it gave here
    // names it. Without one, what was said so far goes with the prompt.
    const session = earlier.findLast((message) => message.metadata?.provider === provider.id)?.metadata?.session ?? ''
    let prompt = last ? textOf(last) : ''
    if (!session && earlier.length > 0) {
      const transcript = earlier
        .slice(-20)
        .map((message) => `${message.role === 'user' ? 'User' : 'Assistant'}: ${textOf(message)}`)
        .filter((line) => !line.endsWith(': '))
        .join('\n\n')
      if (transcript) prompt = `The conversation so far:\n\n${transcript}\n\nNow: ${prompt}`
    }
    const transport = new DefaultChatTransport<ChatMessage>({
      api: `/api/p/${encodeURIComponent(this.projectKey)}/chat`,
      prepareSendMessagesRequest: () => ({
        body: { provider: provider.id, prompt, session, effort },
      }),
    })
    return transport.sendMessages({ ...options, messages })
  }
}

const STORE_LIMIT = 60
const storeKey = (projectKey: string) => `trellis.chat.${projectKey}`

function loadMessages(projectKey: string): ChatMessage[] {
  try {
    const raw = localStorage.getItem(storeKey(projectKey))
    return raw ? (JSON.parse(raw) as ChatMessage[]) : []
  } catch {
    return []
  }
}

/** Keeps the last messages of a project's conversation in this browser. */
export function saveMessages(projectKey: string, messages: ChatMessage[]) {
  try {
    if (messages.length === 0) localStorage.removeItem(storeKey(projectKey))
    else localStorage.setItem(storeKey(projectKey), JSON.stringify(messages.slice(-STORE_LIMIT)))
  } catch {
    // Storage full or blocked: the conversation still lives for this visit.
  }
}

interface ProjectChat {
  chat: Chat<ChatMessage>
  settings: ChatSettings
}

/**
 * One conversation per project, kept for the life of the page so moving
 * between the board and the vault does not lose it, and restored from the
 * browser's storage after a reload.
 */
const chats = new Map<string, ProjectChat>()

export function projectChat(projectKey: string): ProjectChat {
  let entry = chats.get(projectKey)
  if (!entry) {
    const settings: ChatSettings = { provider: null, local: false, effort: '' }
    const chat = new Chat<ChatMessage>({
      id: `chat-${projectKey}`,
      messages: loadMessages(projectKey),
      transport: new TrellisTransport(projectKey, settings),
      onFinish: ({ messages }) => saveMessages(projectKey, messages),
    })
    entry = { chat, settings }
    chats.set(projectKey, entry)
  }
  return entry
}

/** Sets what the project's next turn is sent with. */
export function configureChat(projectKey: string, settings: ChatSettings) {
  Object.assign(projectChat(projectKey).settings, settings)
}

/** A tool call's name as the person reads it. */
export function toolLabel(name: string) {
  const words = name.replace(/^tool-/, '').replace(/^mcp__\w+?__/, '').split(/[_\s.]+/).filter(Boolean)
  const text = words.join(' ')
  return text.charAt(0).toUpperCase() + text.slice(1)
}
