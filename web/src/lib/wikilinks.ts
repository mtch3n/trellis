/**
 * `[[wikilinks]]` in a rendered entry. The markdown files are the source of
 * truth and they spell links the way Obsidian does, so reading an entry in the
 * browser has to follow them; until now they rendered as the literal text.
 *
 * A link whose entry exists becomes an anchor to it. One whose entry does not
 * is a stub — normal in this vault, since writing a link before its target is
 * how entries get planned — so it is marked rather than dropped.
 */
import type { Root, PhrasingContent, Text } from 'mdast'
import { visit } from 'unist-util-visit'

/** Where a wikilink leads. */
export interface WikilinkTargets {
  /** Every slug the vault holds, so a link can be told from a stub. */
  slugs: Set<string>
  /** The path an entry opens at, inside the app. */
  pathOf: (slug: string) => string
}

/**
 * Marks the anchors this plugin makes, so the renderer can tell them from the
 * links an author wrote. A stub carries the prefix and nothing else.
 */
export const WIKILINK_PREFIX = 'trellis-wikilink:'

// [[target]], [[target#anchor]] or [[target|what to call it]].
const WIKILINK = /\[\[([^\]|#]+)(#[^\]|]+)?(?:\|([^\]]+))?]]/g

/**
 * A remark plugin: every wikilink in the text becomes a link node whose href
 * the renderer resolves. Code spans and fenced code are left alone, because a
 * wikilink inside them is being shown, not followed.
 */
export function remarkWikilinks(targets: WikilinkTargets) {
  return (tree: Root) => {
    visit(tree, 'text', (node: Text, index, parent) => {
      if (!parent || index === undefined) return
      if (parent.type === 'link' || parent.type === 'linkReference') return
      const parts = split(node.value, targets)
      if (!parts) return
      parent.children.splice(index, 1, ...parts)
      return index + parts.length
    })
  }
}

/** One text node as the nodes it becomes, or null when it holds no wikilink. */
function split(text: string, targets: WikilinkTargets): PhrasingContent[] | null {
  WIKILINK.lastIndex = 0
  if (!WIKILINK.test(text)) return null
  WIKILINK.lastIndex = 0
  const out: PhrasingContent[] = []
  let at = 0
  for (const match of text.matchAll(WIKILINK)) {
    const [whole, rawTarget, anchor, alias] = match
    const start = match.index
    if (start > at) out.push({ type: 'text', value: text.slice(at, start) })
    at = start + whole.length
    const slug = rawTarget.trim().replace(/^\/+|\/+$/g, '')
    const known = targets.slugs.has(slug)
    out.push({
      type: 'link',
      // A stub carries the prefix with nothing after it: there is nowhere to go.
      url: known ? WIKILINK_PREFIX + targets.pathOf(slug) + (anchor ?? '') : WIKILINK_PREFIX,
      children: [{ type: 'text', value: alias?.trim() || slug + (anchor ?? '') }],
    })
  }
  if (at < text.length) out.push({ type: 'text', value: text.slice(at) })
  return out
}
