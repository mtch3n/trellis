import type { Entry } from '@/lib/entry'
import { sentence } from '@/lib/format'

/**
 * The vault as a file tree. The two scopes are the top folders, the global
 * vault first because it is visible everywhere, and a slug with slashes in it
 * (`deployment/rollback-runbook`) nests the entry in folders named by its
 * leading segments. Folders come before files, as a file explorer shows them.
 */

export interface TreeFolder {
  kind: 'folder'
  /** Unique across the tree: the scope, then the folder segments. */
  path: string
  name: string
  children: TreeNode[]
  /** Files anywhere beneath. */
  count: number
}

export interface TreeFile {
  kind: 'file'
  path: string
  entry: Entry
}

export type TreeNode = TreeFolder | TreeFile

export type TreeSort = 'title' | 'title-desc' | 'updated' | 'created'

/**
 * How the files are grouped inside each scope: by the directories their slugs
 * spell out, or by the template they follow. Grouping by template is a second
 * reading of the same tree, so a template never becomes a directory — the
 * vault-paths design requires directories to come from slugs and nothing else.
 */
export type TreeGroup = 'directory' | 'template'

export const GLOBAL_SCOPE = 'GLOBAL'

export function buildVaultTree(
  entries: Entry[],
  { projectKey, sort, group = 'directory' }: { projectKey: string; sort: TreeSort; group?: TreeGroup },
): TreeFolder[] {
  const roots: TreeFolder[] = [
    { kind: 'folder', path: GLOBAL_SCOPE, name: 'Global vault', children: [], count: 0 },
    { kind: 'folder', path: projectKey, name: projectKey, children: [], count: 0 },
  ]

  for (const entry of entries) {
    const scope = entry.global ? roots[0] : roots[1]
    const segments = entry.slug.split('/')
    const leaf = segments[segments.length - 1]
    if (group === 'template') {
      // One folder per template the scope actually holds, and one for the
      // entries that follow none, which is most of them.
      const name = entry.template ? sentence(entry.template) : 'No template'
      const folder = folderIn(scope, `${scope.path}#${entry.template ?? ''}`, name)
      folder.children.push({ kind: 'file', path: `${folder.path}/${entry.slug}`, entry })
      continue
    }
    let folder = scope
    for (const segment of segments.slice(0, -1)) {
      folder = folderIn(folder, `${folder.path}/${segment}`, segment)
    }
    folder.children.push({ kind: 'file', path: `${folder.path}/${leaf}`, entry })
  }

  for (const root of roots) arrange(root, sort)
  return roots
}

/** The folder at this path inside a parent, made on first use. */
function folderIn(parent: TreeFolder, path: string, name: string): TreeFolder {
  const found = parent.children.find((node): node is TreeFolder => node.kind === 'folder' && node.path === path)
  if (found) return found
  const made: TreeFolder = { kind: 'folder', path, name, children: [], count: 0 }
  parent.children.push(made)
  return made
}


function arrange(folder: TreeFolder, sort: TreeSort): number {
  let count = 0
  for (const node of folder.children) count += node.kind === 'folder' ? arrange(node, sort) : 1
  folder.count = count
  folder.children.sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === 'folder' ? -1 : 1
    if (a.kind === 'folder' || b.kind === 'folder') {
      return (a as TreeFolder).name.localeCompare((b as TreeFolder).name)
    }
    return compareFiles(a.entry, b.entry, sort)
  })
  return count
}

function compareFiles(a: Entry, b: Entry, sort: TreeSort) {
  switch (sort) {
    case 'title':
      return a.title.localeCompare(b.title)
    case 'title-desc':
      return b.title.localeCompare(a.title)
    case 'updated':
      return (b.updated_at ?? 0) - (a.updated_at ?? 0)
    case 'created':
      return (b.created_at ?? 0) - (a.created_at ?? 0)
  }
}

/** Every folder path in the tree, for collapsing all of them at once. */
export function folderPaths(nodes: TreeNode[]): string[] {
  return nodes.flatMap((node) => (node.kind === 'folder' ? [node.path, ...folderPaths(node.children)] : []))
}

/** The folders that hold a file, outermost first, so it can be revealed. */
export function ancestorsOf(nodes: TreeNode[], id: string, trail: string[] = []): string[] | null {
  for (const node of nodes) {
    if (node.kind === 'file') {
      if (node.entry.id === id) return trail
      continue
    }
    const found = ancestorsOf(node.children, id, [...trail, node.path])
    if (found) return found
  }
  return null
}

/**
 * The folders a new entry of this project can go in, as the directory the
 * server takes: `ops`, `ops/db`. The global vault is left out, because entries
 * are created in the project and reach the vault only by promotion.
 */
export function projectFolders(entries: Entry[]): string[] {
  const dirs = new Set<string>()
  for (const entry of entries) {
    if (entry.global) continue
    const segments = entry.slug.split('/').slice(0, -1)
    segments.forEach((_, index) => dirs.add(segments.slice(0, index + 1).join('/')))
  }
  return [...dirs].sort((a, b) => a.localeCompare(b))
}

/** A project folder's tree path as the directory the server takes, or null outside the project. */
export function folderDir(path: string, projectKey: string): string | null {
  if (path === projectKey) return ''
  return path.startsWith(`${projectKey}/`) ? path.slice(projectKey.length + 1) : null
}
