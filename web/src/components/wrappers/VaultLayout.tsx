import { useSyncExternalStore, type ReactNode } from 'react'
import { useDefaultLayout } from 'react-resizable-panels'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable'

const WIDE = '(min-width: 64rem)'

function subscribe(change: () => void) {
  const query = window.matchMedia(WIDE)
  query.addEventListener('change', change)
  return () => query.removeEventListener('change', change)
}

const wideNow = () => window.matchMedia(WIDE).matches

/**
 * The vault's frame: the navigator and the entry side by side, with the
 * divider between them draggable and the width remembered per browser. The
 * navigator keeps its pixel width when the window changes, so resizing the
 * window gives the room to the entry. Below the lg breakpoint the two stack
 * and nothing resizes.
 */
export function VaultLayout({ nav, children }: { nav: ReactNode; children: ReactNode }) {
  const wide = useSyncExternalStore(subscribe, wideNow)
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({ id: 'trellis.vault-layout', storage: localStorage })

  if (!wide) {
    return (
      <div>
        {nav}
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
        <ResizablePanel id="navigator" defaultSize="18rem" minSize="14rem" maxSize="36rem" groupResizeBehavior="preserve-pixel-size">
          {nav}
        </ResizablePanel>
        <ResizableHandle
          aria-label="Resize the navigator"
          className="transition-colors after:w-2 data-[separator=active]:bg-foreground data-[separator=hover]:bg-rule-strong"
        />
        <ResizablePanel id="entry" minSize="24rem">
          {children}
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  )
}
