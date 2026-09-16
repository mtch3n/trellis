package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// EventQuery filters a read of the event feed. The zero value reads every
// project's events from the beginning, all kinds, every action but "read".
type EventQuery struct {
	ProjectID string   // "" = every project
	After     int64    // exclusive
	Limit     int      // default 1000, max 5000
	Kinds     []string // card | knowledge | board | label | note; empty = all
	Actions   []string // created, edited, moved, ...; empty = all but read
	DocTypes  []string // knowledge only: finding, decision, ...
	NotActor  string   // skip events written by this actor
}

// FeedEvent is one entry an extension, the CLI or the web timeline can react
// to. Content never ships: old and new are populated only for a card's
// column move, and a deleted entity's ref and title are empty except for its
// own deleted event, whose title is what was recorded at deletion.
type FeedEvent struct {
	Seq    int64  `json:"seq"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Type   string `json:"type,omitempty"`
	Action string `json:"action"`
	Field  string `json:"field,omitempty"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new,omitempty"`
}

// feedRow is what the join returns, before the disclosure policy in
// toFeedEvent decides what of it may leave. Every joined column is
// COALESCE'd to its type's zero value, so "" (or 0 for a seq) means the
// corresponding entity is not the one this event is about, or no longer
// exists.
type feedRow struct {
	Seq       int64  `db:"seq"`
	TS        int64  `db:"ts"`
	Actor     string `db:"actor"`
	Kind      string `db:"kind"`
	Action    string `db:"action"`
	Field     string `db:"field"`
	OldValue  string `db:"old_value"`
	NewValue  string `db:"new_value"`
	CardRef   string `db:"card_ref"`
	CardTitle string `db:"card_title"`
	KBKey     string `db:"kb_key"`
	KBSlug    string `db:"kb_slug"`
	KBTitle   string `db:"kb_title"`
	KBDocType string `db:"kb_doctype"`
	BoardName string `db:"board_name"`
	LabelName string `db:"label_name"`
	NoteKey   string `db:"note_key"`
	NoteSeq   int64  `db:"note_seq"`
	NoteTitle string `db:"note_title"`
}

// toFeedEvent applies the feed's disclosure policy. It is the only place that
// decides what leaves: old/new travel only for a card's column move, and ref
// and title come from the entity's current row, empty when it no longer
// exists, except that a deleted event's own title is what old_value recorded.
func (r feedRow) toFeedEvent() FeedEvent {
	ev := FeedEvent{
		Seq: r.Seq, TS: r.TS, Actor: r.Actor, Kind: r.Kind, Action: r.Action, Field: r.Field,
	}
	if r.Kind == "card" && r.Action == "moved" {
		ev.Old, ev.New = r.OldValue, r.NewValue
	}
	switch r.Kind {
	case "card":
		if r.CardRef != "" {
			ev.Ref = r.CardRef
			ev.Title = r.CardTitle
		}
	case "knowledge":
		if r.KBKey != "" {
			ev.Ref = DocAddress(r.KBKey, r.KBKey == GlobalKey, r.KBSlug)
			ev.Title = r.KBTitle
			ev.Type = r.KBDocType
		}
	case "board":
		if r.BoardName != "" {
			ev.Ref = r.BoardName
			ev.Title = r.BoardName
		}
	case "label":
		if r.LabelName != "" {
			ev.Ref = r.LabelName
			ev.Title = r.LabelName
		}
	case "note":
		if r.NoteKey != "" {
			ev.Ref = r.NoteKey + "-" + itoa(r.NoteSeq)
			ev.Title = r.NoteTitle
		}
	}
	if r.Action == "deleted" {
		ev.Ref = ""
		ev.Title = r.OldValue
	}
	return ev
}

// EventFeed is the one query behind the CLI, the web endpoint and any future
// extension. See toFeedEvent for the disclosure policy and the Global
// Constraints in the plan for why ProjectID scoping cannot reach a
// hard-deleted entity's history.
func (c *Core) EventFeed(ctx context.Context, q EventQuery) ([]FeedEvent, *int64, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}

	kindClause, kindArgs := inClause("e.entity_type", q.Kinds)
	var actionClause string
	var actionArgs []any
	if len(q.Actions) == 0 {
		actionClause = " AND e.action != 'read'"
	} else {
		actionClause, actionArgs = inClause("e.action", q.Actions)
	}
	docTypeClause, docTypeArgs := inClause("k.doc_type", q.DocTypes)
	var actorClause string
	var actorArgs []any
	if q.NotActor != "" {
		actorClause = " AND e.actor != ?"
		actorArgs = []any{q.NotActor}
	}
	var projectClause string
	var projectArgs []any
	if q.ProjectID != "" {
		projectClause = " AND COALESCE(c.project_id, k.project_id, b.project_id, l.project_id, nc.project_id) = ?"
		projectArgs = []any{q.ProjectID}
	}

	args := []any{q.After}
	args = append(args, kindArgs...)
	args = append(args, actionArgs...)
	args = append(args, docTypeArgs...)
	args = append(args, actorArgs...)
	args = append(args, projectArgs...)
	args = append(args, limit)

	query := `
		SELECT
		    e.seq, e.ts, e.actor, e.entity_type AS kind, e.action,
		    COALESCE(e.field, '') AS field,
		    COALESCE(e.old_value, '') AS old_value,
		    COALESCE(e.new_value, '') AS new_value,
		    COALESCE(c.ref, '') AS card_ref,
		    COALESCE(c.title, '') AS card_title,
		    COALESCE(CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE pk.key END, '') AS kb_key,
		    COALESCE(k.slug, '') AS kb_slug,
		    COALESCE(k.title, '') AS kb_title,
		    COALESCE(k.doc_type, '') AS kb_doctype,
		    COALESCE(b.name, '') AS board_name,
		    COALESCE(l.name, '') AS label_name,
		    COALESCE(pn.key, '') AS note_key,
		    COALESCE(nc.seq, 0) AS note_seq,
		    COALESCE(nc.title, '') AS note_title
		FROM event e
		LEFT JOIN card c ON c.id = e.entity_id AND e.entity_type = 'card'
		LEFT JOIN knowledge k ON k.id = e.entity_id AND e.entity_type = 'knowledge'
		LEFT JOIN project pk ON pk.id = k.project_id
		LEFT JOIN board b ON b.id = e.entity_id AND e.entity_type = 'board'
		LEFT JOIN label l ON l.id = e.entity_id AND e.entity_type = 'label'
		LEFT JOIN note n ON n.id = e.entity_id AND e.entity_type = 'note'
		LEFT JOIN card nc ON nc.id = n.card_id
		LEFT JOIN project pn ON pn.id = nc.project_id
		WHERE e.seq > ?
		  AND e.entity_type IN ('card', 'knowledge', 'board', 'label', 'note')` +
		kindClause + actionClause + docTypeClause + actorClause + projectClause + `
		ORDER BY e.seq ASC
		LIMIT ?`

	var rows []feedRow
	if err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		return tx.Select(&rows, query, args...)
	}); err != nil {
		return nil, nil, err
	}

	events := make([]FeedEvent, len(rows))
	for i, r := range rows {
		events[i] = r.toFeedEvent()
	}
	if len(events) == 0 {
		return events, nil, nil
	}
	next := events[len(events)-1].Seq
	return events, &next, nil
}
