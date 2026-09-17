/** Why the server refused a request: its message, and each problem it named. */
export interface Refusal {
  message: string
  /** What an entry got wrong against its template, one problem each. Empty for other refusals. */
  problems: string[]
}

/**
 * What a failed request has to say, kept apart. The server answers errors as
 * `{"error": "<message>", "problems": [...]}`, and showing that JSON as it
 * arrives puts braces and escaped newlines in front of a person.
 */
export async function readRefusal(response: Response): Promise<Refusal> {
  const text = await response.text()
  try {
    const parsed = JSON.parse(text) as { error?: unknown; problems?: unknown }
    if (typeof parsed.error === 'string' && parsed.error.trim()) {
      const problems = Array.isArray(parsed.problems)
        ? parsed.problems.filter((problem): problem is string => typeof problem === 'string')
        : []
      return { message: parsed.error.trim(), problems }
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
