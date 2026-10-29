import { Link, Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { Toaster } from '@/components/ui/toast'
import { buttonVariants } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { AppShell } from '@/components/wrappers/AppShell'
import { RequireSession } from '@/components/wrappers/RequireSession'
import { BoardPage, BoardRedirect } from './pages/BoardPage'
import { CardPage } from './pages/CardPage'
import { LoginPage } from './pages/LoginPage'
import { ProjectsPage } from './pages/ProjectsPage'
import { ReviewPage } from './pages/ReviewPage'
import { SearchPage } from './pages/SearchPage'
import { VaultPage } from './pages/VaultPage'

function Signed() {
  return (
    <RequireSession>
      <AppShell>
        <Outlet />
      </AppShell>
    </RequireSession>
  )
}

function NotFound() {
  return (
    <Empty className="min-h-svh">
      <EmptyHeader>
        <EmptyTitle>Page not found</EmptyTitle>
        <EmptyDescription>That page does not exist.</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Link to="/projects" className={buttonVariants({ variant: 'outline', size: 'sm' })}>Go to the projects</Link>
      </EmptyContent>
    </Empty>
  )
}

function App() {
  return (
    <>
      <Toaster />
      <Routes>
        <Route path="/" element={<Navigate to="/projects" replace />} />
        <Route path="/login" element={<LoginPage />} />
        <Route element={<Signed />}>
          <Route path="/projects" element={<ProjectsPage />} />
          <Route path="/p/:projectKey" element={<Navigate to="board" replace />} />
          <Route path="/p/:projectKey/board" element={<BoardRedirect />} />
          <Route path="/p/:projectKey/b/:boardName" element={<BoardPage />} />
          <Route path="/p/:projectKey/card/:cardRef" element={<CardPage />} />
          <Route path="/p/:projectKey/vault/*" element={<VaultPage />} />
          <Route path="/p/:projectKey/search" element={<SearchPage />} />
          <Route path="/p/:projectKey/review" element={<ReviewPage />} />
        </Route>
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  )
}

export default App
