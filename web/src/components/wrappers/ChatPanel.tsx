import { useEffect, useRef, useState } from 'react'
import { useChat } from '@ai-sdk/react'
import { getToolName, isToolUIPart, type DynamicToolUIPart, type ToolUIPart } from 'ai'
import { ArrowUp, Check, ChevronRight, CircleAlert, SquarePen, Square, X } from 'lucide-react'
import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Bubble, BubbleContent } from '@/components/ui/bubble'
import { Button, buttonVariants } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Empty, EmptyContent, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '@/components/ui/input-group'
import { Marker, MarkerContent } from '@/components/ui/marker'
import { Message, MessageContent, MessageFooter } from '@/components/ui/message'
import {
  MessageScroller, MessageScrollerButton, MessageScrollerContent, MessageScrollerItem,
  MessageScrollerProvider, MessageScrollerViewport,
} from '@/components/ui/message-scroller'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { EffortSelect } from '@/components/wrappers/EffortSelect'
import { IconButton } from '@/components/wrappers/IconButton'
import { MarkdownContent } from '@/components/wrappers/MarkdownContent'
import { GuardedLink } from '@/components/wrappers/NavigationGuard'
import { configureChat, projectChat, saveMessages, toolLabel, type ChatMessage, type Effort } from '@/lib/chat'
import { fetchProviders, isLocal, providerLabel, type ProvidersResponse } from '@/lib/providers'
import { cn } from '@/lib/utils'

const PROVIDER_KEY = 'trellis.chat.provider'
const EFFORT_KEY = 'trellis.chat.effort'

/**
 * The chat, docked at the right edge beside whatever page is open, so the
 * board stays in view while the agent reads and changes it. It talks to one
 * provider at a time: an API the browser drives through the daemon, or an
 * agent the daemon runs. The conversation belongs to the project and
 * survives moving between pages.
 */
export function ChatPanel({ projectKey, open, onClose, onBusyChange }: {
  projectKey: string
  open: boolean
  onClose: () => void
  /** Told when a reply starts and ends, so the closed panel can still show it is working. */
  onBusyChange?: (busy: boolean) => void
}) {
  // Rendering every chunk re-parses the markdown each time; a frame's worth
  // of chunks at once reads the same and costs a fraction.
  const { messages, sendMessage, status, stop, error, regenerate, setMessages, clearError } =
    useChat<ChatMessage>({ chat: projectChat(projectKey).chat, throttle: 50 })
  const [data, setData] = useState<ProvidersResponse | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [providerId, setProviderId] = useState(() => localStorage.getItem(PROVIDER_KEY) ?? '')
  const [effort, setEffort] = useState<Effort>(() => (localStorage.getItem(EFFORT_KEY) ?? '') as Effort)
  const [draft, setDraft] = useState('')
  const input = useRef<HTMLTextAreaElement>(null)

  // Providers change in settings, so each opening reads them again.
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    fetchProviders(controller.signal)
      .then((next) => { setData(next); setLoadError(null) })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setLoadError(err instanceof Error ? err.message : 'Could not read the providers')
      })
    const focus = window.setTimeout(() => input.current?.focus(), 60)
    return () => { controller.abort(); window.clearTimeout(focus) }
  }, [open])

  const providers = data?.providers ?? []
  const kinds = data?.kinds ?? []
  const provider = providers.find((item) => item.id === providerId)
    ?? providers.find((item) => item.id === data?.default_provider)
    ?? providers[0]
    ?? null

  const local = provider ? isLocal(provider, kinds) : false

  // The transport reads these when the next message goes out.
  useEffect(() => {
    configureChat(projectKey, { provider, local, effort })
  }, [projectKey, provider, local, effort])

  const busy = status === 'submitted' || status === 'streaming'
  const last = messages.at(-1)
  const waiting = busy && awaitingModel(last)

  useEffect(() => { onBusyChange?.(busy) }, [busy, onBusyChange])

  const send = (text: string) => {
    const prompt = text.trim()
    if (!prompt || busy || !provider) return
    clearError()
    void sendMessage({ text: prompt })
    setDraft('')
  }

  const startOver = () => {
    if (busy) void stop()
    setMessages([])
    saveMessages(projectKey, [])
    clearError()
    input.current?.focus()
  }

  const chooseProvider = (id: string) => {
    setProviderId(id)
    localStorage.setItem(PROVIDER_KEY, id)
  }
  const chooseEffort = (value: string) => {
    setEffort(value as Effort)
    localStorage.setItem(EFFORT_KEY, value)
  }

  const labelOf = (id?: string) => {
    const match = providers.find((item) => item.id === id)
    return match ? providerLabel(match, kinds) : id
  }

  return (
    <aside
      aria-label="Chat"
      data-closed={!open || undefined}
      inert={!open}
      className={cn(
        'fixed inset-x-0 top-shell bottom-0 z-10 flex flex-col border-l border-border bg-background sm:left-auto sm:w-chat',
        'transition duration-200 ease-arrive',
        'data-closed:invisible data-closed:translate-x-4 data-closed:opacity-0 data-closed:duration-150 data-closed:ease-settle',
      )}
      onKeyDown={(event) => { if (event.key === 'Escape' && !event.defaultPrevented) onClose() }}
    >
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
        {!data && !loadError ? (
          <Skeleton className="h-7 flex-1" />
        ) : (
          <Select
            items={providers.map((item) => ({ value: item.id, label: providerLabel(item, kinds) }))}
            value={provider?.id ?? null}
            disabled={providers.length === 0}
            onValueChange={(value) => { if (value) chooseProvider(value) }}
          >
            <SelectTrigger size="sm" aria-label="Provider" className="min-w-0 flex-1">
              <SelectValue placeholder="No provider" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {providers.map((item) => (
                  <SelectItem key={item.id} value={item.id}>{providerLabel(item, kinds)}</SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        )}
        <EffortSelect size="sm" labelled={false} value={effort} fallback={provider?.effort || undefined} onChange={chooseEffort} className="w-36" />
        <IconButton label="New chat" disabled={messages.length === 0} onClick={startOver}>
          <SquarePen />
        </IconButton>
        <IconButton label="Close chat" onClick={onClose}>
          <X />
        </IconButton>
      </header>

      <MessageScrollerProvider autoScroll>
        <MessageScroller className="flex-1">
          <MessageScrollerViewport>
            <MessageScrollerContent aria-busy={busy} className="gap-5 px-4 py-5">
              {loadError && (
                <Alert variant="destructive">
                  <AlertTitle>Could not read the providers</AlertTitle>
                  <AlertDescription>{loadError}</AlertDescription>
                </Alert>
              )}

              {data && providers.length === 0 && (
                <Empty className="my-auto">
                  <EmptyHeader>
                    <EmptyTitle>No providers</EmptyTitle>
                  </EmptyHeader>
                  <EmptyContent>
                    <GuardedLink to="/settings/providers" className={buttonVariants({ size: 'sm' })}>Add provider</GuardedLink>
                  </EmptyContent>
                </Empty>
              )}

              {messages.map((message, index) => (
                <MessageScrollerItem key={message.id} messageId={message.id} scrollAnchor={message.role === 'user'} className="animate-enter">
                  {message.role === 'user' ? (
                    <Message align="end">
                      <MessageContent>
                        <Bubble variant="secondary" align="end">
                          <BubbleContent className="whitespace-pre-wrap">
                            {message.parts.map((part) => (part.type === 'text' ? part.text : '')).join('')}
                          </BubbleContent>
                        </Bubble>
                      </MessageContent>
                    </Message>
                  ) : (
                    <AssistantMessage
                      message={message}
                      projectKey={projectKey}
                      live={busy && message === last}
                      answeredBy={
                        // Named only where the provider changes; the header already names the current one.
                        message.metadata?.provider !== (messages.slice(0, index).findLast((earlier) => earlier.role === 'assistant')?.metadata?.provider ?? provider?.id)
                          ? labelOf(message.metadata?.provider)
                          : undefined
                      }
                    />
                  )}
                </MessageScrollerItem>
              ))}

              {waiting && (
                <MessageScrollerItem messageId="pending">
                  {/* Straight after sending it shows at once. Between steps it
                      waits a moment, so the pause between one step's end and
                      the next one's start does not flash it. */}
                  <Marker
                    role="status"
                    className={cn(
                      'animate-in fade-in slide-in-from-bottom-1 fill-mode-both duration-200 ease-arrive',
                      last?.role === 'assistant' && 'delay-300',
                    )}
                  >
                    <MarkerContent className="shimmer">Thinking</MarkerContent>
                  </Marker>
                </MessageScrollerItem>
              )}

              {error && (
                <MessageScrollerItem messageId="error">
                  <Alert variant="destructive">
                    <CircleAlert />
                    <AlertTitle>The reply stopped</AlertTitle>
                    <AlertDescription className="break-words">{error.message}</AlertDescription>
                    <AlertAction>
                      <Button variant="outline" size="sm" onClick={() => { clearError(); void regenerate() }}>Retry</Button>
                    </AlertAction>
                  </Alert>
                </MessageScrollerItem>
              )}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton />
        </MessageScroller>
      </MessageScrollerProvider>

      <form
        className="border-t border-border p-3"
        onSubmit={(event) => { event.preventDefault(); send(draft) }}
      >
        <InputGroup>
          <InputGroupTextarea
            ref={input}
            value={draft}
            rows={2}
            disabled={!provider}
            placeholder={`Ask about ${projectKey}`}
            aria-label="Message"
            className="max-h-48"
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault()
                send(draft)
              }
            }}
          />
          <InputGroupAddon align="block-end">
            {busy ? (
              <InputGroupButton key="stop" size="icon-xs" variant="outline" className="ml-auto animate-in fade-in zoom-in-75 duration-150" aria-label="Stop" onClick={() => void stop()}>
                <Square className="fill-current" />
              </InputGroupButton>
            ) : (
              <InputGroupButton key="send" type="submit" size="icon-xs" variant="default" className="ml-auto animate-in fade-in zoom-in-75 duration-150" aria-label="Send" disabled={!draft.trim() || !provider}>
                <ArrowUp />
              </InputGroupButton>
            )}
          </InputGroupAddon>
        </InputGroup>
      </form>
    </aside>
  )
}

/**
 * Whether a turn in progress has nothing on screen that shows it working:
 * the reply has not started, or a step has ended and the next has not
 * begun. Words arriving, reasoning arriving and a running tool each show
 * themselves.
 */
function awaitingModel(last?: ChatMessage) {
  if (last?.role !== 'assistant') return true
  const part = last.parts.findLast((item) =>
    ((item.type === 'text' || item.type === 'reasoning') && item.text.trim()) || isToolUIPart(item))
  if (!part) return true
  if (part.type === 'text' || part.type === 'reasoning') return part.state !== 'streaming'
  if (isToolUIPart(part)) return part.state !== 'input-streaming' && part.state !== 'input-available'
  return true
}

/** A reply: its reasoning, its tool calls and its words, in the order they happened. */
function AssistantMessage({ message, projectKey, live, answeredBy }: {
  message: ChatMessage
  projectKey: string
  /** Still arriving. A stopped reply leaves parts marked streaming, so the part's own state is not enough. */
  live: boolean
  answeredBy?: string
}) {
  return (
    <Message align="start">
      <MessageContent className="w-full">
        <Bubble variant="ghost" className="w-full">
          <BubbleContent className="flex w-full flex-col gap-3">
            {message.parts.map((part, index) => {
              if (part.type === 'text') {
                return part.text.trim() ? (
                  <MarkdownContent
                    key={index}
                    content={part.text}
                    className={cn('text-sm', live && part.state === 'streaming' && 'chat-caret')}
                  />
                ) : null
              }
              if (part.type === 'reasoning') {
                return part.text.trim() ? <Reasoning key={index} text={part.text} streaming={live && part.state === 'streaming'} /> : null
              }
              if (isToolUIPart(part)) {
                return <ToolCall key={index} part={part} projectKey={projectKey} />
              }
              return null
            })}
          </BubbleContent>
        </Bubble>
        {answeredBy && <MessageFooter className="text-xs text-muted-foreground">{answeredBy}</MessageFooter>}
      </MessageContent>
    </Message>
  )
}

/** A folding panel's height, animated open and shut. */
const FOLD = 'h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-200 ease-settle data-starting-style:h-0 data-ending-style:h-0'

/**
 * The model's reasoning. It stays open while it arrives, so there is
 * something to read during the wait, then folds away once the answer starts
 * and says how long it took. A reply restored from storage has no timing
 * and just says it reasoned.
 */
function Reasoning({ text, streaming }: { text: string; streaming: boolean }) {
  const [open, setOpen] = useState(streaming)
  const [seconds, setSeconds] = useState<number | null>(null)
  const started = useRef<number | null>(null)

  useEffect(() => {
    if (streaming) {
      started.current ??= Date.now()
      return
    }
    if (started.current === null) return
    setSeconds(Math.max(1, Math.round((Date.now() - started.current) / 1000)))
    started.current = null
    const fold = window.setTimeout(() => setOpen(false), 500)
    return () => window.clearTimeout(fold)
  }, [streaming])

  const label = streaming ? 'Reasoning' : seconds ? `Reasoned for ${seconds}s` : 'Reasoned'

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="animate-enter">
      <CollapsibleTrigger className="group/reasoning flex items-center gap-1.5 text-xs text-muted-foreground transition-colors duration-150 hover:text-foreground">
        <ChevronRight className="size-3.5 transition-transform duration-200 ease-settle group-data-panel-open/reasoning:rotate-90" />
        <span className={cn(streaming && 'shimmer')}>{label}</span>
      </CollapsibleTrigger>
      <CollapsibleContent className={FOLD}>
        <p className="mt-2 border-l border-border pl-3 text-xs leading-relaxed whitespace-pre-wrap text-muted-foreground">{text}</p>
      </CollapsibleContent>
    </Collapsible>
  )
}

/** The first thing worth showing about a tool's input, on one line. */
function inputSummary(input: unknown) {
  if (!input || typeof input !== 'object') return ''
  const values = Object.values(input as Record<string, unknown>).filter((value) => typeof value === 'string' && value)
  return (values[0] as string | undefined) ?? ''
}

/** A card the tool wrote or read, when its answer names one. */
function cardRef(output: unknown, projectKey: string) {
  if (!output || typeof output !== 'object') return null
  const ref = (output as { ref?: unknown }).ref
  return typeof ref === 'string' && ref.startsWith(`${projectKey}-`) ? ref : null
}

function pretty(value: unknown) {
  return typeof value === 'string' ? value : JSON.stringify(value, null, 2)
}

/**
 * One tool call as a single line: what it did, on what, and how it went.
 * Opening it shows the exact input and answer. A card the call touched is a
 * link, so a change the agent made is one step from being checked.
 */
function ToolCall({ part, projectKey }: { part: ToolUIPart | DynamicToolUIPart; projectKey: string }) {
  const name = getToolName(part)
  const running = part.state === 'input-streaming' || part.state === 'input-available'
  const failed = part.state === 'output-error'
  const output = part.state === 'output-available' ? part.output : undefined
  const ref = cardRef(output, projectKey)
  const summary = inputSummary(part.input)

  return (
    <Collapsible className="animate-enter border border-border">
      <div className="flex items-center gap-2 px-2.5 py-1.5 text-xs">
        <CollapsibleTrigger className="group/tool flex min-w-0 flex-1 items-center gap-2 text-left">
          {running ? (
            <Spinner className="size-3.5 text-muted-foreground" />
          ) : failed ? (
            <CircleAlert className="size-3.5 animate-in text-destructive duration-200 zoom-in-50 fade-in" />
          ) : (
            <Check className="size-3.5 animate-in text-muted-foreground duration-200 zoom-in-50 fade-in" />
          )}
          <span className={cn('font-medium', running && 'shimmer')}>{toolLabel(name)}</span>
          {summary && <span className="truncate text-meta text-muted-foreground">{summary}</span>}
          <ChevronRight className="ml-auto size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 ease-settle group-data-panel-open/tool:rotate-90" />
        </CollapsibleTrigger>
        {ref && (
          <GuardedLink to={`/p/${projectKey}/card/${ref}`} className="shrink-0 text-meta text-foreground underline underline-offset-2">
            {ref}
          </GuardedLink>
        )}
      </div>
      <CollapsibleContent className={FOLD}>
        <div className="flex flex-col gap-2 border-t border-border px-2.5 py-2">
          <pre className="max-h-40 overflow-auto text-meta whitespace-pre-wrap text-muted-foreground">{pretty(part.input)}</pre>
          {failed && <p className="text-xs text-destructive">{part.errorText}</p>}
          {output !== undefined && (
            <pre className="max-h-64 overflow-auto border-t border-border pt-2 text-meta whitespace-pre-wrap">{pretty(output)}</pre>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
