import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { buttonVariants } from '@/components/ui/button'
import { GuardedLink } from '@/components/wrappers/NavigationGuard'
import { SettingsGeneral } from '@/components/wrappers/SettingsGeneral'
import { SettingsLogs } from '@/components/wrappers/SettingsLogs'
import { SettingsMaintenance } from '@/components/wrappers/SettingsMaintenance'
import { SettingsTemplates } from '@/components/wrappers/SettingsTemplates'
import { TemplateEditor } from '@/components/wrappers/TemplateEditor'
import { cn } from '@/lib/utils'

const SECTIONS = [
  { path: 'general', label: 'General' },
  { path: 'templates', label: 'Templates' },
  { path: 'maintenance', label: 'Maintenance' },
  { path: 'logs', label: 'Logs' },
] as const

/**
 * The machine's settings: not a project's, so the shell carries no project,
 * the way the projects list does. A menu of sections on the left, the open
 * section on the right; on a narrow screen the menu sits above as a row.
 *
 * Each section owns its header and its one primary action. The menu's links
 * go through the navigation guard, so leaving a section with unsaved changes
 * asks first.
 */
export function SettingsPage() {
  const { pathname } = useLocation()
  const current = pathname.split('/')[2]

  return (
    <main className="mx-auto w-full max-w-5xl px-6 pb-24 lg:px-8">
      <div className="grid gap-x-10 gap-y-2 pt-4 md:grid-cols-settings">
        <nav aria-label="Settings" className="flex flex-wrap gap-1 md:sticky md:top-shell-gap md:flex-col md:self-start md:pt-4">
          {/* Links styled as quiet buttons: they navigate, so they are announced as links. */}
          {SECTIONS.map((section) => (
            <GuardedLink
              key={section.path}
              to={`/settings/${section.path}`}
              aria-current={current === section.path ? 'page' : undefined}
              className={cn(
                buttonVariants({ variant: 'ghost', size: 'sm' }),
                'justify-start font-normal text-muted-foreground aria-[current=page]:bg-muted aria-[current=page]:text-foreground',
              )}
            >
              {section.label}
            </GuardedLink>
          ))}
        </nav>

        <div className="min-w-0 max-w-3xl">
          <Routes>
            <Route index element={<Navigate to="general" replace />} />
            <Route path="general" element={<SettingsGeneral />} />
            <Route path="templates" element={<SettingsTemplates />} />
            <Route path="templates/:name" element={<TemplateEditor />} />
            <Route path="maintenance" element={<SettingsMaintenance />} />
            <Route path="logs" element={<SettingsLogs />} />
            <Route path="*" element={<Navigate to="general" replace />} />
          </Routes>
        </div>
      </div>
    </main>
  )
}
