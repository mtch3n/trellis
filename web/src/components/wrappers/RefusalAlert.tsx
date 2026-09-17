import { sentence } from '@/lib/format'
import type { Refusal } from '@/lib/api'

/**
 * Why the server said no, inside the form that asked. The message leads, and
 * each problem it named, such as a template rule, is its own line, so the
 * writer can work down them. Plain text, not a box: it usually sits inside a
 * dialog, and a bordered alert there is a box in a box.
 */
export function RefusalAlert({ refusal }: { refusal: Refusal }) {
  return (
    <div role="alert" className="flex flex-col gap-1 text-sm text-pretty text-destructive">
      <p className="font-medium">{refusal.message}</p>
      {refusal.problems.length > 0 && (
        <ul className="ml-4 list-disc">
          {refusal.problems.map((problem) => <li key={problem}>{sentence(problem).replace(/\.$/, '')}.</li>)}
        </ul>
      )}
    </div>
  )
}
