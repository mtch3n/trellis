package core

import (
	"context"
	"strings"
)

// ActivityWindow is how far back Activity looks, in milliseconds: 30 days.
const ActivityWindow = int64(30 * 24 * 60 * 60 * 1000)

// ActorActivity is one actor's events in a project's activity window.
type ActorActivity struct {
	Actor    string `json:"actor" db:"actor"`
	Events   int    `json:"events" db:"events"`
	LastSeen int64  `json:"last_seen" db:"last_seen"`
}

// Activity is who acted on a project in the last ActivityWindow. Agents are
// counted as sessions rather than listed, since each session is its own
// actor.
type Activity struct {
	Actors        []ActorActivity `json:"actors"`
	Sessions      int             `json:"sessions"`
	SessionEvents int             `json:"session_events"`
}

// Activity reads who acted on the project named by key in the last
// ActivityWindow, most recently seen first. Reads are not acts, so they are
// not counted, as in the event log.
func (c *Core) Activity(ctx context.Context, key string) (Activity, error) {
	p, err := c.ProjectByKey(ctx, key)
	if err != nil {
		return Activity{}, err
	}
	var rows []ActorActivity
	err = c.db.SelectContext(ctx, &rows, `
		SELECT actor, COUNT(*) AS events, MAX(ts) AS last_seen
		FROM event
		WHERE project_id = ? AND ts > ? AND action != 'read'
		GROUP BY actor
		ORDER BY last_seen DESC, actor`,
		p.ID, c.clock.NowMS()-ActivityWindow)
	if err != nil {
		return Activity{}, err
	}
	a := Activity{Actors: []ActorActivity{}}
	for _, row := range rows {
		if strings.HasPrefix(row.Actor, "agent:") {
			a.Sessions++
			a.SessionEvents += row.Events
			continue
		}
		a.Actors = append(a.Actors, row)
	}
	return a, nil
}
