/** A board as the project and board routes describe one. */
export interface BoardInfo {
  name: string
  slug: string
  /** The board a project opens on. */
  is_default?: boolean
}

/**
 * The board a project opens on: the one marked default, else the first it
 * has. A project always has one, so a caller with none has not loaded yet.
 *
 * The shell picks differently on purpose — the board in the URL wins there —
 * but everywhere else that needs "this project's board", this is it.
 */
export function defaultBoard<T extends BoardInfo>(boards: T[]): T | undefined {
  return boards.find((board) => board.is_default) ?? boards[0]
}
