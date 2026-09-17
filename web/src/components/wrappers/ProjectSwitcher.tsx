import { ChevronsUpDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Lamp } from '@/components/wrappers/Lamp'

export interface SwitcherProject {
  key: string
  cards: number
  expired: number
}

/**
 * The project scope control. Switching is navigation, so it is a menu rather
 * than a form select: the popup opens below the trigger instead of over it, is
 * wide enough that no key is cut short, and gives each project the one fact
 * that decides whether it is worth opening. The full table is an action on
 * the heading's row, not a pretend project in the list, and costs no row of
 * its own.
 */
export function ProjectSwitcher({
  current,
  projects,
  onSwitch,
  onAll,
}: {
  current: string
  projects: SwitcherProject[]
  onSwitch: (key: string) => void
  onAll: () => void
}) {
  const here = projects.find((project) => project.key === current)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="sm" className="max-w-56 gap-2 px-2" aria-label={`Project ${current}. Switch project`} />}
      >
        {here && here.expired > 0 && <Lamp state="claimed" label={`${here.expired} expired claims`} />}
        <span className="min-w-0 truncate">{current}</span>
        <ChevronsUpDown data-icon="inline-end" className="text-muted-foreground" />
      </DropdownMenuTrigger>

      <DropdownMenuContent sideOffset={6} className="w-80 p-1.5">
        <DropdownMenuGroup>
          <div className="flex items-center justify-between gap-3">
            <DropdownMenuLabel>Projects</DropdownMenuLabel>
            <DropdownMenuItem className="py-0.5 text-xs text-muted-foreground" onClick={onAll}>
              All projects
            </DropdownMenuItem>
          </div>
          <DropdownMenuRadioGroup value={current} onValueChange={(key: string) => { if (key !== current) onSwitch(key) }}>
            {projects.map((project) => (
              <DropdownMenuRadioItem key={project.key} value={project.key} className="gap-3 py-1.5" title={project.key}>
                <span className="min-w-0 flex-1 truncate">{project.key}</span>
                {project.expired > 0 ? (
                  <span className="flex shrink-0 items-center gap-1.5 text-xs text-claimed">
                    <Lamp state="claimed" label="Expired claims" />
                    {project.expired} expired
                  </span>
                ) : (
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {project.cards === 0 ? 'Empty' : `${project.cards} ${project.cards === 1 ? 'card' : 'cards'}`}
                  </span>
                )}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
