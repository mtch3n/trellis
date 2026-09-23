import { diffLines, diffWordsWithSpace, parsePatch } from 'diff'
import { cn } from '@/lib/utils'

/**
 * What changed between two texts. Lines suit a body; words suit a title or a
 * summary. Additions sit on the accent surface and removals are struck
 * through, so the palette keeps its meaning: no green, no red.
 */
export function DiffView({ before, after, mode = 'lines' }: { before: string; after: string; mode?: 'lines' | 'words' }) {
  if (before === after) {
    return <p className="text-xs text-muted-foreground">No textual change.</p>
  }

  if (mode === 'words') {
    const changes = diffWordsWithSpace(before, after)
    return (
      <p className="text-sm">
        {changes.map((change, i) => {
          if (change.added) {
            return (
              <span key={i} className="bg-accent text-foreground">
                {change.value}
              </span>
            )
          }
          if (change.removed) {
            return (
              <span key={i} className="text-muted-foreground line-through">
                {change.value}
              </span>
            )
          }
          return <span key={i}>{change.value}</span>
        })}
      </p>
    )
  }

  // mode === 'lines'
  const changes = diffLines(before, after)
  const lines: Array<{ value: string; added?: boolean; removed?: boolean }> = []

  for (const change of changes) {
    const lineArray = change.value.split('\n')
    // Drop the single trailing empty string from split if there's a trailing newline
    if (lineArray.length > 0 && lineArray[lineArray.length - 1] === '') {
      lineArray.pop()
    }
    for (const line of lineArray) {
      lines.push({
        value: line,
        added: change.added,
        removed: change.removed,
      })
    }
  }

  return (
    <div className="overflow-x-auto py-2 text-meta leading-relaxed">
      {renderLinesWithCollapse(lines)}
    </div>
  )
}

/**
 * A unified diff the server already made, drawn with the same rows as a diff
 * made here. The file names are left out, since the dialog already says what
 * changed, and each hunk opens with where it sits.
 */
export function PatchView({ patch }: { patch: string }) {
  const hunks = parsePatch(patch).flatMap((file) => file.hunks)
  return (
    <div className="overflow-x-auto py-2 text-meta leading-relaxed">
      {hunks.map((hunk, h) => (
        <div key={h} className={cn(h > 0 && 'mt-3')}>
          <div className="px-3 pb-1 text-xs text-muted-foreground">
            Lines {hunk.newStart}–{hunk.newStart + Math.max(hunk.newLines - 1, 0)}
          </div>
          {hunk.lines
            .filter((line) => !line.startsWith('\\'))
            .map((line, i) =>
              renderLine({ value: line.slice(1), added: line[0] === '+', removed: line[0] === '-' }, `${h}-${i}`),
            )}
        </div>
      ))}
    </div>
  )
}

function renderLinesWithCollapse(lines: Array<{ value: string; added?: boolean; removed?: boolean }>) {
  const result = []
  let i = 0

  while (i < lines.length) {
    const line = lines[i]

    if (line.added || line.removed) {
      // Render changed line as-is
      result.push(renderLine(line, i))
      i++
    } else {
      // Count consecutive unchanged lines
      let unchangedCount = 0
      let j = i
      while (j < lines.length && !lines[j].added && !lines[j].removed) {
        unchangedCount++
        j++
      }

      // Long unchanged stretches fold to two lines of context either side.
      if (unchangedCount > 6) {
        // Collapse: show first 2 and last 2, hide the middle
        for (let k = 0; k < 2; k++) {
          result.push(renderLine(lines[i + k], i + k))
        }
        const hiddenCount = unchangedCount - 4
        result.push(
          <div key={`collapse-${i}`} className="px-3 text-xs text-muted-foreground">
            {hiddenCount} unchanged lines
          </div>
        )
        for (let k = unchangedCount - 2; k < unchangedCount; k++) {
          result.push(renderLine(lines[i + k], i + k))
        }
        i = j
      } else {
        // Don't collapse: render all unchanged lines
        for (let k = 0; k < unchangedCount; k++) {
          result.push(renderLine(lines[i + k], i + k))
        }
        i = j
      }
    }
  }

  return result
}

function renderLine(line: { value: string; added?: boolean; removed?: boolean }, key: number | string) {
  const gutter = line.added ? '+' : line.removed ? '-' : ' '
  const lineClass = cn(
    'px-3 flex gap-2',
    line.added && 'bg-accent text-foreground',
    line.removed && 'text-muted-foreground line-through decoration-muted-foreground/60'
  )
  if (!line.added && !line.removed) {
    // unchanged
    return (
      <div key={key} className={cn(lineClass, 'text-muted-foreground')}>
        <span className="w-5 text-right shrink-0">{gutter}</span>
        <span className="whitespace-pre-wrap break-words">{line.value}</span>
      </div>
    )
  }
  return (
    <div key={key} className={lineClass}>
      <span className="w-5 text-right shrink-0">{gutter}</span>
      <span className="whitespace-pre-wrap break-words">{line.value}</span>
    </div>
  )
}
