import { Link } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import rehypeSanitize from 'rehype-sanitize'
import rehypeSlug from 'rehype-slug'
import remarkGfm from 'remark-gfm'
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
}

export function MarkdownContent({ content, className, wikilinks, anchors }: MarkdownContentProps) {
  return (
    <div className={cn('typeset typeset-notes', className)}>
      <ReactMarkdown
        remarkPlugins={wikilinks ? [remarkGfm, [remarkWikilinks, wikilinks]] : [remarkGfm]}
        rehypePlugins={anchors ? [rehypeSanitize, rehypeSlug] : [rehypeSanitize]}
        components={wikilinks ? { a: WikiAnchor } : undefined}
      >
        {content}
      </ReactMarkdown>
    </div>
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
