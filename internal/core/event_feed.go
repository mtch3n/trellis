package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

// eventKinds lists every entity_type EventFeed's WHERE clause matches. A
// --kind naming anything else -- a stale name from before a rename, such as
// "note" before migration 0021 renamed it to "comment" -- must fail loudly
// rather than quietly match nothing.
var eventKinds = []string{"card", "entry", "board", "label", "comment"}

// EventQuery filters a read of the event feed. The zero value reads every
// project's events from the beginning, all kinds, every action but "read".
type EventQuery struct {
	ProjectID string   // "" = every project
	After     int64    // exclusive
	Limit     int      // default 1000, max 5000
	Kinds     []string // card | entry | board | label | comment; empty = all
	Actions   []string // created, edited, moved, ...; empty = all but read
	Templates []string // entries only: finding, decision, ...
	NotActor  string   // skip events written by this actor
}

// FeedEvent is one entry an extension, the CLI or the web timeline can react
// to. Content never ships: old and new are populated only for a card's
// column move, and a deleted entity's ref and title are empty except for its
// own deleted event, whose title is what was recorded at deletion.
type FeedEvent struct {
	Seq      int64  `json:"seq"`
	TS       int64  `json:"ts"`
	Actor    string `json:"actor"`
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Title    string `json:"title"`
	Template string `json:"template,omitempty"`
	Action   string `json:"action"`
	Field    string `json:"field,omitempty"`
	Old      string `json:"old,omitempty"`
	New      string `json:"new,omitempty"`
}

// feedRow is what the join returns, before the disclosure policy in
// toFeedEvent decides what of it may leave. Every joined column is
// COALESCE'd to its type's zero value, so "" (or 0 for a seq) means the
// corresponding entity is not the one this event is about, or no longer
// exists.
type feedRow struct {
	Seq           int64  `db:"seq"`
	TS            int64  `db:"ts"`
	Actor         string `db:"actor"`
	Kind          string `db:"kind"`
	Action        string `db:"action"`
	Field         string `db:"field"`
	OldValue      string `db:"old_value"`
	NewValue      string `db:"new_value"`
	CardRef       string `db:"card_ref"`
	CardTitle     string `db:"card_title"`
	EntryKey      string `db:"entry_key"`
	EntrySlug     string `db:"entry_slug"`
	EntryTitle    string `db:"entry_title"`
	EntryTemplate string `db:"entry_template"`
	BoardName     string `db:"board_name"`
	LabelName     string `db:"label_name"`
	CommentRef    string `db:"comment_ref"`
	CommentTitle  string `db:"comment_title"`
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
	case "entry":
		if r.EntryKey != "" {
			ev.Ref = EntryAddress(r.EntryKey, r.EntryKey == GlobalKey, r.EntrySlug)
			ev.Title = r.EntryTitle
			ev.Template = r.EntryTemplate
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
	case "comment":
		if r.CommentRef != "" {
			ev.Ref = r.CommentRef
			ev.Title = r.CommentTitle
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
	for _, k := range q.Kinds {
		if !slices.Contains(eventKinds, k) {
			return nil, nil, ErrUsage("unknown_event_kind",
				fmt.Sprintf("%q is not an event kind: %s", k, strings.Join(eventKinds, ", ")),
				"trellis events --kind "+strings.Join(eventKinds, "|"))
		}
	}
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
	// A deleted entry's row is gone, and its template with it, so its
	// deleted event passes a template filter rather than vanish from it.
	templateClause, templateArgs := inClause("k.template", q.Templates)
	if templateClause != "" {
		templateClause = " AND (" + strings.TrimPrefix(templateClause, " AND ") +
			" OR (e.entity_type = 'entry' AND e.action = 'deleted'))"
	}
	var actorClause string
	var actorArgs []any
	if q.NotActor != "" {
		actorClause = " AND e.actor != ?"
		actorArgs = []any{q.NotActor}
	}
	var projectClause string
	var projectArgs []any
	if q.ProjectID != "" {
		projectClause = " AND e.project_id = ?"
		projectArgs = []any{q.ProjectID}
	}

	args := []any{q.After}
	args = append(args, kindArgs...)
	args = append(args, actionArgs...)
	args = append(args, templateArgs...)
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
		    COALESCE(CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE pk.key END, '') AS entry_key,
		    COALESCE(k.slug, '') AS entry_slug,
		    COALESCE(k.title, '') AS entry_title,
		    COALESCE(k.template, '') AS entry_template,
		    COALESCE(b.name, '') AS board_name,
		    COALESCE(l.name, '') AS label_name,
		    COALESCE(nc.ref, '') AS comment_ref,
		    COALESCE(nc.title, '') AS comment_title
		FROM event e
		LEFT JOIN card c ON c.id = e.entity_id AND e.entity_type = 'card'
		LEFT JOIN entry k ON k.id = e.entity_id AND e.entity_type = 'entry'
		LEFT JOIN project pk ON pk.id = k.project_id
		LEFT JOIN board b ON b.id = e.entity_id AND e.entity_type = 'board'
		LEFT JOIN label l ON l.id = e.entity_id AND e.entity_type = 'label'
		LEFT JOIN comment cm ON cm.id = e.entity_id AND e.entity_type = 'comment'
		LEFT JOIN card nc ON nc.id = cm.card_id
		WHERE e.seq > ?
		  AND e.entity_type IN ('card', 'entry', 'board', 'label', 'comment')` +
		kindClause + actionClause + templateClause + actorClause + projectClause + `
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
