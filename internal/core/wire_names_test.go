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
