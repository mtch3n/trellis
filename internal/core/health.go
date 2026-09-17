package core

import (
	"context"
	"errors"
	"os"

	"github.com/jmoiron/sqlx"
)

// ReadWindowDays is the window the read counters use.
const ReadWindowDays = 30

// recordRead notes that an entry was actually read. §10.7 sources this from
// `invocation`, but the invocation log deliberately stores no positional
// arguments — the slug is exactly what it drops — so the read is recorded here
// instead, where it is exact rather than parsed back out of a command line.
func (c *Core) recordRead(tx *sqlx.Tx, entryID string) error {
	return c.recordEvent(tx, "entry", entryID, "read", "", "", "")
}

// HealthLine is one row of the housekeeping report. Every line names the
// command that acts on it: a score would hide the reason, and the reason is the
// only actionable part (§10.10).
type HealthLine struct {
	What  string `json:"what"`
	Count int    `json:"count"`
	Fix   string `json:"fix"`
}

// Health counts what a human would want to look at before deciding to tidy.
// Detection is free and runs on demand; action is always human (§10.10).
func (c *Core) Health(ctx context.Context, projectID string) ([]HealthLine, error) {
	diagnostics, err := c.Lint(ctx, projectID)
	if err != nil {
		return nil, err
	}
	byKind := map[string]int{}
	for _, f := range diagnostics {
		byKind[f.Kind]++
	}

	var cold, stale, total int
	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&total, `SELECT COUNT(*) FROM entry WHERE project_id = ?`, projectID); err != nil {
			return err
		}
		if err := tx.Get(&cold, `
			SELECT COUNT(*) FROM entry k
			WHERE k.project_id = ?
			  AND NOT EXISTS (
			    SELECT 1 FROM event e
			    WHERE e.entity_type = 'entry' AND e.entity_id = k.id
			      AND e.action = 'read' AND e.ts > ?)`,
			projectID, c.clock.NowMS()-readWindowMS()); err != nil {
			return err
		}

		pins, err := c.pins(tx, projectID, "", 0)
		if err != nil {
			return err
		}
		stale = 0
		for _, pin := range pins {
			if pin.Stale {
				stale++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	revisions, leftoverRevisions, err := c.RevisionHealth(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return []HealthLine{
		{What: "entries", Count: total, Fix: "trellis vault ls"},
		{What: "never read in " + itoa(ReadWindowDays) + "d", Count: cold, Fix: "trellis vault ls --cold"},
		{What: "stale pinned recaps", Count: stale, Fix: "trellis vault pins --stale"},
		{What: "orphans (no links)", Count: byKind["orphan"], Fix: "trellis vault lint"},
		{What: "stubs", Count: byKind["stub"], Fix: "trellis vault lint"},
		{What: "broken anchors", Count: byKind["broken_anchor"], Fix: "trellis vault lint"},
		{What: "revisions", Count: revisions, Fix: "trellis maintenance prune --revisions"},
		{What: "leftover revision directories", Count: leftoverRevisions, Fix: "trellis maintenance prune --leftover-revisions"},
	}, nil
}

// RevisionHealth counts an entry's retained revisions and the revision
// directories no entry accounts for. A row whose file is missing keeps its
// history and is reported by lint as a missing file, not counted here.
func (c *Core) RevisionHealth(ctx context.Context, projectID string) (revisions, leftover int, err error) {
	var entries []Entry
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		entries, err = c.entriesWithPaths(tx, projectID)
		return err
	}); err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		entries, derr := os.ReadDir(revisionDir(e.Path))
		if errors.Is(derr, os.ErrNotExist) {
			continue
		}
		if derr != nil {
			return 0, 0, derr
		}
		revisions += len(entries)
	}
	leftovers, err := c.leftoverRevisionDirs(ctx, projectID)
	if err != nil {
		return 0, 0, err
	}
	return revisions, len(leftovers), nil
}

func readWindowMS() int64 { return int64(ReadWindowDays) * 24 * 60 * 60 * 1000 }

// ColdEntries lists entries nothing has read inside the window. Cold is a
// candidate for review, never for automatic deletion.
//
// Private comes from the files, not the mirror, so a caller that withholds a
// private entry's content decides on what the author last wrote. An entry
// whose file is gone is reported private: its status cannot be confirmed, and
// one missing file must not fail the whole listing.
func (c *Core) ColdEntries(ctx context.Context, projectID string) ([]Entry, error) {
	entries := []Entry{}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Select(&entries, `
			SELECT k.* FROM entry k
			WHERE k.project_id = ?
			  AND NOT EXISTS (
			    SELECT 1 FROM event e
			    WHERE e.entity_type = 'entry' AND e.entity_id = k.id
			      AND e.action = 'read' AND e.ts > ?)
			ORDER BY k.updated_at`, projectID, c.clock.NowMS()-readWindowMS()); err != nil {
			return err
		}
		ids := make([]string, 0, len(entries))
		for _, e := range entries {
			ids = append(ids, e.ID)
		}
		private, missing, err := c.privateAfterRefresh(tx, ids)
		if err != nil {
			return err
		}
		for i := range entries {
			entries[i].Private = private[entries[i].ID]
			entries[i].Missing = missing[entries[i].ID]
			if err := c.entryView(tx, &entries[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return entries, err
}
