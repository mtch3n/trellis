import { useLayoutEffect, useState } from 'react'

/** One heading an outline lists: the id `rehype-slug` gave it, its words, and how deep it sits. */
export interface OutlineHeading {
  id: string
  text: string
  /** 0 for the shallowest heading present, whatever its level. */
  depth: number
}

const SELECTOR = '.typeset :is(h1, h2, h3)[id]'

/**
 * The headings of the markdown rendered inside `root`, read back from the
 * page after each render of `content`. Reading the DOM rather than parsing the
 * markdown again means the outline lists exactly what is on screen, under the
 * ids it really has.
 */
export function useOutline(root: HTMLElement | null, content: string | undefined): OutlineHeading[] {
  const [headings, setHeadings] = useState<OutlineHeading[]>([])

  useLayoutEffect(() => {
    const found = root ? [...root.querySelectorAll<HTMLElement>(SELECTOR)] : []
    const levels = found.map((element) => Number(element.tagName.slice(1)))
    const top = Math.min(...levels)
    const next = found.map((element, i) => ({ id: element.id, text: element.textContent ?? '', depth: levels[i] - top }))
    setHeadings((current) => (same(current, next) ? current : next))
  }, [root, content])

  return headings
}

function same(a: OutlineHeading[], b: OutlineHeading[]) {
  return a.length === b.length && a.every((heading, i) => heading.id === b[i].id && heading.text === b[i].text && heading.depth === b[i].depth)
}
