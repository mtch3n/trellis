package core

import "github.com/jmoiron/sqlx"

// purgeDisclosedCopies removes the copies of a body that trellis made before
// its author declared it private. Three exist locally:
//
//	knowledge.recap / recap_hash   the stored recap
//	event.new_value  pinned        PinKnowledge writes the recap text there
//	event.new_value  edited        EditKnowledgeFields writes the whole body
//	                               there, which is the largest copy and the one
//	                               nobody thinks to look for
//
// The pin itself is not one of these. A pin row is (id, knowledge_id,
// board_id, created_at) — it holds no text — and it is left alone on purpose.
// Deleting it was never about content; it was about stopping injection, and
// injection is already stopped once recap is NULL: Pins decides what to
// inject from the file, so a surviving pin on a private entry still injects
// only its ref and title.
//
// Leaving the pin also keeps this transaction-safe. This runs inside the
// caller's transaction, from refreshFromFile via loadDoc. UnpinKnowledge
// calls loadDoc and then runs its own DELETE FROM pin; if the purge had
// already deleted the row, that DELETE would affect zero rows, UnpinKnowledge
// would return not_pinned, and Core.Tx would roll back the whole
// transaction — undoing the purge along with everything else. An author who
// marks a document private and then immediately unpins it must not see that.
//
// The purge is self-healing, so this is not the only place it runs. Any
// caller that returns an error after the purge — PinKnowledge's
// recap_required is one — rolls the purge back along with everything else in
// its transaction, including the private mirror this function does not touch
// directly (refreshFromFile sets it just before calling in). The next
// successful read recomputes the false-to-true transition from the file and
// re-attempts the purge. Nothing discloses in the meantime: both Recall and
// Pins decide what to redact from the file, not from the recap column, so a
// rolled-back purge can leave knowledge.recap stale until a later read purges
// again, but that stale value never reaches an agent.
//
// The vector index needs nothing here. Private entries are absent from the
// corpus ListSearchKnowledge returns, and Reconcile builds both its upsert set
// and its prune keep-list from that one list, so the next reconcile evicts the
// chunks as a side effect of not re-embedding them.
//
// Anything already sent to a remote embedder is gone. This cleans up locally
// and makes no wider claim.
func (c *Core) purgeDisclosedCopies(tx *sqlx.Tx, doc *Knowledge) error {
	if _, err := tx.Exec(
		`UPDATE knowledge SET recap = NULL, recap_hash = NULL WHERE id = ?`, doc.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE event SET new_value = NULL, old_value = NULL
		 WHERE entity_type = 'knowledge' AND entity_id = ?
		   AND action IN ('pinned', 'unpinned', 'edited')`,
		doc.ID); err != nil {
		return err
	}
	doc.Recap, doc.RecapHash = nil, nil
	return c.recordEvent(tx, "knowledge", doc.ID, "privatised", "", "", "")
}
