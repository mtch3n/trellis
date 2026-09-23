import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { NavLink } from 'react-router-dom'
import { ArrowUpDown, ChevronRight, ChevronsDownUp, ChevronsUpDown, ListFilter, Lock, Network, Plus, Search, Stethoscope } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { IconButton } from '@/components/wrappers/IconButton'
import { sentence } from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  GLOBAL_SCOPE,
  ancestorsOf,
  buildVaultTree,
  folderDir,
  folderPaths,
  type TreeFolder,
  type TreeGroup,
  type TreeNode,
  type TreeSort,
} from '@/lib/vault-tree'
import type { Entry } from '@/lib/entry'

const CLOSED_KEY = 'trellis.vault-tree.closed'
const SORT_KEY = 'trellis.vault-tree.sort'
const GROUP_KEY = 'trellis.vault-tree.group'

/** How an entry was written down. A closed set, so it can be offered as one. */
const PROVENANCES = ['authored', 'prompted', 'extracted'] as const

const SORTS: ReadonlyArray<{ value: TreeSort; label: string }> = [
  { value: 'title', label: 'Title, A to Z' },
  { value: 'title-desc', label: 'Title, Z to A' },
  { value: 'updated', label: 'Recently edited' },
  { value: 'created', label: 'Recently created' },
]

function stored<T>(key: string, fallback: T, parse: (raw: string) => T): T {
  try {
    const raw = localStorage.getItem(key)
    return raw === null ? fallback : parse(raw)
  } catch {
    return fallback
  }
}

/**
 * The navigator, as a file explorer. Entries are markdown files and the
 * scopes are directories, so the sidebar shows that tree rather than inventing
 * one: the global vault and this project as top folders, subfolders where a
 * slug has them, one line per file. Folders remember whether they were open,
 * and the folders around the open entry open themselves when it changes. The
 * arrow keys walk the tree. The graph docks at its foot, passed in as `dock`.
 *
 * New entries start from the toolbar, at the project's top level, or from a
 * project folder's own row, inside it.
 *
 * The selection is one surface that slides to the open row, so moving
 * between entries reads as moving, not as one row blinking off and another on.
 */
export function VaultNav({
  entries,
  vaultCount,
  projectKey = 'Project',
  activeId,
  templates = [],
  dock,
  onOpenGraph,
  onOpenHealth,
  onCreate,
}: {
  entries: Entry[]
  vaultCount: number
  projectKey?: string
  activeId?: string
  /** The templates on this machine, for the template filter. */
  templates?: string[]
  dock: ReactNode
  /** Below the dock's breakpoint the graph is reached from the toolbar. */
  onOpenGraph: () => void
  /** Opens the vault's health, nominations and uptake. */
  onOpenHealth: () => void
  /** Start a new entry in a project folder; "" is the top level. */
  onCreate: (dir: string) => void
}) {
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState<TreeSort>(() =>
    stored(SORT_KEY, 'title', (raw) => (SORTS.some((option) => option.value === raw) ? (raw as TreeSort) : 'title')),
  )
  const [group, setGroup] = useState<TreeGroup>(() =>
    stored(GROUP_KEY, 'directory', (raw) => (raw === 'template' ? 'template' : 'directory')),
  )
  // Which templates and provenances are being kept. Empty means every one,
  // so a filter starts by saying nothing rather than by listing everything.
  const [keptTemplates, setKeptTemplates] = useState<string[]>([])
  const [keptProvenances, setKeptProvenances] = useState<string[]>([])
  const [closed, setClosed] = useState<Set<string>>(() =>
    stored(CLOSED_KEY, new Set<string>(), (raw) => new Set(JSON.parse(raw) as string[])),
  )
  const list = useRef<HTMLDivElement>(null)
  const selection = useRef<HTMLDivElement>(null)

  useEffect(() => {
    try { localStorage.setItem(CLOSED_KEY, JSON.stringify([...closed])) } catch { /* private mode */ }
  }, [closed])
  useEffect(() => {
    try { localStorage.setItem(SORT_KEY, sort) } catch { /* private mode */ }
  }, [sort])
  useEffect(() => {
    try { localStorage.setItem(GROUP_KEY, group) } catch { /* private mode */ }
  }, [group])

  const tree = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const matching = entries.filter((entry) => {
      if (keptTemplates.length > 0 && !keptTemplates.includes(entry.template ?? '')) return false
      if (keptProvenances.length > 0 && !keptProvenances.includes(entry.provenance ?? '')) return false
      if (!needle) return true
      return entry.title.toLowerCase().includes(needle) ||
        entry.slug.toLowerCase().includes(needle) ||
        (entry.template ?? '').toLowerCase().includes(needle)
    })
    return buildVaultTree(matching, { projectKey, sort, group })
  }, [entries, filter, projectKey, sort, group, keptTemplates, keptProvenances])
  const folders = useMemo(() => folderPaths(tree), [tree])
  const allClosed = folders.length > 0 && folders.every((path) => closed.has(path))

  // Opening an entry reveals it: the folders around it open, once. Adjusted
  // while rendering, so the row is on screen when the selection moves to it.
  const [revealed, setRevealed] = useState<string | undefined>(undefined)
  if (activeId !== revealed) {
    setRevealed(activeId)
    const trail = activeId ? ancestorsOf(tree, activeId) : null
    if (trail?.some((path) => closed.has(path))) {
      setClosed(new Set([...closed].filter((path) => !trail.includes(path))))
    }
  }

  const setOpen = useCallback((path: string, open: boolean) => {
    setClosed((current) => {
      const next = new Set(current)
      if (open) next.delete(path)
      else next.add(path)
      return next
    })
  }, [])

  const place = useCallback(() => {
    const surface = selection.current
    const container = list.current
    const row = container?.querySelector<HTMLElement>('[aria-current="page"]')
    if (!surface || !container) return
    if (!row) {
      surface.setAttribute('data-shown', 'false')
      return
    }
    const box = container.getBoundingClientRect()
    const rect = row.getBoundingClientRect()
    surface.style.translate = `${rect.left - box.left}px ${rect.top - box.top}px`
    surface.style.width = `${rect.width}px`
    surface.style.height = `${rect.height}px`
    // The first placement is instant; only later moves slide.
    requestAnimationFrame(() => surface.setAttribute('data-shown', 'true'))
  }, [])

  // After every render the open row can have moved: the route, the filter, the
  // sort. While a folder opens or closes the rows move without a render, so
  // the list's size is watched too.
  useLayoutEffect(place)
  useEffect(() => {
    const node = list.current
    if (!node) return
    const observer = new ResizeObserver(place)
    observer.observe(node)
    return () => observer.disconnect()
  }, [place])

  const base = `/p/${projectKey}/vault`
  const filtering = filter.trim() !== '' || keptTemplates.length > 0 || keptProvenances.length > 0

  return (
    <nav aria-label="Vault" className="flex flex-col border-border max-lg:border-b lg:h-full">
      {/* Two rows, so the filter gets the width to be read and each tool
          room to be hit. */}
      <div className="flex shrink-0 flex-col gap-1.5 px-3 pt-4 pb-2">
        <InputGroup>
          <InputGroupAddon>
            <Search />
          </InputGroupAddon>
          <InputGroupInput
            aria-label="Filter entries"
            placeholder="Filter"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </InputGroup>
        <div className="flex items-center justify-between">
          <IconButton label="New entry" size="icon" onClick={() => onCreate('')}>
            <Plus className="size-4.5" />
          </IconButton>
          <FilterMenu
            templates={templates}
            keptTemplates={keptTemplates}
            keptProvenances={keptProvenances}
            group={group}
            onTemplates={setKeptTemplates}
            onProvenances={setKeptProvenances}
            onGroup={setGroup}
          />
          <SortMenu sort={sort} onSort={setSort} />
          <IconButton
            label={allClosed ? 'Expand all' : 'Collapse all'}
            size="icon"
            disabled={folders.length === 0}
            onClick={() => setClosed(allClosed ? new Set() : new Set(folders))}
          >
            {allClosed ? <ChevronsUpDown className="size-4.5" /> : <ChevronsDownUp className="size-4.5" />}
          </IconButton>
          <IconButton label="Vault health" size="icon" onClick={onOpenHealth}>
            <Stethoscope className="size-4.5" />
          </IconButton>
          <IconButton label="Open the graph" size="icon" className="lg:hidden" onClick={onOpenGraph}>
            <Network className="size-4.5" />
          </IconButton>
        </div>
      </div>

      <div className="min-h-0 flex-1 scroll-fade-y overflow-y-auto px-2 pb-6 max-lg:max-h-80">
        <div ref={list} className="relative pt-1" onKeyDown={walk}>
          <div
            ref={selection}
            aria-hidden="true"
            data-shown="false"
            className="pointer-events-none absolute top-0 left-0 bg-accent opacity-0 data-[shown=true]:opacity-100 data-[shown=true]:transition-all data-[shown=true]:duration-200 data-[shown=true]:ease-settle"
          />
          <ul className="flex flex-col gap-1">
            {tree.map((root) => (
              <Folder
                key={root.path}
                folder={root}
                depth={0}
                base={base}
                activeId={activeId}
                isOpen={(path) => filtering || !closed.has(path)}
                onOpenChange={setOpen}
                createIn={(path) => {
                  const dir = folderDir(path, projectKey)
                  return dir === null ? undefined : () => onCreate(dir)
                }}
                empty={
                  filtering
                    ? 'No match'
                    : root.path === GLOBAL_SCOPE && vaultCount === 0
                      ? 'Empty. Agents nominate entries, and promoting one moves it here.'
                      : 'Nothing written yet'
                }
              />
            ))}
          </ul>
        </div>
      </div>

      <div className="hidden lg:contents">{dock}</div>
    </nav>
  )
}

/**
 * The arrow keys walk the rows on screen: up and down move, right opens a
 * folder or steps into it, left closes it or steps out to its folder.
 */
function walk(event: KeyboardEvent<HTMLElement>) {
  const rows = [...event.currentTarget.querySelectorAll<HTMLElement>('[data-tree-row]')]
  const index = rows.indexOf(document.activeElement as HTMLElement)
  if (index < 0) return
  const row = rows[index]
  const expanded = row.getAttribute('aria-expanded')
  switch (event.key) {
    case 'ArrowDown':
      rows[index + 1]?.focus()
      break
    case 'ArrowUp':
      rows[index - 1]?.focus()
      break
    case 'Home':
      rows[0]?.focus()
      break
    case 'End':
      rows[rows.length - 1]?.focus()
      break
    case 'ArrowRight':
      if (expanded === 'false') row.click()
      else if (expanded === 'true') rows[index + 1]?.focus()
      else return
      break
    case 'ArrowLeft':
      if (expanded === 'true') row.click()
      else row.closest('li')?.parentElement?.closest('li')?.querySelector<HTMLElement>('[data-tree-row]')?.focus()
      break
    default:
      return
  }
  event.preventDefault()
}

/**
 * What the tree shows and how it is grouped: the templates and provenances
 * kept, and whether the files sit in the directories their slugs spell out or
 * under the template they follow. Nothing chosen means everything, so the
 * menu opens saying nothing rather than listing the whole vault back.
 *
 * Grouping by template is how a reader asks "what decisions do we have?"
 * without a decisions directory, which the vault-paths design rules out.
 */
function FilterMenu({ templates, keptTemplates, keptProvenances, group, onTemplates, onProvenances, onGroup }: {
  templates: string[]
  keptTemplates: string[]
  keptProvenances: string[]
  group: TreeGroup
  onTemplates: (templates: string[]) => void
  onProvenances: (provenances: string[]) => void
  onGroup: (group: TreeGroup) => void
}) {
  const active = keptTemplates.length + keptProvenances.length > 0
  const toggle = (values: string[], value: string) =>
    values.includes(value) ? values.filter((kept) => kept !== value) : [...values, value]

  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger
          render={
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="Filter and group"
                  className={cn(active && 'bg-muted text-foreground')}
                />
              }
            />
          }
        >
          <ListFilter className="size-4.5" />
        </TooltipTrigger>
        <TooltipContent side="bottom">Filter and group</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Group files by</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={group} onValueChange={(value: TreeGroup) => onGroup(value)}>
            <DropdownMenuRadioItem value="directory">Directory</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="template">Template</DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLabel>Template</DropdownMenuLabel>
          <DropdownMenuCheckboxItem
            checked={keptTemplates.includes('')}
            onCheckedChange={() => onTemplates(toggle(keptTemplates, ''))}
          >
            No template
          </DropdownMenuCheckboxItem>
          {templates.map((template) => (
            <DropdownMenuCheckboxItem
              key={template}
              checked={keptTemplates.includes(template)}
              onCheckedChange={() => onTemplates(toggle(keptTemplates, template))}
            >
              {sentence(template)}
            </DropdownMenuCheckboxItem>
          ))}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLabel>Written down as</DropdownMenuLabel>
          {PROVENANCES.map((provenance) => (
            <DropdownMenuCheckboxItem
              key={provenance}
              checked={keptProvenances.includes(provenance)}
              onCheckedChange={() => onProvenances(toggle(keptProvenances, provenance))}
            >
              {sentence(provenance)}
            </DropdownMenuCheckboxItem>
          ))}
        </DropdownMenuGroup>
        {active && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => { onTemplates([]); onProvenances([]) }}>
              Clear the filters
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** How files are ordered inside each folder. Folders always come first. */
function SortMenu({ sort, onSort }: { sort: TreeSort; onSort: (sort: TreeSort) => void }) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger
          render={<DropdownMenuTrigger render={<Button variant="ghost" size="icon" aria-label="Sort" />} />}
        >
          <ArrowUpDown className="size-4.5" />
        </TooltipTrigger>
        <TooltipContent side="bottom">Sort</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Sort files by</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={sort} onValueChange={(value: TreeSort) => onSort(value)}>
            {SORTS.map((option) => (
              <DropdownMenuRadioItem key={option.value} value={option.value}>
                {option.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * A folder row and, when open, what it holds. Everything inside hangs from a
 * faint guide under the chevron, so depth reads at a glance.
 */
function Folder({
  folder,
  depth,
  base,
  activeId,
  isOpen,
  onOpenChange,
  createIn,
  empty,
}: {
  folder: TreeFolder
  depth: number
  base: string
  activeId?: string
  isOpen: (path: string) => boolean
  onOpenChange: (path: string, open: boolean) => void
  /** How to start an entry in a folder, where one can be started. */
  createIn: (path: string) => (() => void) | undefined
  empty?: string
}) {
  const create = createIn(folder.path)
  return (
    <Collapsible
      open={isOpen(folder.path)}
      onOpenChange={(open) => onOpenChange(folder.path, open)}
      render={<li />}
    >
      {/* The new-entry button sits over the count, beside the row rather
          than in it, because a button cannot hold another. */}
      <div className="group/folder relative">
        <CollapsibleTrigger
          data-tree-row
          render={
            <Button
              variant="ghost"
              size="sm"
              className="h-7 w-full justify-start gap-1.5 px-2 font-normal text-foreground/85 hover:text-foreground aria-expanded:bg-transparent aria-expanded:hover:bg-muted"
            />
          }
        >
          <ChevronRight
            data-icon="inline-start"
            className="text-muted-foreground transition-transform duration-200 ease-settle group-aria-expanded/button:rotate-90"
          />
          <span className={cn('min-w-0 truncate', depth === 0 && 'font-medium')}>{folder.name}</span>
          <span
            className={cn(
              'ml-auto text-xs text-muted-foreground',
              create && 'group-hover/folder:opacity-0 pointer-coarse:opacity-0',
            )}
          >
            {folder.count}
          </span>
        </CollapsibleTrigger>
        {create && (
          <IconButton
            label={`New entry in ${folder.name}`}
            size="icon-xs"
            side="right"
            // Opaque, so a button shown by keyboard focus covers the count. A
            // touch screen cannot point, so there it is always shown.
            className="absolute top-0.5 right-1 bg-muted opacity-0 group-hover/folder:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
            onClick={create}
          >
            <Plus className="size-4" />
          </IconButton>
        )}
      </div>

      <CollapsibleContent className="h-(--collapsible-panel-height) overflow-hidden transition-all duration-200 ease-settle data-ending-style:h-0 data-starting-style:h-0">
        {folder.children.length === 0 ? (
          empty && <p className="py-1 pr-2 pl-7 text-xs text-pretty text-muted-foreground">{empty}</p>
        ) : (
          <ul className="ml-3.5 flex flex-col border-l border-border pl-1">
            {folder.children.map((node: TreeNode) =>
              node.kind === 'folder' ? (
                <Folder
                  key={node.path}
                  folder={node}
                  depth={depth + 1}
                  base={base}
                  activeId={activeId}
                  isOpen={isOpen}
                  onOpenChange={onOpenChange}
                  createIn={createIn}
                />
              ) : (
                <li key={node.path}>
                  <NavLink
                    data-tree-row
                    to={`${base}/${encodeURIComponent(node.entry.slug)}`}
                    aria-current={node.entry.id === activeId ? 'page' : undefined}
                    className={cn(
                      'relative flex h-7 items-center gap-1.5 px-2 text-sm outline-none transition-colors duration-150 focus-visible:ring-1 focus-visible:ring-ring',
                      node.entry.id === activeId
                        ? 'text-foreground'
                        : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                    )}
                  >
                    {/* The chevron's width, so a file's name lines up with its sibling folders'. */}
                    <span aria-hidden="true" className="size-3.5 shrink-0" />
                    <span className="min-w-0 flex-1 truncate">{node.entry.title}</span>
                    {node.entry.private && <Lock aria-label="Private" className="size-3 shrink-0" />}
                  </NavLink>
                </li>
              ),
            )}
          </ul>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}
