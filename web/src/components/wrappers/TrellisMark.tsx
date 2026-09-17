import { cn } from '@/lib/utils'

/**
 * The Trellis mark, from `brand/logo/mark.svg` — the same path data, so the
 * shape stays the brand's however it is used.
 *
 * The kit's mark is charcoal on ivory and has an inverse for dark surfaces.
 * The shell is dark by default and light by choice, so neither fixed colour
 * works in both: this draws the silhouette in `currentColor` instead, which
 * is the one-colour use of the mark, and keeps the interface achromatic the
 * way every other glyph in the chrome is.
 */
export function TrellisMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 800 800"
      aria-hidden="true"
      className={cn('size-5 shrink-0', className)}
      fill="currentColor"
    >
      <path d="M397 99 C291 97 187 143 156 191 C137 220 151 260 175 275 C211 304 268 309 332 320 C398 331 440 344 474 380 C498 410 515 443 568 443 C625 444 670 405 685 356 C704 289 677 219 622 176 C565 125 480 100 397 99 Z M194 360 C123 358 61 418 61 486 C60 548 96 597 145 626 C200 665 275 687 362 687 C444 687 530 665 573 626 C600 601 602 560 573 537 C548 514 504 505 458 499 C398 490 360 476 329 443 C302 415 288 389 248 373 C230 365 211 360 194 360 Z" />
    </svg>
  )
}
