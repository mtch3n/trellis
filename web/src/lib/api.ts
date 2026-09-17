import { ago } from '@/lib/format'

/** Why the server refused a request: its message, and each problem it named. */
export interface Refusal {
  message: string
  /** What an entry got wrong against its template, one problem each. Empty for other refusals. */
  problems: string[]
  /** The machine-readable reason, such as `contention` or `conflict`. */
  code?: string
  /** What the refusal knows beyond its words, such as who claims a card. */
  detail?: unknown
}

/** Who holds a contended card, as a refusal's detail carries it. */
interface Contention {
  claimed_by?: { handle?: string; last_seen?: number }
  recommended_action?: string
}

/**
 * What a failed request has to say, kept apart. The server answers errors as
 * `{"error": "<message>", "problems": [...]}`, and showing that JSON as it
 * arrives puts braces and escaped newlines in front of a person.
 */
export async function readRefusal(response: Response): Promise<Refusal> {
  const text = await response.text()
  try {
    const parsed = JSON.parse(text) as { error?: unknown; problems?: unknown; code?: unknown; detail?: unknown }
    if (typeof parsed.error === 'string' && parsed.error.trim()) {
      const problems = Array.isArray(parsed.problems)
        ? parsed.problems.filter((problem): problem is string => typeof problem === 'string')
        : []
      return {
        message: parsed.error.trim(),
        problems,
        code: typeof parsed.code === 'string' ? parsed.code : undefined,
        detail: parsed.detail,
      }
    }
  } catch {
    // Not JSON: the text is the message.
  }
  return { message: text.trim() || `The server answered ${response.status}.`, problems: [] }
}

/** A refusal as one line of words, for a toast. */
export function refusalText({ message, problems }: Refusal) {
  if (problems.length === 0) return message
  return `${message.replace(/[.:]$/, '')}: ${problems.map((problem) => problem.replace(/\.$/, '')).join('; ')}.`
}

/** What a failed request has to say, as one line of words. */
export async function readError(response: Response) {
  return refusalText(await readRefusal(response))
}

/**
 * Who claims the card a write was refused for, when the refusal says so. The
 * server sends the claimant's agent record as the error's detail, so the
 * browser can name them without asking a second time.
 */
export function whoClaims(refusal: Refusal): { handle: string; seen: string } | null {
  if (refusal.code !== 'contention' || !refusal.detail || typeof refusal.detail !== 'object') return null
  const { claimed_by: claimant } = refusal.detail as Contention
  if (!claimant?.handle) return null
  return {
    handle: claimant.handle,
    seen: claimant.last_seen ? ago(claimant.last_seen) : 'at an unknown time',
  }
}
