package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNominateEntryCountsCitations(t *testing.T) {
	c, p, _ := vaultCore(t)
	target, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Postgres conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateEntry(t.Context(), p.ID, NewEntry{
		Title: "Setup", Body: "Follow [[postgres-conventions]].\n"}); err != nil {
		t.Fatal(err)
	}
	if err := c.NominateEntry(t.Context(), p.ID, target.Slug, "every repo re-derives this"); err != nil {
		t.Fatal(err)
	}
	nominees, err := c.Nominations(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nominees) != 1 || nominees[0].Cited != 1 || nominees[0].Nominations != 1 {
		t.Fatalf("Nominations = %+v, want one with the citation counted", nominees)
	}
}

// nominateTen fills the project's queue with ten waiting entries and returns
// their slugs.
func nominateTen(t *testing.T, c *Core, projectID string) []string {
	t.Helper()
	var slugs []string
	for i := range 10 {
		e, err := c.CreateEntry(t.Context(), projectID, NewEntry{Title: fmt.Sprintf("Waiting %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.NominateEntry(t.Context(), projectID, e.Slug, "holds in any repo"); err != nil {
			t.Fatalf("nominating %s: %v", e.Slug, err)
		}
		slugs = append(slugs, e.Slug)
	}
	return slugs
}

func TestGate_GATE_C8_renominating_a_waiting_entry_updates_its_reason(t *testing.T) {
	c, p, _ := vaultCore(t)
	slugs := nominateTen(t, c, p.ID)

	if err := c.NominateEntry(t.Context(), p.ID, slugs[3], "a sharper reason"); err != nil {
		t.Fatalf("re-nominating a waiting entry: %v", err)
	}
	nominees, err := c.Nominations(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nominees) != 10 {
		t.Fatalf("queue holds %d, want 10", len(nominees))
	}
	for _, n := range nominees {
		if n.Slug == slugs[3] && n.Reason != "a sharper reason" {
			t.Errorf("reason = %q, want the new one", n.Reason)
		}
	}
}

func TestGate_GATE_C9_an_eleventh_nomination_is_refused_until_one_leaves(t *testing.T) {
	c, p, _ := vaultCore(t)
	slugs := nominateTen(t, c, p.ID)
	eleventh, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Eleventh"})
	if err != nil {
		t.Fatal(err)
	}
	var eventsBefore int
	if err := c.db.Get(&eventsBefore, `SELECT COUNT(*) FROM event`); err != nil {
		t.Fatal(err)
	}

	err = c.NominateEntry(t.Context(), p.ID, eleventh.Slug, "holds in any repo")
	ce, ok := errors.AsType[*Error](err)
	if !ok || ce.Exit != 5 || !strings.Contains(ce.Fix, "trellis vault nominations") {
		t.Fatalf("eleventh nomination: %v, want a policy error whose fix names trellis vault nominations", err)
	}
	var rows, eventsAfter int
	if err := c.db.Get(&rows, `SELECT COUNT(*) FROM nomination WHERE entry_id = ?`, eleventh.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.db.Get(&eventsAfter, `SELECT COUNT(*) FROM event`); err != nil {
		t.Fatal(err)
	}
	if rows != 0 || eventsAfter != eventsBefore {
		t.Fatalf("refused nomination wrote %d rows and %d events", rows, eventsAfter-eventsBefore)
	}

	if _, err := c.PromoteEntry(t.Context(), p.ID, slugs[0], "shared"); err != nil {
		t.Fatal(err)
	}
	if err := c.NominateEntry(t.Context(), p.ID, eleventh.Slug, "holds in any repo"); err != nil {
		t.Fatalf("nomination after a promote: %v", err)
	}
}
