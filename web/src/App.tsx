import { Routes, Route } from 'react-router-dom'
import { ProjectsPage } from './pages/ProjectsPage'
import { BoardPage } from './pages/BoardPage'
import { KnowledgePage } from './pages/KnowledgePage'
import { SearchPage } from './pages/SearchPage'
import { ActivityPage } from './pages/ActivityPage'
import { ProjectPage } from './pages/ProjectPage'
import { CardPage } from './pages/CardPage'
import { ProjectKnowledgePage } from './pages/ProjectKnowledgePage'
import './App.css'

function App() {
  return (
    <Routes>
      <Route path="/" element={<ProjectsPage />} />
      <Route path="/p/:projectKey" element={<ProjectPage />} />
      <Route path="/p/:projectKey/card/:cardRef" element={<CardPage />} />
      <Route path="/p/:projectKey/kb" element={<ProjectKnowledgePage />} />
      <Route path="/global/kb" element={<ProjectKnowledgePage />} />
      <Route path="/p/:projectKey/b/:boardSlug" element={<BoardPage />} />
      <Route path="/p/:projectKey/b/:boardSlug/kb" element={<KnowledgePage />} />
      <Route path="/search" element={<SearchPage />} />
      <Route path="/activity" element={<ActivityPage />} />
    </Routes>
  )
}

export default App
