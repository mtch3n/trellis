/**
 * Sentence case for the words the backend stores in lower case: column
 * names, priorities, template names, event actions. Display only; the stored
 * value is what gets sent back.
 */
export function sentence(word: string) {
  return word ? word.charAt(0).toUpperCase() + word.slice(1) : word
}

/** Words run together as a sentence reads them: "a, b and c". */
export function wordList(words: string[]) {
  if (words.length <= 1) return words.join('')
  return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`
}

/** An entry's template as a label; an entry need not have one. */
export function templateLabel(template?: string) {
  return template ? sentence(template) : 'No template'
}

/** How long ago, in the fewest words that still mean something. */
export function ago(timestamp: number, now = Date.now()) {
  const seconds = Math.max(0, Math.round((now - timestamp) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} h ago`
  const days = Math.round(hours / 24)
  if (days < 14) return `${days} d ago`
  return new Date(timestamp).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

/** A file size in the unit a person would say out loud. */
export function bytes(size: number) {
  if (size < 1024) return `${size} B`
  const units = ['KB', 'MB', 'GB']
  let value = size / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`
}
