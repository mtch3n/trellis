import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * The row of actions over a card or an entry: what it is on the left, what can
 * be done to it on the right. It sticks under the app bar while the document
 * scrolls, so Save stays in reach down a long body, and it stacks above the
 * document, whose editor is positioned and would otherwise paint over it. Its
 * surface reaches 12px past each side, the width of an edit wash, so no sliver
 * of a field shows beside it while the document scrolls underneath.
 */
export function ActionRow({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <header
      className={cn(
        'sticky top-shell z-10 flex h-14 items-center gap-3 bg-background',
        'before:absolute before:inset-y-0 before:-inset-x-3 before:-z-10 before:bg-background',
        className,
      )}
    >
      {children}
    </header>
  )
}
