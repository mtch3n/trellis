package core

import "testing"

// inject runs a recording recall and returns how many hits it marked.
func inject(t *testing.T, c *Core, projectID, text string) int {
	t.Helper()
	hits, err := c.Recall(t.Context(), projectID, text, RecallOpts{Record: true, Limit: 2})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	return len(hits)
}

func uptakeFor(t *testing.T, c *Core, projectID, provenance string) RecallUptake {
	t.Helper()
	rows, err := c.RecallUptake(t.Context(), projectID)
	if err != nil {
		t.Fatalf("RecallUptake: %v", err)
	}
	for _, r := range rows {
		if r.Provenance == provenance {
			return r
		}
	}
	return RecallUptake{Provenance: provenance}
}

func TestRecallOnlyRecordsWhenAsked(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Retry budget", Summary: "retry budget",
	}); err != nil {
		t.Fatal(err)
	}
	// Looking around must not enter the measurement.
	if _, err := c.Recall(ctx, p.ID, "retry budget", RecallOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := uptakeFor(t, c, p.ID, "authored"); got.Injected != 0 {
		t.Errorf("an unrecorded recall was counted: %+v", got)
	}
}

func TestUptakePairsAnInjectionWithTheReadThatFollows(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Retry budget", Summary: "retry budget",
	})
	if err != nil {
		t.Fatal(err)
	}

	if n := inject(t, c, p.ID, "retry budget"); n != 1 {
		t.Fatalf("recall returned %d hits, want 1", n)
	}
	if got := uptakeFor(t, c, p.ID, "authored"); got.Injected != 1 || got.Opened != 0 {
		t.Fatalf("before the read: %+v, want injected 1 opened 0", got)
	}

	if _, err := c.ReadEntry(ctx, p.ID, entry.Slug); err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if got := uptakeFor(t, c, p.ID, "authored"); got.Injected != 1 || got.Opened != 1 {
		t.Errorf("after the read: %+v, want injected 1 opened 1", got)
	}
}

func TestUptakeIgnoresAReadThatCameFirst(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Retry budget", Summary: "retry budget",
	})
	if err != nil {
		t.Fatal(err)
	}
	// An entry the agent had already opened is not evidence that the later
	// injection did anything.
	if _, err := c.ReadEntry(ctx, p.ID, entry.Slug); err != nil {
		t.Fatal(err)
	}
	inject(t, c, p.ID, "retry budget")
	if got := uptakeFor(t, c, p.ID, "authored"); got.Opened != 0 {
		t.Errorf("%+v, want opened 0: the read came before the injection", got)
	}
}

func TestUptakeDoesNotCreditAnotherSessionsRead(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Retry budget", Summary: "retry budget",
	})
	if err != nil {
		t.Fatal(err)
	}
	inject(t, c, p.ID, "retry budget")

	// actor is the session key. A read from a different session says nothing
	// about whether this injection was useful.
	if _, err := c.db.Exec(
		`INSERT INTO event (ts, actor, entity_type, entity_id, action)
		 VALUES (?, 'agent:someone-else', 'entry', ?, 'read')`,
		c.clock.NowMS(), entry.ID); err != nil {
		t.Fatal(err)
	}
	if got := uptakeFor(t, c, p.ID, "authored"); got.Opened != 0 {
		t.Errorf("%+v, want opened 0: the read belongs to another session", got)
	}
}

func TestUptakeSeparatesIngestionPaths(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	authored, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Retry budget authored", Summary: "retry budget", Provenance: "authored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Retry budget extracted", Summary: "retry budget", Provenance: "extracted",
	}); err != nil {
		t.Fatal(err)
	}

	if n := inject(t, c, p.ID, "retry budget"); n != 2 {
		t.Fatalf("recall returned %d hits, want both entries", n)
	}
	if _, err := c.ReadEntry(ctx, p.ID, authored.Slug); err != nil {
		t.Fatal(err)
	}

	// Comparing the paths is the entire reason provenance is recorded.
	if got := uptakeFor(t, c, p.ID, "authored"); got.Injected != 1 || got.Opened != 1 {
		t.Errorf("authored = %+v, want 1/1", got)
	}
	if got := uptakeFor(t, c, p.ID, "extracted"); got.Injected != 1 || got.Opened != 0 {
		t.Errorf("extracted = %+v, want 1/0", got)
	}
}

func TestUptakeRecordsEvenWhenTheLimitIsReached(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()
	for _, title := range []string{"Retry budget", "Retry jitter", "Retry ceiling"} {
		if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
			Title: title, Summary: "retry budget",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Filling the limit used to leave the assembly loop early and skip
	// recording entirely, so this only shows up when the limit actually binds.
	if n := inject(t, c, p.ID, "retry budget"); n != 2 {
		t.Fatalf("recall returned %d hits", n)
	}
	if got := uptakeFor(t, c, p.ID, "authored"); got.Injected != 2 {
		t.Errorf("%+v, want 2 injected: a full result set must still be recorded", got)
	}
}
