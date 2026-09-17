import { Fragment } from 'react'
import { Routes, Route, Link, useParams } from 'react-router-dom'
import { RootRedirect } from './pages/RootRedirect'
import { ProjectsPage } from './pages/ProjectsPage'
import { BoardPage } from './pages/BoardPage'
import { VaultPage } from './pages/VaultPage'
import { OverviewPage } from './pages/OverviewPage'
import { CardPage } from './pages/CardPage'
import { SearchPage } from './pages/SearchPage'
import { EventLogPage } from './pages/EventLogPage'
import { SettingsPage } from './pages/SettingsPage'
import { Toaster } from '@/components/ui/toast'
import { AppShell, type Section } from '@/components/wrappers/AppShell'
import { NavigationGuardProvider } from '@/components/wrappers/NavigationGuard'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { buttonVariants } from '@/components/ui/button'

/**
 * Every screen inside the shell needs the project it is scoped to. The child is
 * keyed on the project, so switching projects starts clean while moving between
 * entries or cards inside one keeps the tree, the filter and the loaded data.
 * Keying on the full path remounted the vault page on every click.
 */
function Shelled({ section, children }: { section: Section; children: React.ReactNode }) {
  const { projectKey } = useParams<{ projectKey: string }>()
  return (
    <AppShell projectKey={projectKey} section={section}>
      <Fragment key={`${section}:${projectKey}`}>{children}</Fragment>
    </AppShell>
  )
}

function App() {
  return (
    <NavigationGuardProvider>
      <Toaster />
      <Routes>
        <Route path="/" element={<RootRedirect />} />
        <Route path="/projects" element={<AppShell><ProjectsPage /></AppShell>} />

        <Route
          path="/p/:projectKey"
          element={<Shelled section="overview"><OverviewPage /></Shelled>}
        />
        <Route
          path="/p/:projectKey/b/:boardSlug"
          element={<Shelled section="board"><BoardPage /></Shelled>}
        />
        <Route
          path="/p/:projectKey/card/:cardRef"
          element={<Shelled section="board"><CardPage /></Shelled>}
        />
        <Route
          path="/p/:projectKey/vault"
          element={<Shelled section="vault"><VaultPage /></Shelled>}
        />
        <Route
          path="/p/:projectKey/vault/:slug"
          element={<Shelled section="vault"><VaultPage /></Shelled>}
        />

        <Route path="/search" element={<AppShell><SearchPage /></AppShell>} />
        <Route path="/event-log" element={<AppShell><EventLogPage /></AppShell>} />
        {/* Settings belong to the machine, not a project. The page routes its
            own sections, so the shell stays mounted while moving between them. */}
        <Route path="/settings/*" element={<AppShell section="settings"><SettingsPage /></AppShell>} />

        <Route
          path="*"
          element={
            <Empty className="min-h-svh">
              <EmptyHeader>
                <EmptyTitle>Page not found</EmptyTitle>
                <EmptyDescription>
                  That route does not exist in this workspace.
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Link to="/" className={buttonVariants({ variant: 'outline', size: 'sm' })}>Go to the overview</Link>
              </EmptyContent>
            </Empty>
          }
        />
      </Routes>
    </NavigationGuardProvider>
  )
}

export default App
