import { sentence } from '@/lib/format'

/**
 * What an event's action is called. Every action the server writes has a word
 * here, so a log never shows a raw `set_default`, and an action added later
 * still reads as words until it is named.
 */
const ACTIONS: Record<string, string> = {
  created: 'Created',
  edited: 'Edited',
  moved: 'Moved',
  deleted: 'Deleted',
  claimed: 'Claimed',
  stolen: 'Stolen',
  released: 'Released',
  renewed: 'Renewed',
  archived: 'Archived',
  restored: 'Restored',
  blocked: 'Blocked',
  unblocked: 'Unblocked',
  linked: 'Linked',
  labeled: 'Labeled',
  unlabeled: 'Unlabeled',
  tagged: 'Tagged',
  untagged: 'Untagged',
  pinned: 'Pinned',
  unpinned: 'Unpinned',
  nominated: 'Nominated',
  promoted: 'Promoted',
  demoted: 'Demoted',
  verified: 'Verified',
  privatized: 'Made private',
  injected: 'Injected',
  read: 'Read',
  reloaded: 'Reloaded',
  renamed: 'Renamed',
  set_default: 'Set as default',
  merged: 'Merged',
  rebound: 'Rebound',
}

export function actionLabel(action: string) {
  return ACTIONS[action] ?? sentence(action.replace(/_/g, ' '))
}
