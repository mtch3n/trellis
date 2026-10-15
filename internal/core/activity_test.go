package core

import (
	"testing"
)

func insertEvents(t *testing.T, c *Core, projectID, actor string, ts int64, n int) {
	t.Helper()
	for range n {
		if _, err := c.db.Exec(
			`INSERT INTO event (ts, actor, entity_type, entity_id, action, project_id) VALUES (?, ?, 'card', 'c', 'edited', ?)`,
			ts, actor, projectID); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
}

func TestActivity_PPL_C1_actors_listed_and_agents_collapsed_into_sessions(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	now := c.clock.NowMS()
	insertEvents(t, c, p.ID, "human:a", now-2*day, 2)
	insertEvents(t, c, p.ID, "human:a", now-day, 1)
	insertEvents(t, c, p.ID, "agent:x", now-day, 2)
	insertEvents(t, c, p.ID, "agent:y", now-day, 1)

	got, err := c.Activity(t.Context(), p.Key)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(got.Actors) != 1 || got.Actors[0] != (ActorActivity{Actor: "human:a", Events: 3, LastSeen: now - day}) {
		t.Errorf("actors = %+v, want human:a with 3 events last seen %d", got.Actors, now-day)
	}
	if got.Sessions != 2 || got.SessionEvents != 3 {
		t.Errorf("sessions = %d, events = %d, want 2 sessions, 3 events", got.Sessions, got.SessionEvents)
	}
}

func TestActivity_PPL_C2_old_and_other_project_events_are_not_counted(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	other := seededProject2(t, c)
	now := c.clock.NowMS()
	insertEvents(t, c, p.ID, "human:a", now-day, 1)
	insertEvents(t, c, p.ID, "human:a", now-31*day, 4)
	insertEvents(t, c, p.ID, "human:old", now-31*day, 1)
	insertEvents(t, c, p.ID, "agent:old", now-31*day, 1)
	insertEvents(t, c, other.ID, "human:a", now-day, 5)
	insertEvents(t, c, other.ID, "agent:x", now-day, 5)

	got, err := c.Activity(t.Context(), p.Key)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(got.Actors) != 1 || got.Actors[0].Actor != "human:a" || got.Actors[0].Events != 1 {
		t.Errorf("actors = %+v, want only human:a with 1 event", got.Actors)
	}
	if got.Sessions != 0 || got.SessionEvents != 0 {
		t.Errorf("sessions = %d, events = %d, want none", got.Sessions, got.SessionEvents)
	}
}

func TestActivity_PPL_C8_processes_are_unattributed_not_people(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	now := c.clock.NowMS()
	insertEvents(t, c, p.ID, "human:a", now-day, 1)
	insertEvents(t, c, p.ID, "cli:4242", now-day, 2)
	insertEvents(t, c, p.ID, "cli:4243", now-day, 1)
	insertEvents(t, c, p.ID, "daemon:7", now-day, 1)
	insertEvents(t, c, p.ID, "agent:x", now-day, 1)

	got, err := c.Activity(t.Context(), p.Key)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(got.Actors) != 1 || got.Actors[0].Actor != "human:a" {
		t.Errorf("actors = %+v, want only human:a", got.Actors)
	}
	if got.Unattributed != 3 || got.UnattributedEvents != 4 {
		t.Errorf("unattributed = %d, events = %d, want 3 processes, 4 events", got.Unattributed, got.UnattributedEvents)
	}
	if got.Sessions != 1 {
		t.Errorf("sessions = %d, want 1", got.Sessions)
	}
}
