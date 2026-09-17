package cli

import (
	"testing"
)

// The events view reads a project's events, a comment titled by its card.
func TestRecentEventsListsCommentsByTheirCard(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "Wire the event log")
	runCmd(t, "card", "comment", "1", "--body", "started")

	c, db, err := openCore()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := c.ProjectByKey(t.Context(), "TEST")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := recentEvents(t.Context(), db, p.ID)
	if err != nil {
		t.Fatalf("recentEvents: %v", err)
	}
	if len(rows) == 0 || rows[0].Entity != "comment" || rows[0].Title != "Wire the event log" {
		t.Fatalf("rows = %+v, want the comment first, titled by its card", rows)
	}
}
