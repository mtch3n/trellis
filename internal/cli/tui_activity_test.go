package cli

import (
	"testing"
)

// The activity view reads a project's events, a comment titled by its card.
func TestRecentActivityListsCommentsByTheirCard(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "Wire the feed")
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
	rows, err := recentActivity(t.Context(), db, p.ID)
	if err != nil {
		t.Fatalf("recentActivity: %v", err)
	}
	if len(rows) == 0 || rows[0].Kind != "comment" || rows[0].Title != "Wire the feed" {
		t.Fatalf("rows = %+v, want the comment first, titled by its card", rows)
	}
}
