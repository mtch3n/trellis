package core

import (
	"encoding/json/v2"
	"testing"
)

// TestJSONNamesUseTheGlossary pins the names core's types carry on the wire
// (spec §5, "JSON"), so renaming a Go field cannot move them. The vocabulary
// test keeps the retired names from coming back.
func TestJSONNamesUseTheGlossary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  []string
	}{
		{"card", Card{ClaimedBy: new("agent:a"), ClaimUntil: new(int64(1))}, []string{"claimed_by", "claim_until"}},
		{"contention", ContentionInfo{}, []string{"claimed_by"}},
		{"search hit", SearchHit{Unverified: true}, []string{"kind", "unverified"}},
		{"diagnostic", Diagnostic{}, []string{"kind", "entry"}},
		{"merge plan", MergePlan{}, []string{"entries", "entries_rewritten", "markers"}},
		{"nomination", Nomination{}, []string{"nominations"}},
		{"proposed write", ProposedWrite{Entity: "card", EntityID: "c1"}, []string{"entity", "entity_id"}},
		{"event", LogEvent{}, []string{"entity"}},
	} {
		raw, err := json.Marshal(tc.value)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for _, key := range tc.want {
			if _, ok := got[key]; !ok {
				t.Errorf("%s: no %q in %s", tc.name, key, raw)
			}
		}
	}
}

// TestRetiredErrorCodesAreGone provokes each path whose code was renamed. The
// vocabulary test cannot stand in for this one: none of these codes contains a
// retired word, so an alias left behind would pass every other gate.
func TestRetiredErrorCodesAreGone(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()
	free, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "claimed by nobody"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "claimed by another actor"})
	if err != nil {
		t.Fatal(err)
	}
	other := New(c.db, c.clock, "sess:other", c.root)
	if _, err := other.ClaimCard(ctx, theirs.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	// A project with a global entry and no claim at all, for the one refusal
	// that a live claim would answer first.
	promoted, pp, _ := vaultCore(t)
	entry, err := promoted.CreateEntry(ctx, pp.ID, NewEntry{Title: "Shared conventions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := promoted.PromoteEntry(ctx, pp.ID, entry.Slug, "every repo re-derives this"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		run     func() error
		want    string
		retired string
	}{
		{"a board that does not exist", func() error {
			_, err := c.SelectBoard(ctx, p.ID, "nope")
			return err
		}, "board_not_found", "unknown_board"},
		{"a column that does not exist", func() error {
			_, err := c.MoveCard(ctx, p.ID, b.ID, CardRef{UUID: free.ID}, "nope")
			return err
		}, "column_not_found", "unknown_column"},
		{"an entity the event log does not have", func() error {
			_, _, err := c.EventLog(ctx, EventQuery{Entities: []string{"ticket"}})
			return err
		}, "unknown_entity", "unknown_event_kind"},
		{"releasing a card nobody claims", func() error {
			return c.ReleaseCard(ctx, free.ID)
		}, "not_yours", "not_owned"},
		{"editing a card another actor claims", func() error {
			_, err := c.EditCard(ctx, p.ID, CardRef{UUID: theirs.ID}, CardEdit{AddTags: []string{"seen"}})
			return err
		}, "contention", "not_owned"},
		// The next one's retired name is built from a retired word, so the
		// vocabulary test already forbids it tree-wide; writing it out here
		// would only add an allowlist line of its own.
		{"deleting a project an agent is working in", func() error {
			return c.DeleteProject(ctx, p.Key)
		}, "project_has_claims", ""},
		{"deleting a project whose entries went global", func() error {
			return promoted.DeleteProject(ctx, pp.Key)
		}, "project_has_global_entries", "project_has_vault_entries"},
	} {
		got := errCodeOf(tc.run())
		if tc.retired != "" && got == tc.retired {
			t.Errorf("%s: retired code %q", tc.name, got)
		}
		if got != tc.want {
			t.Errorf("%s: code = %q, want %q", tc.name, got, tc.want)
		}
	}
}
