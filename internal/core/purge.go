package core

import "github.com/jmoiron/sqlx"

// purgeDisclosedCopies removes the copies of a body that trellis made before
// its author declared it private. Three exist locally:
//
//	entry.recap / recap_hash       the stored recap
//	event.new_value  pinned        PinEntry writes the recap text there
//	                               while the entry is still ordinary
//	event.new_value  edited        EditEntryFields writes the whole body
//	                               there, which is the largest copy and the one
//	                               nobody thinks to look for
//	event.new_value  artifact_linked / artifact_unlinked
//	                               EditEntryFields records the artifact's
//	                               name unconditionally, because presence of a
//	                               name is metadata, not content — but once the
//	                               entry is private that name still identifies
//	                               a file that must not linger in the log
//
// The pin itself is not one of these. A pin row is (id, entry_id,
// board_id, created_at) — it holds no text — and it is left alone on purpose.
// Deleting it was never about content; it was about stopping injection, and
// injection is already stopped once recap is NULL: Pins decides what to
// inject from the file, so a surviving pin on a private entry still injects
// only its ref and title.
//
// Leaving the pin also keeps this transaction-safe. This runs inside the
// caller's transaction, from refreshFromFile via loadEntry. UnpinEntry
// calls loadEntry and then runs its own DELETE FROM pin; if the purge had
// already deleted the row, that DELETE would affect zero rows, UnpinEntry
// would return not_pinned, and Core.Tx would roll back the whole
// transaction — undoing the purge along with everything else. An author who
// marks an entry private and then immediately unpins it must not see that.
//
// The purge is self-healing, so this is not the only place it runs. Any
// caller that returns an error after the purge rolls it back along with
// everything else in its transaction — UnpinEntry naming a board that does
// not exist is one, since loadEntry has already purged when boardByName fails —
// and that includes the private mirror this function does not touch directly
// (refreshFromFile sets it just before calling in). The next
// successful read recomputes the false-to-true transition from the file and
// re-attempts the purge. Nothing discloses in the meantime: both Recall and
// Pins decide what to redact from the file, not from the recap column, so a
// rolled-back purge can leave entry.recap stale until a later read purges
// again, but that stale value never reaches an agent.
//
// The vector index needs nothing here. Private entries are absent from the
// corpus ListSearchEntries returns, and Reconcile builds both its upsert set
// and its prune keep-list from that one list, so the next reconcile evicts the
// chunks as a side effect of not re-embedding them.
//
// Anything already sent to a remote embedder is gone. This cleans up locally
// and makes no wider claim.
func (c *Core) purgeDisclosedCopies(tx *sqlx.Tx, entry *Entry) error {
	if _, err := tx.Exec(
		`UPDATE entry SET recap = NULL, recap_hash = NULL WHERE id = ?`, entry.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE event SET new_value = NULL, old_value = NULL
		 WHERE entity_type = 'entry' AND entity_id = ?
		   AND action IN ('pinned', 'unpinned', 'edited', 'artifact_linked', 'artifact_unlinked')`,
		entry.ID); err != nil {
		return err
	}
	entry.Recap, entry.RecapHash = nil, nil
	return c.recordEvent(tx, "entry", entry.ID, "privatized", "", "", "")
}
