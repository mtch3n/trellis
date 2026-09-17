/**
 * One vault entry: a markdown file with frontmatter. Lists carry no body, so
 * an entry read from a list has its facts and nothing more; the single-entry
 * route fills in the rest.
 */
import type { Artifact } from '@/components/wrappers/ArtifactList'
import type { FieldValues } from '@/lib/templates'

export interface Entry {
  id: string
  slug: string
  ref: string
  title: string
  summary?: string
  /** Set when the entry is pinned: the line a session reads before the body. */
  recap?: string
  body?: string
  /** The template the entry follows, or "" when it follows none. */
  template?: string
  path?: string
  global?: boolean
  private?: boolean
  created_at?: number
  updated_at?: number
  version: number
  /** The entry's artifacts. Absent when there are none, and never on vault entries. */
  artifacts?: Artifact[]
  /** What the entry's claims rest on. Only the full entry carries them. */
  sources?: string[]
  /** Frontmatter beyond what Trellis names: what templates and `set` write. Empty on private entries in lists. */
  fields?: FieldValues
}
