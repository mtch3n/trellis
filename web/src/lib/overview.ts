/** One actor's events in a project over the last 30 days, as the activity endpoint sends it. */
export interface ActorActivity {
  actor: string
  events: number
  last_seen: number
}

/** `GET /api/p/{key}/activity`: who acted on the project in the last 30 days. */
export interface Activity {
  actors: ActorActivity[]
  sessions: number
  session_events: number
}

export const NO_ACTIVITY = 'No activity in the last 30 days.'

/** The command that writes the entry the page reads its declared purpose and people from. */
export const CREATE_OVERVIEW = 'trellis vault new --title Overview'

/** The slug of that entry. */
export const OVERVIEW_SLUG = 'overview'

function count(n: number, word: string) {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

/**
 * The observed activity group: each non-agent actor as it is, and every
 * agent session folded into one line, since each session is its own actor.
 */
export function activityView(activity: Activity):
  | { empty: string }
  | { actors: { actor: string; events: string; lastSeen: number }[]; sessions?: string } {
  if (activity.actors.length === 0 && activity.sessions === 0) return { empty: NO_ACTIVITY }
  return {
    actors: activity.actors.map((a) => ({ actor: a.actor, events: count(a.events, 'event'), lastSeen: a.last_seen })),
    ...(activity.sessions > 0 && {
      sessions: `${count(activity.sessions, 'session')}, ${count(activity.session_events, 'event')}`,
    }),
  }
}

/** The declared section: the overview entry's body and where it opens, or the command that writes it. */
export function declaredView(
  key: string,
  entry: { slug: string; body?: string } | null,
): { body: string; href: string } | { command: string } {
  if (!entry) return { command: CREATE_OVERVIEW }
  return { body: entry.body ?? '', href: `/p/${key}/vault/${encodeURIComponent(entry.slug)}` }
}

/** The line under the title, or nothing when the project has no description. */
export function descriptionLine(description: string): string | undefined {
  return description.trim() || undefined
}
