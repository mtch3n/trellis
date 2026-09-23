import { useEffect, useRef, useSyncExternalStore, type ReactNode } from 'react'
import { useDefaultLayout, usePanelRef } from 'react-resizable-panels'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable'

const WIDE = '(min-width: 64rem)'

function subscribe(change: () => void) {
  const query = window.matchMedia(WIDE)
  query.addEventListener('change', change)
  return () => query.removeEventListener('change', change)
}

const wideNow = () => window.matchMedia(WIDE).matches

const WIDTH_KEY = 'trellis.vault-nav-width'

function storedWidth() {
  try { return Number(localStorage.getItem(WIDTH_KEY)) || null } catch { return null }
}

/**
 * The vault's frame: the navigator and the entry side by side, with the
 * divider between them draggable and the width remembered per browser. The
 * navigator keeps its pixel width when the window changes, so resizing the
 * window gives the room to the entry. Below the lg breakpoint the two stack
 * and nothing resizes.
 *
 * The navigator collapses: from its toggle, or by dragging the divider past
 * its narrowest. Either way `navOpen` follows, and it reopens at the width it
 * had, even after a reload. The divider has no colour at rest; it shows when pointed at.
 */
export function VaultLayout({
  nav,
  navOpen,
  onNavOpenChange,
  children,
}: {
  nav: ReactNode
  navOpen: boolean
  onNavOpenChange: (open: boolean) => void
  children: ReactNode
}) {
  const wide = useSyncExternalStore(subscribe, wideNow)
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({ id: 'trellis.vault-layout', storage: localStorage })
  const panel = usePanelRef()
  // The width the navigator last had open, in pixels.
  const lastWidth = useRef(storedWidth())

  useEffect(() => {
    const handle = panel.current
    if (!handle || handle.isCollapsed() !== navOpen) return
    if (navOpen) handle.resize(lastWidth.current ? `${lastWidth.current}px` : '18rem')
    else handle.collapse()
  }, [navOpen, panel, wide])

  const resized = () => {
    const handle = panel.current
    if (!handle) return
    const open = !handle.isCollapsed()
    if (open) lastWidth.current = handle.getSize().inPixels
    if (open === navOpen) return
    if (!open && lastWidth.current) {
      try { localStorage.setItem(WIDTH_KEY, String(Math.round(lastWidth.current))) } catch { /* private mode */ }
    }
    onNavOpenChange(open)
  }

  if (!wide) {
    return (
      <div>
        {navOpen && nav}
        {children}
      </div>
    )
  }

  // The group sizes itself to 100% of its parent, so the parent carries the
  // frame's height.
  return (
    <div className="h-under-shell">
      <ResizablePanelGroup
        orientation="horizontal"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
      >
        <ResizablePanel
          id="navigator"
          panelRef={panel}
          collapsible
          defaultSize="18rem"
          minSize="14rem"
          maxSize="36rem"
          groupResizeBehavior="preserve-pixel-size"
          onResize={resized}
        >
          {/* Out of the tab order while collapsed, not just out of sight. */}
          <div className="h-full" inert={!navOpen}>{nav}</div>
        </ResizablePanel>
        <ResizableHandle
          aria-label="Resize the navigator"
          className="bg-transparent transition-colors after:w-2 data-[separator=active]:bg-foreground data-[separator=hover]:bg-rule-strong"
        />
        <ResizablePanel id="entry" minSize="24rem">
          {children}
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  )
}
