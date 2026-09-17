import { createContext, useContext, useEffect } from 'react'

/**
 * Leaving a screen that holds unsaved changes asks first. The app is not on a
 * data router, so there is no blocker to lean on: the links and menus that
 * navigate ask the guard, and a reload or a closed tab gets the browser's own
 * prompt.
 */
export interface NavigationGuard {
  /** Run `go` now, or once the reader agrees to drop the unsaved changes. */
  guard: (go: () => void) => void
  /** Report whether the screen holds unsaved changes. */
  setDirty: (dirty: boolean) => void
}

export const NavigationGuardContext = createContext<NavigationGuard>({
  guard: (go) => go(),
  setDirty: () => {},
})

export function useNavigationGuard() {
  return useContext(NavigationGuardContext)
}

/** Marks the screen as holding unsaved changes while `dirty` is true, and clears it on leaving. */
export function useUnsavedChanges(dirty: boolean) {
  const { setDirty } = useNavigationGuard()
  useEffect(() => { setDirty(dirty) }, [dirty, setDirty])
  useEffect(() => () => setDirty(false), [setDirty])
}
