import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { sentence } from '@/lib/format'
import type { Refusal } from '@/lib/api'

/**
 * Why the server said no, inside the form that asked. The message leads, and
 * each problem it named, such as a template rule, is its own line, so the
 * writer can work down them.
 */
export function RefusalAlert({ refusal }: { refusal: Refusal }) {
  return (
    <Alert variant="destructive">
      <AlertTitle className="text-pretty">{refusal.message}</AlertTitle>
      {refusal.problems.length > 0 && (
        <AlertDescription>
          <ul className="ml-4 list-disc">
            {refusal.problems.map((problem) => <li key={problem}>{sentence(problem).replace(/\.$/, '')}.</li>)}
          </ul>
        </AlertDescription>
      )}
    </Alert>
  )
}
