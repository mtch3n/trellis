/**
 * A body that opens with its entry's title as an H1, as a template skeleton
 * does. The file keeps the heading, because an editor such as Obsidian shows
 * it as the page title, but the entry page already sets the title above the
 * body, so the page leaves the heading out and puts it back on save.
 */

const HEADING = /^\s*#[ \t]+(.+?)[ \t]*#*[ \t]*(?:\r?\n|$)/

/** The body without a leading H1 that repeats the title, and whether it had one. */
export function splitTitleHeading(body: string, title: string) {
  const match = HEADING.exec(body)
  if (!match || match[1].trim() !== title.trim()) return { heading: false, rest: body }
  return { heading: true, rest: body.slice(match[0].length).replace(/^\s*\n/, '') }
}

/** The body with its title heading restored, under the title as it is now. */
export function joinTitleHeading(rest: string, title: string) {
  return rest.trim() ? `# ${title.trim()}\n\n${rest}` : `# ${title.trim()}\n`
}
