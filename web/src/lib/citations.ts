import type { FootnoteDefinition, Root } from 'mdast'
import { visit } from 'unist-util-visit'

/** How long a citation's label may run when its footnote has no link to name it by. */
const LABEL = 28

/** A link a citation may open: only the web, never a script. */
export function citeHref(url: string | undefined) {
  if (!url) return undefined
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : undefined
  } catch {
    return undefined
  }
}

const squash = (words: string[]) => words.join('').replace(/\s+/g, ' ').trim()

/**
 * What a footnote says about its source: the link, the link's words as the
 * title when they are more than the address, and the rest of the note as the
 * excerpt. `[^1]: [Design skills](https://example.com/x) Instructs agents to…`
 * gives all three.
 */
function describe(definition: FootnoteDefinition) {
  let href: string | undefined
  const title: string[] = []
  const rest: string[] = []
  visit(definition, (node) => {
    if (node.type === 'link' && href === undefined && citeHref(node.url)) {
      href = citeHref(node.url)
      visit(node, (inner) => { if (inner.type === 'text' || inner.type === 'inlineCode') title.push(inner.value) })
      return 'skip' // unist-util-visit's SKIP: the link's own words are already read
    }
    if (node.type === 'text' || node.type === 'inlineCode') rest.push(node.value)
  })
  const heading = squash(title)
  const excerpt = squash(rest).replace(/^[\s:–-]+/, '')
  const named = heading && heading !== href && !href?.startsWith(heading) ? heading : ''
  const label = href
    ? new URL(href).hostname.replace(/^www\./, '')
    : excerpt.length > LABEL ? `${excerpt.slice(0, LABEL - 1).trimEnd()}…` : excerpt
  return { href, title: named, excerpt, label: label || 'source' }
}

/**
 * GFM footnotes as citations. Each footnote reference carries what its note
 * says: a short label (the host of the note's first link, else its opening
 * words), the title and excerpt a hover card shows, and the link to open when
 * there is one. Rendering
 * decides how to draw them; the markdown stays plain footnotes, so the file
 * reads the same in any editor.
 */
export function remarkCitations() {
  return (tree: Root) => {
    const notes = new Map<string, ReturnType<typeof describe>>()
    visit(tree, 'footnoteDefinition', (node) => { notes.set(node.identifier, describe(node)) })
    visit(tree, 'footnoteReference', (node) => {
      const note = notes.get(node.identifier)
      if (!note) return
      node.data = {
        ...node.data,
        hProperties: {
          dataCite: note.label,
          dataCiteHref: note.href,
          dataCiteTitle: note.title,
          dataCiteExcerpt: note.excerpt,
          dataCiteNote: node.identifier,
        },
      }
    })
  }
}
