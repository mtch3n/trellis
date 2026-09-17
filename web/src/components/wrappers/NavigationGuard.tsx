import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link, useNavigate, type LinkProps } from 'react-router-dom'
import { ConfirmDialog } from '@/components/wrappers/ConfirmDialog'
import { NavigationGuardContext, useNavigationGuard } from '@/lib/navigation-guard'

/**
 * Holds whether the screen has unsaved changes and asks before leaving it.
 * Links and menus ask through `guard`; a reload or a closed tab gets the
 * browser's prompt, which is the only one a page may show there.
 */
export function NavigationGuardProvider({ children }: { children: ReactNode }) {
  const dirty = useRef(false)
  const [warning, setWarning] = useState(false)
  // The navigation waiting on the answer. Kept in an object so a function
  // can be stored in state.
  const [pending, setPending] = useState<{ go: () => void } | null>(null)

  const setDirty = useCallback((next: boolean) => {
    dirty.current = next
    setWarning(next)
  }, [])

  const guard = useCallback((go: () => void) => {
    if (dirty.current) setPending({ go })
    else go()
  }, [])

  useEffect(() => {
    if (!warning) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault() }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [warning])

  const value = useMemo(() => ({ guard, setDirty }), [guard, setDirty])

  return (
    <NavigationGuardContext.Provider value={value}>
      {children}
      <ConfirmDialog
        open={pending !== null}
        title="Discard unsaved changes?"
        description="What you changed here has not been saved. Leaving drops it."
        confirm="Discard changes"
        cancel="Keep editing"
        onOpenChange={(open) => { if (!open) setPending(null) }}
        onConfirm={() => {
          const next = pending
          setPending(null)
          setDirty(false)
          next?.go()
        }}
      />
    </NavigationGuardContext.Provider>
  )
}

/**
 * A link that asks before leaving unsaved changes. A click that opens a new
 * tab or window leaves this page alone, so it is let through.
 */
export function GuardedLink({ to, replace, state, onClick, ...props }: LinkProps) {
  const { guard } = useNavigationGuard()
  const navigate = useNavigate()
  return (
    <Link
      to={to}
      replace={replace}
      state={state}
      {...props}
      onClick={(event) => {
        onClick?.(event)
        if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        event.preventDefault()
        guard(() => navigate(to, { replace, state }))
      }}
    />
  )
}
