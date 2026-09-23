import { Link } from 'react-router-dom'
import ReactMarkdown, { type ExtraProps, type Options } from 'react-markdown'
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize'
import rehypeSlug from 'rehype-slug'
import remarkGfm from 'remark-gfm'
import { Badge } from '@/components/ui/badge'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { citeHref, remarkCitations } from '@/lib/citations'
import { cn } from '@/lib/utils'
import { remarkWikilinks, WIKILINK_PREFIX, type WikilinkTargets } from '@/lib/wikilinks'

interface MarkdownContentProps {
  content: string
  className?: string
  /**
   * Where `[[wikilinks]]` in this text lead: the entries that exist, and the
   * path a slug opens at. Without it a wikilink stays the text it is, which
   * is what a card's comment or a diff wants.
   */
  wikilinks?: WikilinkTargets
  /**
   * Give each heading an id from its text, GitHub's way, so an outline can
   * point at it. It runs after sanitizing, which would otherwise prefix the
   * ids it did not write.
   */
  anchors?: boolean
  /**
   * Draw footnote references as citations: a chip naming the note's source,
   * which opens its link or, without one, scrolls to the note.
   */
  citations?: boolean
}

// A footnote's way back up to its citation. GFM's default hooked arrow is an
// emoji character that many systems draw as a coloured tile; this one is not.
const remarkRehype: Options['remarkRehypeOptions'] = { footnoteBackContent: '\u2191' }

// The citation facts ride on the footnote's <sup>; everything else is the
// default schema, so sanitizing is unchanged for every other element.
const citationSchema = {
  ...defaultSchema,
  attributes: {
    ...defaultSchema.attributes,
    sup: [...(defaultSchema.attributes?.sup ?? []), 'dataCite', 'dataCiteHref', 'dataCiteTitle', 'dataCiteExcerpt', 'dataCiteNote'],
  },
}

export function MarkdownContent({ content, className, wikilinks, anchors, citations }: MarkdownContentProps) {
  const remarkPlugins: Options['remarkPlugins'] = [remarkGfm]
  if (wikilinks) remarkPlugins.push([remarkWikilinks, wikilinks])
  if (citations) remarkPlugins.push(remarkCitations)
  const rehypePlugins: Options['rehypePlugins'] = [citations ? [rehypeSanitize, citationSchema] : rehypeSanitize]
  if (anchors) rehypePlugins.push(rehypeSlug)
  return (
    <div className={cn('typeset typeset-notes', className)}>
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
        remarkRehypeOptions={remarkRehype}
        components={{ ...(wikilinks && { a: WikiAnchor }), ...(citations && { sup: CiteMark }) }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}

/**
 * A footnote reference drawn as a citation chip. Pointing at it, or focusing
 * it, opens a card with what the note says: the source, the title its link
 * was given, and the rest of the note. Nothing is fetched; the card shows only
 * what the entry wrote. With a web link the chip opens it in a new tab;
 * without one it scrolls to the note at the foot of the text. A reference
 * with no note stays the superscript it was.
 */
function CiteMark({ node, children, ...props }: React.ComponentProps<'sup'> & ExtraProps) {
  const facts = node?.properties ?? {}
  const text = (value: unknown) => (typeof value === 'string' && value ? value : undefined)
  const label = text(facts.dataCite)
  if (!label) return <sup {...props}>{children}</sup>
  const href = citeHref(text(facts.dataCiteHref))
  const title = text(facts.dataCiteTitle)
  const excerpt = text(facts.dataCiteExcerpt)
  const note = String(facts.dataCiteNote ?? '')

  // An in-page link to the note, scrolled to by hand: the note's id carries
  // the sanitizer's prefix, and the address should not change.
  const toNote = (event: React.MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault()
    const root = event.currentTarget.closest('.typeset')
    root?.querySelector(`[id$="fn-${CSS.escape(note)}"]`)?.scrollIntoView({ block: 'center' })
  }
  const target = href
    ? <a href={href} target="_blank" rel="noreferrer" />
    : <a href={`#fn-${note}`} onClick={toNote} />

  return (
    <Popover>
      <PopoverTrigger
        openOnHover
        delay={200}
        closeDelay={150}
        render={
          <Badge
            variant="secondary"
            className="mx-0.5 h-4.5 align-baseline font-normal text-muted-foreground hover:text-foreground data-popup-open:text-foreground"
            render={target}
          />
        }
      >
        {label}
      </PopoverTrigger>
      <PopoverContent side="bottom" align="start" className="w-80 gap-1.5 p-3">
        <p className="text-xs text-muted-foreground">{href ? label : 'Note'}</p>
        {title && <p className="text-sm font-medium text-pretty">{title}</p>}
        {excerpt && <p className="line-clamp-4 text-xs text-pretty text-muted-foreground">{excerpt}</p>}
      </PopoverContent>
    </Popover>
  )
}

/**
 * An anchor the wikilink plugin made. A link to an entry that exists is a
 * router link, so following it does not reload the app; one whose entry has
 * not been written is dashed and muted, because a stub is a loose end and not
 * an alarm. Every other anchor is left exactly as the markdown wrote it.
 */
function WikiAnchor({ href, children, ...props }: React.ComponentProps<'a'>) {
  if (!href?.startsWith(WIKILINK_PREFIX)) return <a href={href} {...props}>{children}</a>
  const target = href.slice(WIKILINK_PREFIX.length)
  if (target === '') {
    return (
      <span
        className="border-b border-dashed border-rule-strong text-muted-foreground"
        title="Nothing by that name is in the vault yet"
      >
        {children}
      </span>
    )
  }
  return <Link to={target}>{children}</Link>
}
