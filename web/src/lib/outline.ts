import { useLayoutEffect, useState } from 'react'

/** One heading an outline lists: the id `rehype-slug` gave it, its words, and how deep it sits. */
export interface OutlineHeading {
  id: string
  text: string
  /** 0 for the shallowest heading present, whatever its level. */
  depth: number
}

// GFM's footnote heading is for screen readers only, so it stays out.
const SELECTOR = '.typeset :is(h1, h2, h3)[id]:not(.sr-only)'

/**
 * The headings of the markdown rendered inside `root`, read back from the
 * page after each render of `content`. Reading the DOM rather than parsing the
 * markdown again means the outline lists exactly what is on screen, under the
 * ids it really has.
 *
 * While `content` is undefined, an entry's body is on its way, so what was
 * listed stays listed: the column holding it does not close and reopen
 * between one entry and the next. A null `root` lists nothing. `version`
 * changes whenever the list does, for a caller that redraws on it.
 */
export function useOutline(root: HTMLElement | null, content: string | undefined) {
  const [outline, setOutline] = useState<{ headings: OutlineHeading[]; version: number }>({ headings: [], version: 0 })

  useLayoutEffect(() => {
    if (root && content === undefined) return
    const found = root ? [...root.querySelectorAll<HTMLElement>(SELECTOR)] : []
    const levels = found.map((element) => Number(element.tagName.slice(1)))
    const top = Math.min(...levels)
    const next = found.map((element, i) => ({ id: element.id, text: element.textContent ?? '', depth: levels[i] - top }))
    setOutline((current) => (same(current.headings, next) ? current : { headings: next, version: current.version + 1 }))
  }, [root, content])

  return outline
}

function same(a: OutlineHeading[], b: OutlineHeading[]) {
  return a.length === b.length && a.every((heading, i) => heading.id === b[i].id && heading.text === b[i].text && heading.depth === b[i].depth)
}
